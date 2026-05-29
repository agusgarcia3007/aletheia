package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aletheia/internal/verifier"
)

func TestLoadRepoConfigs(t *testing.T) {
	for _, path := range []string{"../../configs/tiny.yaml", "../../configs/seed-10m.yaml", "../../configs/micro.yaml", "../../configs/aletheia-mikros.yaml", "../../configs/aletheia-mikros-v1.yaml", "../../configs/aletheia-hephaestus.yaml"} {
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		if cfg.Project.MemoryDB == "" || cfg.Memory.Embedding != "hashing" {
			t.Fatalf("config %s defaults not applied: %+v", path, cfg)
		}
	}
}

func TestDefaultsAndVerifierHelpers(t *testing.T) {
	path := writeConfig(t, `model:
  vocab_size: 512
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project.DataDir != "./data" || cfg.Project.MemoryDB != "./data/memory.sqlite" {
		t.Fatalf("project defaults = %+v", cfg.Project)
	}
	if cfg.Search.Strategy != "greedy" || cfg.Search.BeamWidth != 4 {
		t.Fatalf("search defaults = %+v", cfg.Search)
	}
	names := strings.Join(cfg.EnabledVerifierNames(), ",")
	if names != verifier.GoTestName {
		t.Fatalf("verifiers = %s, want %s", names, verifier.GoTestName)
	}
	if cfg.EffectiveVerifierTimeout().Seconds() != 60 {
		t.Fatalf("timeout = %s", cfg.EffectiveVerifierTimeout())
	}
	if !cfg.Memory.GraphEnabledBool() {
		t.Fatal("graph should default enabled")
	}
}

func TestStrictUnknownField(t *testing.T) {
	path := writeConfig(t, `model:
  vocab_size: 512
unexpected: true
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestInvalidSearchStrategy(t *testing.T) {
	path := writeConfig(t, `model:
  vocab_size: 512
search:
  strategy: random
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "search.strategy") {
		t.Fatalf("error = %v, want search.strategy", err)
	}
}

func TestMCTSSearchStrategyLoads(t *testing.T) {
	path := writeConfig(t, `model:
  vocab_size: 512
search:
  strategy: mcts
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Search.Strategy != "mcts" {
		t.Fatalf("strategy = %q, want mcts", cfg.Search.Strategy)
	}
}

func TestVerifierValidation(t *testing.T) {
	badCommand := writeConfig(t, `model:
  vocab_size: 512
verifiers:
  go_test:
    enabled: true
    command: "go test ./bad"
`)
	if _, err := Load(badCommand); err == nil || !strings.Contains(err.Error(), "verifiers.go_test.command") {
		t.Fatalf("error = %v, want go_test command validation", err)
	}

	fuzzDisabled := writeConfig(t, `model:
  vocab_size: 512
verifiers:
  fuzz:
    enabled: false
`)
	if _, err := Load(fuzzDisabled); err != nil {
		t.Fatalf("disabled fuzz should load: %v", err)
	}

	fuzzEnabled := writeConfig(t, `model:
  vocab_size: 512
verifiers:
  fuzz:
    enabled: true
`)
	cfg, err := Load(fuzzEnabled)
	if err != nil {
		t.Fatalf("enabled fuzz should load now that verifier exists: %v", err)
	}
	if strings.Join(cfg.EnabledVerifierNames(), ",") != strings.Join([]string{verifier.GoTestName, verifier.GoTestFuzzName}, ",") {
		t.Fatalf("fuzz verifiers = %v", cfg.EnabledVerifierNames())
	}
}

func TestEnabledVerifierOrderAndTimeout(t *testing.T) {
	path := writeConfig(t, `model:
  vocab_size: 512
verifiers:
  static_go_parse:
    enabled: true
  go_test:
    enabled: true
    timeout_seconds: 20
  go_vet:
    enabled: true
    timeout_seconds: 30
  go_test_race:
    enabled: true
    timeout_seconds: 40
  bench:
    enabled: true
    timeout_seconds: 50
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(cfg.EnabledVerifierNames(), ",")
	want := strings.Join([]string{verifier.StaticGoParseName, verifier.GoTestName, verifier.GoVetName, verifier.GoTestRaceName, verifier.GoTestBenchName}, ",")
	if got != want {
		t.Fatalf("verifier order = %s, want %s", got, want)
	}
	if cfg.EffectiveVerifierTimeout().Seconds() != 50 {
		t.Fatalf("timeout = %s, want 50s", cfg.EffectiveVerifierTimeout())
	}
}

func TestResearchConfigDefaultsValidationAndEnv(t *testing.T) {
	path := writeConfig(t, `model:
  vocab_size: 512
research:
  enabled: true
  auto_on_knowledge_gap: true
  provider: searxng
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Research.Enabled || !cfg.Research.AutoOnKnowledgeGap || cfg.Research.SearXNGURL != "http://searxng:8080" || cfg.Research.MaxSources != 5 {
		t.Fatalf("research defaults = %+v", cfg.Research)
	}
	if len(cfg.Research.BlockedDomains) == 0 {
		t.Fatal("blocked domains should default")
	}

	t.Setenv("ALETHEIA_RESEARCH_ENABLED", "false")
	t.Setenv("ALETHEIA_RESEARCH_AUTO", "false")
	t.Setenv("ALETHEIA_SEARXNG_URL", "http://search:8080")
	t.Setenv("ALETHEIA_RESEARCH_MAX_SOURCES", "9")
	research := cfg.ResearchWithEnv()
	if research.Enabled || research.AutoOnKnowledgeGap || research.SearXNGURL != "http://search:8080" || research.MaxSources != 9 {
		t.Fatalf("research env = %+v", research)
	}
}

func TestResearchConfigRejectsInvalidDomain(t *testing.T) {
	path := writeConfig(t, `model:
  vocab_size: 512
research:
  blocked_domains:
    - "https://bad.example"
`)
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "research domains") {
		t.Fatalf("error = %v, want research domain validation", err)
	}
}

func writeConfig(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
