package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Project   ProjectConfig   `yaml:"project"`
	Model     ModelConfig     `yaml:"model"`
	Training  TrainingConfig  `yaml:"training"`
	Inference InferenceConfig `yaml:"inference"`
}

type ProjectConfig struct {
	Name          string `yaml:"name"`
	DataDir       string `yaml:"data_dir"`
	CheckpointDir string `yaml:"checkpoint_dir"`
	MemoryDB      string `yaml:"memory_db"`
}

type ModelConfig struct {
	Name          string  `yaml:"name"`
	VocabSize     int     `yaml:"vocab_size"`
	ContextLength int     `yaml:"context_length"`
	NLayers       int     `yaml:"n_layers"`
	NHeads        int     `yaml:"n_heads"`
	DModel        int     `yaml:"d_model"`
	DFF           int     `yaml:"d_ff"`
	Dropout       float64 `yaml:"dropout"`
	Rope          bool    `yaml:"rope"`
	Norm          string  `yaml:"norm"`
	Activation    string  `yaml:"activation"`
	Seed          int64   `yaml:"seed"`
}

type TrainingConfig struct {
	BatchSize       int     `yaml:"batch_size"`
	LearningRate    float64 `yaml:"learning_rate"`
	WeightDecay     float64 `yaml:"weight_decay"`
	MaxSteps        int     `yaml:"max_steps"`
	GradClip        float64 `yaml:"grad_clip"`
	CheckpointEvery int     `yaml:"checkpoint_every"`
	EvalEvery       int     `yaml:"eval_every"`
}

type InferenceConfig struct {
	Temperature float64 `yaml:"temperature"`
	TopK        int     `yaml:"top_k"`
	TopP        float64 `yaml:"top_p"`
	MaxTokens   int     `yaml:"max_tokens"`
}

func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return Config{}, err
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) ApplyDefaults() {
	if c.Project.Name == "" {
		c.Project.Name = "aletheia-mu"
	}
	if c.Project.CheckpointDir == "" {
		c.Project.CheckpointDir = "./checkpoints"
	}
	if c.Model.Name == "" {
		c.Model.Name = "tiny"
	}
	if c.Model.ContextLength == 0 {
		c.Model.ContextLength = 64
	}
	if c.Model.NLayers == 0 {
		c.Model.NLayers = 1
	}
	if c.Model.NHeads == 0 {
		c.Model.NHeads = 1
	}
	if c.Model.DModel == 0 {
		c.Model.DModel = 32
	}
	if c.Model.DFF == 0 {
		c.Model.DFF = c.Model.DModel * 2
	}
	if c.Model.Norm == "" {
		c.Model.Norm = "rmsnorm"
	}
	if c.Model.Activation == "" {
		c.Model.Activation = "swiglu"
	}
	if c.Training.BatchSize == 0 {
		c.Training.BatchSize = 16
	}
	if c.Training.LearningRate == 0 {
		c.Training.LearningRate = 0.05
	}
	if c.Training.MaxSteps == 0 {
		c.Training.MaxSteps = 100
	}
	if c.Training.GradClip == 0 {
		c.Training.GradClip = 5
	}
	if c.Inference.TopK == 0 {
		c.Inference.TopK = 8
	}
	if c.Inference.MaxTokens == 0 {
		c.Inference.MaxTokens = 32
	}
}

func (c Config) Validate() error {
	if c.Model.VocabSize <= 0 {
		return fmt.Errorf("model.vocab_size must be positive")
	}
	if c.Model.ContextLength <= 1 {
		return fmt.Errorf("model.context_length must be greater than 1")
	}
	if c.Model.DModel <= 0 {
		return fmt.Errorf("model.d_model must be positive")
	}
	if c.Model.NHeads <= 0 {
		return fmt.Errorf("model.n_heads must be positive")
	}
	if c.Model.DModel%c.Model.NHeads != 0 {
		return fmt.Errorf("model.d_model must be divisible by model.n_heads")
	}
	if c.Model.DFF <= 0 {
		return fmt.Errorf("model.d_ff must be positive")
	}
	if c.Training.BatchSize <= 0 {
		return fmt.Errorf("training.batch_size must be positive")
	}
	if c.Training.MaxSteps < 0 {
		return fmt.Errorf("training.max_steps must be non-negative")
	}
	if c.Training.LearningRate <= 0 {
		return fmt.Errorf("training.learning_rate must be positive")
	}
	if c.Training.GradClip < 0 {
		return fmt.Errorf("training.grad_clip must be non-negative")
	}
	return nil
}
