package model

import (
	"fmt"
	"math"
	"math/rand"

	"aletheia/internal/tensor"
)

const ArchitectureTransformerV2 = "decoder_transformer_v2"

type TransformerV2 struct {
	Config    Config
	Embedding []float32
	Layers    []TransformerV2Layer
	NormScale []float32
	LMHead    []float32
}

type TransformerV2Layer struct {
	Q        []float32
	K        []float32
	V        []float32
	O        []float32
	FFGate   []float32
	FFUp     []float32
	FFDown   []float32
	AttnNorm []float32
	FFNNorm  []float32
	// Mixture-of-Experts feed-forward (active when len(Experts) > 0). MoEGate is
	// the [DModel x NumExperts] router; each expert is its own SwiGLU FFN.
	MoEGate []float32
	Experts []ffnExpert
}

// ffnExpert is a single SwiGLU feed-forward expert in a MoE layer.
type ffnExpert struct {
	Gate []float32 // DModel x DFF
	Up   []float32 // DModel x DFF
	Down []float32 // DFF x DModel
}

func NewTransformerV2(cfg Config) (*TransformerV2, error) {
	if cfg.VocabSize <= 0 || cfg.DModel <= 0 || cfg.ContextLength <= 1 {
		return nil, fmt.Errorf("invalid transformer v2 dimensions")
	}
	if cfg.NLayers <= 0 || cfg.NHeads <= 0 || cfg.DFF <= 0 {
		return nil, fmt.Errorf("transformer v2 layers, heads, and d_ff must be positive")
	}
	if cfg.DModel%cfg.NHeads != 0 {
		return nil, fmt.Errorf("d_model must be divisible by n_heads")
	}
	seed := cfg.Seed
	if seed == 0 {
		seed = 1
	}
	rng := rand.New(rand.NewSource(seed))
	init := func(n int) []float32 {
		out := make([]float32, n)
		scale := 1 / math.Sqrt(float64(cfg.DModel))
		for i := range out {
			out[i] = float32(rng.NormFloat64() * scale * 0.02)
		}
		return out
	}
	ones := func(n int) []float32 {
		out := make([]float32, n)
		for i := range out {
			out[i] = 1
		}
		return out
	}
	m := &TransformerV2{
		Config:    cfg,
		Embedding: init(cfg.VocabSize * cfg.DModel),
		Layers:    make([]TransformerV2Layer, cfg.NLayers),
		NormScale: ones(cfg.DModel),
		LMHead:    init(cfg.DModel * cfg.VocabSize),
	}
	for i := range m.Layers {
		layer := TransformerV2Layer{
			Q:        init(cfg.DModel * cfg.DModel),
			K:        init(cfg.DModel * cfg.DModel),
			V:        init(cfg.DModel * cfg.DModel),
			O:        init(cfg.DModel * cfg.DModel),
			FFGate:   init(cfg.DModel * cfg.DFF),
			FFUp:     init(cfg.DModel * cfg.DFF),
			FFDown:   init(cfg.DFF * cfg.DModel),
			AttnNorm: ones(cfg.DModel),
			FFNNorm:  ones(cfg.DModel),
		}
		if cfg.NumExperts > 1 {
			layer.MoEGate = init(cfg.DModel * cfg.NumExperts)
			layer.Experts = make([]ffnExpert, cfg.NumExperts)
			for e := range layer.Experts {
				layer.Experts[e] = ffnExpert{
					Gate: init(cfg.DModel * cfg.DFF),
					Up:   init(cfg.DModel * cfg.DFF),
					Down: init(cfg.DFF * cfg.DModel),
				}
			}
		}
		m.Layers[i] = layer
	}
	return m, nil
}

// topKExperts returns the configured number of experts to activate per token,
// clamped to [1, NumExperts]. Defaults to 2 (DeepSeek-style sparse routing).
func (m *TransformerV2) topKExperts() int {
	k := m.Config.TopKExperts
	if k <= 0 {
		k = 2
	}
	if k > m.Config.NumExperts {
		k = m.Config.NumExperts
	}
	return k
}

func (m *TransformerV2) Forward(tokens []int) ([][]float32, error) {
	if len(tokens) == 0 {
		return nil, fmt.Errorf("forward needs at least one token")
	}
	if len(tokens) > m.Config.ContextLength {
		return nil, fmt.Errorf("sequence length %d exceeds context length %d", len(tokens), m.Config.ContextLength)
	}
	x := make([][]float32, len(tokens))
	for i, token := range tokens {
		if token < 0 || token >= m.Config.VocabSize {
			return nil, fmt.Errorf("token id %d outside vocab size %d", token, m.Config.VocabSize)
		}
		row := m.Embedding[token*m.Config.DModel : (token+1)*m.Config.DModel]
		x[i] = append([]float32(nil), row...)
	}
	for _, layer := range m.Layers {
		next, err := m.forwardLayer(layer, x)
		if err != nil {
			return nil, err
		}
		x = next
	}
	logits := make([][]float32, len(x))
	for i := range x {
		h := scaleVector(tensor.RMSNorm(x[i], 1e-5), m.NormScale)
		logits[i] = matVec(h, m.LMHead, m.Config.VocabSize)
	}
	return logits, nil
}

func (m *TransformerV2) PredictNext(tokens []int) ([]float32, error) {
	logits, err := m.Forward(tokens)
	if err != nil {
		return nil, err
	}
	return logits[len(logits)-1], nil
}

func (m *TransformerV2) forwardLayer(layer TransformerV2Layer, x [][]float32) ([][]float32, error) {
	dim := m.Config.DModel
	headDim := dim / m.Config.NHeads
	n := len(x)
	normed := make([][]float32, n)
	q := make([][]float32, n)
	k := make([][]float32, n)
	v := make([][]float32, n)
	for pos := range x {
		normed[pos] = scaleVector(tensor.RMSNorm(x[pos], 1e-5), layer.AttnNorm)
		q[pos] = matVec(normed[pos], layer.Q, dim)
		k[pos] = matVec(normed[pos], layer.K, dim)
		v[pos] = matVec(normed[pos], layer.V, dim)
		for h := 0; h < m.Config.NHeads; h++ {
			start := h * headDim
			end := start + headDim
			tensor.ApplyRoPE(q[pos][start:end], k[pos][start:end], pos)
		}
	}
	out := make([][]float32, n)
	for pos := 0; pos < n; pos++ {
		context := make([]float32, dim)
		for h := 0; h < m.Config.NHeads; h++ {
			start := h * headDim
			end := start + headDim
			scores := make([]float32, pos+1)
			for past := 0; past <= pos; past++ {
				var score float32
				for d := start; d < end; d++ {
					score += q[pos][d] * k[past][d]
				}
				scores[past] = score / float32(math.Sqrt(float64(headDim)))
			}
			probs, err := tensor.Softmax(scores)
			if err != nil {
				return nil, err
			}
			for past, prob := range probs {
				for d := start; d < end; d++ {
					context[d] += prob * v[past][d]
				}
			}
		}
		attn := matVec(context, layer.O, dim)
		hidden := addVectors(x[pos], attn)
		ffNorm := scaleVector(tensor.RMSNorm(hidden, 1e-5), layer.FFNNorm)
		var ff []float32
		var ffErr error
		if len(layer.Experts) > 0 {
			ff, ffErr = m.moeFeedForward(layer, ffNorm)
		} else {
			ff, ffErr = denseFeedForward(layer, ffNorm, m.Config.DFF, dim)
		}
		if ffErr != nil {
			return nil, ffErr
		}
		out[pos] = addVectors(hidden, ff)
	}
	return out, nil
}

func denseFeedForward(layer TransformerV2Layer, ffNorm []float32, dff, dim int) ([]float32, error) {
	gate := matVec(ffNorm, layer.FFGate, dff)
	up := matVec(ffNorm, layer.FFUp, dff)
	activated, err := tensor.SwiGLU(gate, up)
	if err != nil {
		return nil, err
	}
	return matVec(activated, layer.FFDown, dim), nil
}

// moeFeedForward routes the token to its top-k experts via the gating network
// and returns the gate-weighted combination of their outputs. This is sparse
// activation: only k of NumExperts experts run per token, which is what makes a
// large expert pool efficient (DeepSeek-style MoE).
func (m *TransformerV2) moeFeedForward(layer TransformerV2Layer, ffNorm []float32) ([]float32, error) {
	dim := m.Config.DModel
	dff := m.Config.DFF
	logits := matVec(ffNorm, layer.MoEGate, m.Config.NumExperts)
	probs, err := tensor.Softmax(logits)
	if err != nil {
		return nil, err
	}
	chosen := topKIndices(probs, m.topKExperts())
	var norm float32
	for _, idx := range chosen {
		norm += probs[idx]
	}
	if norm == 0 {
		norm = 1
	}
	out := make([]float32, dim)
	for _, idx := range chosen {
		weight := probs[idx] / norm
		expert := layer.Experts[idx]
		gate := matVec(ffNorm, expert.Gate, dff)
		up := matVec(ffNorm, expert.Up, dff)
		activated, err := tensor.SwiGLU(gate, up)
		if err != nil {
			return nil, err
		}
		ff := matVec(activated, expert.Down, dim)
		for i := range out {
			out[i] += weight * ff[i]
		}
	}
	return out, nil
}

// topKIndices returns the indices of the k largest values (ties broken by lower
// index for determinism).
func topKIndices(values []float32, k int) []int {
	if k > len(values) {
		k = len(values)
	}
	chosen := make([]int, 0, k)
	used := make([]bool, len(values))
	for n := 0; n < k; n++ {
		best := -1
		for i := range values {
			if used[i] {
				continue
			}
			if best == -1 || values[i] > values[best] {
				best = i
			}
		}
		if best == -1 {
			break
		}
		used[best] = true
		chosen = append(chosen, best)
	}
	return chosen
}

// MoEAuxLoss computes the standard load-balancing auxiliary loss given, for a
// batch, the fraction of tokens dispatched to each expert and the mean gate
// probability of each expert. Minimizing it spreads load across experts. It is
// a pure function so it is ready to wire into training without changing the
// forward path.
func MoEAuxLoss(tokenFraction, meanGateProb []float32) float32 {
	n := len(tokenFraction)
	if n == 0 || n != len(meanGateProb) {
		return 0
	}
	var loss float32
	for i := 0; i < n; i++ {
		loss += tokenFraction[i] * meanGateProb[i]
	}
	return loss * float32(n)
}

func matVec(x []float32, weights []float32, outDim int) []float32 {
	inDim := len(x)
	out := make([]float32, outDim)
	for o := 0; o < outDim; o++ {
		var sum float32
		for i := 0; i < inDim; i++ {
			sum += x[i] * weights[i*outDim+o]
		}
		out[o] = sum
	}
	return out
}

func addVectors(a, b []float32) []float32 {
	out := make([]float32, len(a))
	for i := range a {
		out[i] = a[i] + b[i]
	}
	return out
}

func scaleVector(x, scale []float32) []float32 {
	out := make([]float32, len(x))
	for i := range x {
		out[i] = x[i] * scale[i]
	}
	return out
}
