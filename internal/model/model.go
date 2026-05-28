package model

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"aletheia/internal/config"
	"aletheia/internal/tensor"
)

type Config = config.ModelConfig

type Sample struct {
	Tokens   []int
	LossMask []bool
}

type Metrics struct {
	Loss      float64
	Accuracy  float64
	Examples  int
	Tokens    int
	GradNorm  float64
	TrainStep int
}

type Model struct {
	Config     Config
	Embedding  []float32
	Output     []float32
	Bias       []float32
	optimizer  *tensor.AdamW
	TrainStepN int
}

type Manifest struct {
	FormatVersion      int          `json:"format_version"`
	CreatedAt          string       `json:"created_at"`
	Config             Config       `json:"config"`
	TokenizerVocabSize int          `json:"tokenizer_vocab_size"`
	Step               int          `json:"step"`
	Loss               float64      `json:"loss"`
	Params             []ParamShape `json:"params"`
}

type ParamShape struct {
	Name  string `json:"name"`
	Shape []int  `json:"shape"`
}

func New(cfg Config) (*Model, error) {
	if cfg.VocabSize <= 0 {
		return nil, fmt.Errorf("vocab size must be positive")
	}
	if cfg.DModel <= 0 {
		return nil, fmt.Errorf("d_model must be positive")
	}
	if cfg.ContextLength <= 1 {
		return nil, fmt.Errorf("context length must be greater than 1")
	}
	if cfg.NHeads <= 0 || cfg.DModel%cfg.NHeads != 0 {
		return nil, fmt.Errorf("d_model must be divisible by n_heads")
	}
	seed := cfg.Seed
	if seed == 0 {
		seed = 1
	}
	rng := rand.New(rand.NewSource(seed))
	m := &Model{
		Config:    cfg,
		Embedding: make([]float32, cfg.VocabSize*cfg.DModel),
		Output:    make([]float32, cfg.DModel*cfg.VocabSize),
		Bias:      make([]float32, cfg.VocabSize),
	}
	initScale := 0.02
	for i := range m.Embedding {
		m.Embedding[i] = float32(rng.NormFloat64() * initScale)
	}
	for i := range m.Output {
		m.Output[i] = float32(rng.NormFloat64() * initScale)
	}
	return m, nil
}

func (m *Model) Params() [][]float32 {
	return [][]float32{m.Embedding, m.Output, m.Bias}
}

func (m *Model) Forward(tokens []int) ([][]float32, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("forward needs at least one token")
	}
	if len(tokens) > m.Config.ContextLength {
		return nil, fmt.Errorf("sequence length %d exceeds context length %d", len(tokens), m.Config.ContextLength)
	}
	logits := make([][]float32, len(tokens))
	for pos := range tokens {
		next, err := m.logitsAt(tokens, pos)
		if err != nil {
			return nil, err
		}
		logits[pos] = next
	}
	return logits, nil
}

func (m *Model) PredictNext(tokens []int) ([]float32, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("predict needs at least one token")
	}
	return m.logitsAt(tokens, len(tokens)-1)
}

func (m *Model) TrainBatch(samples []Sample, learningRate, weightDecay, gradClip float64) (Metrics, error) {
	if len(samples) == 0 {
		return Metrics{}, fmt.Errorf("empty training batch")
	}
	gradEmb := make([]float32, len(m.Embedding))
	gradOut := make([]float32, len(m.Output))
	gradBias := make([]float32, len(m.Bias))

	var totalLoss float64
	var correct, trainTokens int
	for _, sample := range samples {
		if err := m.validateSample(sample); err != nil {
			return Metrics{}, err
		}
		for pos := 0; pos < len(sample.Tokens)-1; pos++ {
			targetPos := pos + 1
			if !sample.LossMask[targetPos] {
				continue
			}
			h, err := tensor.CausalMean(m.Embedding, m.Config.VocabSize, m.Config.DModel, sample.Tokens, pos)
			if err != nil {
				return Metrics{}, err
			}
			logits := make([]float32, m.Config.VocabSize)
			for v := 0; v < m.Config.VocabSize; v++ {
				sum := m.Bias[v]
				for d := 0; d < m.Config.DModel; d++ {
					sum += h[d] * m.Output[d*m.Config.VocabSize+v]
				}
				logits[v] = sum
			}
			target := sample.Tokens[targetPos]
			loss, dlogits, err := tensor.CrossEntropy(logits, target)
			if err != nil {
				return Metrics{}, err
			}
			totalLoss += loss
			trainTokens++
			if argmax(logits) == target {
				correct++
			}

			for d := 0; d < m.Config.DModel; d++ {
				for v := 0; v < m.Config.VocabSize; v++ {
					gradOut[d*m.Config.VocabSize+v] += h[d] * dlogits[v]
				}
			}
			copyAdd(gradBias, dlogits)

			dh := make([]float32, m.Config.DModel)
			for d := 0; d < m.Config.DModel; d++ {
				var sum float32
				for v := 0; v < m.Config.VocabSize; v++ {
					sum += m.Output[d*m.Config.VocabSize+v] * dlogits[v]
				}
				dh[d] = sum / float32(pos+1)
			}
			for i := 0; i <= pos; i++ {
				id := sample.Tokens[i]
				row := id * m.Config.DModel
				for d := 0; d < m.Config.DModel; d++ {
					gradEmb[row+d] += dh[d]
				}
			}
		}
	}
	if trainTokens == 0 {
		return Metrics{}, fmt.Errorf("batch has no masked training tokens")
	}

	scale := float32(1.0 / float64(trainTokens))
	for _, grad := range [][]float32{gradEmb, gradOut, gradBias} {
		for i := range grad {
			grad[i] *= scale
		}
	}
	gradNorm := tensor.ClipGradNorm([][]float32{gradEmb, gradOut, gradBias}, gradClip)
	if m.optimizer == nil || m.optimizer.LearningRate != learningRate || m.optimizer.WeightDecay != weightDecay {
		m.optimizer = tensor.NewAdamW(learningRate, weightDecay, m.Params())
	}
	if err := m.optimizer.Update(m.Params(), [][]float32{gradEmb, gradOut, gradBias}); err != nil {
		return Metrics{}, err
	}
	m.TrainStepN++

	return Metrics{
		Loss:      totalLoss / float64(trainTokens),
		Accuracy:  float64(correct) / float64(trainTokens),
		Examples:  len(samples),
		Tokens:    trainTokens,
		GradNorm:  gradNorm,
		TrainStep: m.TrainStepN,
	}, nil
}

func (m *Model) Evaluate(samples []Sample) (Metrics, error) {
	var totalLoss float64
	var correct, trainTokens int
	for _, sample := range samples {
		if err := m.validateSample(sample); err != nil {
			return Metrics{}, err
		}
		for pos := 0; pos < len(sample.Tokens)-1; pos++ {
			targetPos := pos + 1
			if !sample.LossMask[targetPos] {
				continue
			}
			logits, err := m.logitsAt(sample.Tokens, pos)
			if err != nil {
				return Metrics{}, err
			}
			target := sample.Tokens[targetPos]
			loss, _, err := tensor.CrossEntropy(logits, target)
			if err != nil {
				return Metrics{}, err
			}
			totalLoss += loss
			trainTokens++
			if argmax(logits) == target {
				correct++
			}
		}
	}
	if trainTokens == 0 {
		return Metrics{}, fmt.Errorf("no masked training tokens")
	}
	return Metrics{
		Loss:     totalLoss / float64(trainTokens),
		Accuracy: float64(correct) / float64(trainTokens),
		Examples: len(samples),
		Tokens:   trainTokens,
	}, nil
}

func (m *Model) Save(dir string, tokenizerVocabSize int, step int, loss float64) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	manifest := Manifest{
		FormatVersion:      1,
		CreatedAt:          time.Now().UTC().Format(time.RFC3339Nano),
		Config:             m.Config,
		TokenizerVocabSize: tokenizerVocabSize,
		Step:               step,
		Loss:               loss,
		Params: []ParamShape{
			{Name: "embedding", Shape: []int{m.Config.VocabSize, m.Config.DModel}},
			{Name: "output", Shape: []int{m.Config.DModel, m.Config.VocabSize}},
			{Name: "bias", Shape: []int{m.Config.VocabSize}},
		},
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0o644); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "weights.f32"))
	if err != nil {
		return err
	}
	defer f.Close()
	for _, param := range m.Params() {
		for _, v := range param {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				return fmt.Errorf("refusing to save invalid weight %v", v)
			}
			if err := binary.Write(f, binary.LittleEndian, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func Load(dir string, tokenizerVocabSize int) (*Model, Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, Manifest{}, err
	}
	if manifest.FormatVersion != 1 {
		return nil, Manifest{}, fmt.Errorf("unsupported checkpoint format version %d", manifest.FormatVersion)
	}
	if manifest.TokenizerVocabSize != tokenizerVocabSize {
		return nil, Manifest{}, fmt.Errorf("checkpoint tokenizer vocab %d incompatible with runtime vocab %d", manifest.TokenizerVocabSize, tokenizerVocabSize)
	}
	m, err := New(manifest.Config)
	if err != nil {
		return nil, Manifest{}, err
	}
	if err := validateParamShapes(manifest, m); err != nil {
		return nil, Manifest{}, err
	}
	f, err := os.Open(filepath.Join(dir, "weights.f32"))
	if err != nil {
		return nil, Manifest{}, err
	}
	defer f.Close()
	for _, param := range m.Params() {
		for i := range param {
			if err := binary.Read(f, binary.LittleEndian, &param[i]); err != nil {
				return nil, Manifest{}, err
			}
			if math.IsNaN(float64(param[i])) || math.IsInf(float64(param[i]), 0) {
				return nil, Manifest{}, fmt.Errorf("checkpoint contains invalid weight %v", param[i])
			}
		}
	}
	m.TrainStepN = manifest.Step
	return m, manifest, nil
}

func (m *Model) logitsAt(tokens []int, pos int) ([]float32, error) {
	h, err := tensor.CausalMean(m.Embedding, m.Config.VocabSize, m.Config.DModel, tokens, pos)
	if err != nil {
		return nil, err
	}
	logits := make([]float32, m.Config.VocabSize)
	for v := 0; v < m.Config.VocabSize; v++ {
		sum := m.Bias[v]
		for d := 0; d < m.Config.DModel; d++ {
			sum += h[d] * m.Output[d*m.Config.VocabSize+v]
		}
		logits[v] = sum
	}
	return logits, nil
}

func (m *Model) validateSample(sample Sample) error {
	if len(sample.Tokens) == 0 {
		return fmt.Errorf("sample has no tokens")
	}
	if len(sample.Tokens) != len(sample.LossMask) {
		return fmt.Errorf("sample token/mask length mismatch")
	}
	if len(sample.Tokens) > m.Config.ContextLength {
		return fmt.Errorf("sample length %d exceeds context length %d", len(sample.Tokens), m.Config.ContextLength)
	}
	for _, id := range sample.Tokens {
		if id < 0 || id >= m.Config.VocabSize {
			return fmt.Errorf("token id %d outside vocab size %d", id, m.Config.VocabSize)
		}
	}
	return nil
}

func validateParamShapes(manifest Manifest, m *Model) error {
	want := []ParamShape{
		{Name: "embedding", Shape: []int{m.Config.VocabSize, m.Config.DModel}},
		{Name: "output", Shape: []int{m.Config.DModel, m.Config.VocabSize}},
		{Name: "bias", Shape: []int{m.Config.VocabSize}},
	}
	if len(manifest.Params) != len(want) {
		return fmt.Errorf("checkpoint param count mismatch")
	}
	for i := range want {
		if manifest.Params[i].Name != want[i].Name || !sameInts(manifest.Params[i].Shape, want[i].Shape) {
			return fmt.Errorf("checkpoint param %d shape mismatch: got %+v want %+v", i, manifest.Params[i], want[i])
		}
	}
	return nil
}

func copyAdd(dst []float32, src []float32) {
	for i := range dst {
		dst[i] += src[i]
	}
}

func argmax(values []float32) int {
	best := 0
	for i := 1; i < len(values); i++ {
		if values[i] > values[best] {
			best = i
		}
	}
	return best
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
