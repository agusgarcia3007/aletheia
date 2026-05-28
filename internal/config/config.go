package config

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"aletheia/internal/verifier"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Project   ProjectConfig   `yaml:"project"`
	Model     ModelConfig     `yaml:"model"`
	Training  TrainingConfig  `yaml:"training"`
	Inference InferenceConfig `yaml:"inference"`
	Search    SearchConfig    `yaml:"search"`
	Verifiers VerifiersConfig `yaml:"verifiers"`
	Memory    MemoryConfig    `yaml:"memory"`
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

type SearchConfig struct {
	Strategy        string `yaml:"strategy"`
	BeamWidth       int    `yaml:"beam_width"`
	MaxDepth        int    `yaml:"max_depth"`
	BudgetSeconds   int    `yaml:"budget_seconds"`
	BudgetToolCalls int    `yaml:"budget_tool_calls"`
}

type VerifiersConfig struct {
	StaticGoParse VerifierConfig `yaml:"static_go_parse"`
	GoTest        VerifierConfig `yaml:"go_test"`
	GoVet         VerifierConfig `yaml:"go_vet"`
	GoTestRace    VerifierConfig `yaml:"go_test_race"`
	Fuzz          VerifierConfig `yaml:"fuzz"`
}

type VerifierConfig struct {
	Enabled        *bool  `yaml:"enabled"`
	Command        string `yaml:"command"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

type MemoryConfig struct {
	ChunkSize    int    `yaml:"chunk_size"`
	ChunkOverlap int    `yaml:"chunk_overlap"`
	MaxFileBytes int64  `yaml:"max_file_bytes"`
	Embedding    string `yaml:"embedding"`
	GraphEnabled *bool  `yaml:"graph_enabled"`
}

func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
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
	if c.Project.DataDir == "" {
		c.Project.DataDir = "./data"
	}
	if c.Project.CheckpointDir == "" {
		c.Project.CheckpointDir = "./checkpoints"
	}
	if c.Project.MemoryDB == "" {
		c.Project.MemoryDB = "./data/memory.sqlite"
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
	if c.Inference.TopP == 0 {
		c.Inference.TopP = 1
	}
	if c.Inference.MaxTokens == 0 {
		c.Inference.MaxTokens = 32
	}
	if c.Search.Strategy == "" {
		c.Search.Strategy = "greedy"
	}
	if c.Search.BeamWidth == 0 {
		c.Search.BeamWidth = 4
	}
	if c.Search.MaxDepth == 0 {
		c.Search.MaxDepth = 8
	}
	if c.Search.BudgetSeconds == 0 {
		c.Search.BudgetSeconds = 120
	}
	if c.Search.BudgetToolCalls == 0 {
		c.Search.BudgetToolCalls = 50
	}
	c.Verifiers.applyDefaults()
	if c.Memory.ChunkSize == 0 {
		c.Memory.ChunkSize = 1200
	}
	if c.Memory.ChunkOverlap == 0 {
		c.Memory.ChunkOverlap = 200
	}
	if c.Memory.MaxFileBytes == 0 {
		c.Memory.MaxFileBytes = 512 * 1024
	}
	if c.Memory.Embedding == "" {
		c.Memory.Embedding = "hashing"
	}
	if c.Memory.GraphEnabled == nil {
		c.Memory.GraphEnabled = boolPtr(true)
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
	if c.Inference.TopK < 0 {
		return fmt.Errorf("inference.top_k must be non-negative")
	}
	if c.Inference.MaxTokens <= 0 {
		return fmt.Errorf("inference.max_tokens must be positive")
	}
	if c.Search.Strategy != "greedy" && c.Search.Strategy != "beam" {
		return fmt.Errorf("search.strategy must be greedy or beam")
	}
	if c.Search.BeamWidth <= 0 {
		return fmt.Errorf("search.beam_width must be positive")
	}
	if c.Search.MaxDepth <= 0 {
		return fmt.Errorf("search.max_depth must be positive")
	}
	if c.Search.BudgetSeconds <= 0 {
		return fmt.Errorf("search.budget_seconds must be positive")
	}
	if c.Search.BudgetToolCalls <= 0 {
		return fmt.Errorf("search.budget_tool_calls must be positive")
	}
	if c.Memory.ChunkSize <= 0 {
		return fmt.Errorf("memory.chunk_size must be positive")
	}
	if c.Memory.ChunkOverlap < 0 {
		return fmt.Errorf("memory.chunk_overlap must be non-negative")
	}
	if c.Memory.ChunkOverlap >= c.Memory.ChunkSize {
		return fmt.Errorf("memory.chunk_overlap must be smaller than memory.chunk_size")
	}
	if c.Memory.MaxFileBytes <= 0 {
		return fmt.Errorf("memory.max_file_bytes must be positive")
	}
	if c.Memory.Embedding != "hashing" {
		return fmt.Errorf("memory.embedding must be hashing")
	}
	if err := c.Verifiers.validate(); err != nil {
		return err
	}
	return nil
}

func (c Config) EnabledVerifierNames() []string {
	return c.Verifiers.EnabledNames()
}

func (c Config) EffectiveVerifierTimeout() time.Duration {
	return time.Duration(c.Verifiers.EffectiveTimeoutSeconds()) * time.Second
}

func (v VerifiersConfig) EnabledNames() []string {
	var names []string
	if v.StaticGoParse.EnabledBool() {
		names = append(names, verifier.StaticGoParseName)
	}
	if v.GoTest.EnabledBool() {
		names = append(names, verifier.GoTestName)
	}
	if v.GoVet.EnabledBool() {
		names = append(names, verifier.GoVetName)
	}
	if v.GoTestRace.EnabledBool() {
		names = append(names, verifier.GoTestRaceName)
	}
	return names
}

func (v VerifiersConfig) EffectiveTimeoutSeconds() int {
	maxSeconds := 0
	for _, verifierConfig := range []VerifierConfig{v.StaticGoParse, v.GoTest, v.GoVet, v.GoTestRace} {
		if verifierConfig.EnabledBool() && verifierConfig.TimeoutSeconds > maxSeconds {
			maxSeconds = verifierConfig.TimeoutSeconds
		}
	}
	if maxSeconds == 0 {
		maxSeconds = 60
	}
	return maxSeconds
}

func (v VerifierConfig) EnabledBool() bool {
	return v.Enabled != nil && *v.Enabled
}

func (m MemoryConfig) GraphEnabledBool() bool {
	return m.GraphEnabled == nil || *m.GraphEnabled
}

func (v *VerifiersConfig) applyDefaults() {
	v.StaticGoParse.applyDefaults(false, "", 0)
	v.GoTest.applyDefaults(true, verifier.GoTestCommand, 60)
	v.GoVet.applyDefaults(false, verifier.GoVetCommand, 60)
	v.GoTestRace.applyDefaults(false, verifier.GoTestRaceCommand, 120)
	v.Fuzz.applyDefaults(false, "", 120)
}

func (v *VerifierConfig) applyDefaults(enabled bool, command string, timeoutSeconds int) {
	if v.Enabled == nil {
		v.Enabled = boolPtr(enabled)
	}
	if v.Command == "" {
		v.Command = command
	}
	if v.TimeoutSeconds == 0 {
		v.TimeoutSeconds = timeoutSeconds
	}
}

func (v VerifiersConfig) validate() error {
	if err := validateVerifier(verifier.StaticGoParseName, v.StaticGoParse, ""); err != nil {
		return err
	}
	if err := validateVerifier(verifier.GoTestName, v.GoTest, verifier.GoTestCommand); err != nil {
		return err
	}
	if err := validateVerifier(verifier.GoVetName, v.GoVet, verifier.GoVetCommand); err != nil {
		return err
	}
	if err := validateVerifier(verifier.GoTestRaceName, v.GoTestRace, verifier.GoTestRaceCommand); err != nil {
		return err
	}
	if v.Fuzz.EnabledBool() {
		return fmt.Errorf("verifiers.fuzz is declared but not supported yet")
	}
	if v.Fuzz.TimeoutSeconds < 0 {
		return fmt.Errorf("verifiers.fuzz.timeout_seconds must be non-negative")
	}
	return nil
}

func validateVerifier(name string, cfg VerifierConfig, expectedCommand string) error {
	if cfg.TimeoutSeconds < 0 {
		return fmt.Errorf("verifiers.%s.timeout_seconds must be non-negative", name)
	}
	if !cfg.EnabledBool() {
		return nil
	}
	if expectedCommand == "" {
		return nil
	}
	if cfg.Command != expectedCommand {
		return fmt.Errorf("verifiers.%s.command must be %q", name, expectedCommand)
	}
	if !verifier.IsAllowed(cfg.Command) {
		return fmt.Errorf("verifiers.%s.command is not allowlisted", name)
	}
	return nil
}

func boolPtr(v bool) *bool {
	return &v
}
