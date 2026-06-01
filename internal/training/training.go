package training

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"aletheia/internal/config"
	"aletheia/internal/model"
	"aletheia/internal/tokenizer"
)

type JSONLExample struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

type Options struct {
	ConfigPath    string
	DatasetPath   string
	OutDir        string
	Steps         int
	OverrideSteps bool
	// Config, when set, is used directly instead of loading ConfigPath from disk
	// — lets callers (e.g. the admin pipeline in a config-less container) train
	// against an in-memory configuration.
	Config *config.Config
}

type Report struct {
	InitialLoss     float64
	FinalLoss       float64
	InitialAccuracy float64
	FinalAccuracy   float64
	Steps           int
	CheckpointPath  string
}

func LoadDataset(path string, tok *tokenizer.Tokenizer, contextLength int) ([]model.Sample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var samples []model.Sample
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ex JSONLExample
		if err := json.Unmarshal(line, &ex); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
		if ex.Prompt == "" {
			return nil, fmt.Errorf("%s:%d: prompt is required", path, lineNo)
		}
		if ex.Completion == "" {
			return nil, fmt.Errorf("%s:%d: completion is required", path, lineNo)
		}
		promptTokens := tok.Encode(ex.Prompt)
		completionTokens := tok.Encode(ex.Completion)
		tokens := append(append([]int(nil), promptTokens...), completionTokens...)
		if len(tokens) > contextLength {
			return nil, fmt.Errorf("%s:%d: sequence length %d exceeds context length %d", path, lineNo, len(tokens), contextLength)
		}
		mask := make([]bool, len(tokens))
		for i := len(promptTokens); i < len(tokens); i++ {
			mask[i] = true
		}
		samples = append(samples, model.Sample{Tokens: tokens, LossMask: mask})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("dataset %s has no examples", path)
	}
	return samples, nil
}

func Train(ctx context.Context, opts Options) (Report, error) {
	var cfg config.Config
	if opts.Config != nil {
		cfg = *opts.Config
	} else {
		loaded, err := config.Load(opts.ConfigPath)
		if err != nil {
			return Report{}, err
		}
		cfg = loaded
	}
	if opts.OverrideSteps {
		cfg.Training.MaxSteps = opts.Steps
	}
	tok := tokenizer.New()
	if cfg.Model.VocabSize < tok.VocabSize() {
		return Report{}, fmt.Errorf("model vocab_size %d is smaller than tokenizer vocab %d", cfg.Model.VocabSize, tok.VocabSize())
	}
	samples, err := LoadDataset(opts.DatasetPath, tok, cfg.Model.ContextLength)
	if err != nil {
		return Report{}, err
	}
	m, err := model.New(cfg.Model)
	if err != nil {
		return Report{}, err
	}
	initial, err := m.Evaluate(samples)
	if err != nil {
		return Report{}, err
	}

	steps := cfg.Training.MaxSteps
	if steps < 0 {
		steps = 0
	}
	for step := 0; step < steps; step++ {
		select {
		case <-ctx.Done():
			return Report{}, ctx.Err()
		default:
		}
		batch := nextBatch(samples, step, cfg.Training.BatchSize)
		if _, err := m.TrainBatch(batch, cfg.Training.LearningRate, cfg.Training.WeightDecay, cfg.Training.GradClip); err != nil {
			return Report{}, err
		}
	}
	final, err := m.Evaluate(samples)
	if err != nil {
		return Report{}, err
	}
	if opts.OutDir == "" {
		opts.OutDir = filepath.Join(cfg.Project.CheckpointDir, cfg.Model.Name)
	}
	if err := m.Save(opts.OutDir, tok.VocabSize(), steps, final.Loss); err != nil {
		return Report{}, err
	}
	if err := copyDatasetArtifact(opts.DatasetPath, filepath.Join(opts.OutDir, "chat_examples.jsonl")); err != nil {
		return Report{}, err
	}
	return Report{
		InitialLoss:     initial.Loss,
		FinalLoss:       final.Loss,
		InitialAccuracy: initial.Accuracy,
		FinalAccuracy:   final.Accuracy,
		Steps:           steps,
		CheckpointPath:  opts.OutDir,
	}, nil
}

func copyDatasetArtifact(src string, dst string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, raw, 0o644)
}

func nextBatch(samples []model.Sample, step int, batchSize int) []model.Sample {
	if batchSize >= len(samples) {
		return samples
	}
	out := make([]model.Sample, 0, batchSize)
	start := (step * batchSize) % len(samples)
	for i := 0; i < batchSize; i++ {
		out = append(out, samples[(start+i)%len(samples)])
	}
	return out
}
