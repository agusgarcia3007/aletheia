package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aletheia/internal/memory"
)

func TestRunConfigInspect(t *testing.T) {
	root := t.TempDir()
	configPath := writeCLIConfig(t, root, "greedy", true)
	out, err := captureStdout(t, func() error {
		return run([]string{"aletheia", "config", "inspect", "--config", configPath})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "project:") || !strings.Contains(out, "search: strategy=greedy") || !strings.Contains(out, "verifiers: static_go_parse,go_test") {
		t.Fatalf("config inspect output:\n%s", out)
	}
}

func TestRunWithoutArgsPrintsUsage(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return run([]string{"aletheia"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Usage:") || !strings.Contains(out, "aletheia solve") {
		t.Fatalf("usage output:\n%s", out)
	}
}

func TestRunServeHelpAndInvalidCheckpoint(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return run([]string{"aletheia", "serve", "--help"})
	})
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("err = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(out, "-checkpoint") || !strings.Contains(out, "-api-key") {
		t.Fatalf("serve help:\n%s", out)
	}

	err = run([]string{"aletheia", "serve", "--auth", "none", "--checkpoint", t.TempDir(), "--addr", "127.0.0.1:0", "--db", filepath.Join(t.TempDir(), "memory.sqlite")})
	if err == nil || !strings.Contains(err.Error(), "load checkpoint") {
		t.Fatalf("err = %v, want checkpoint load error", err)
	}
}

func TestRunInitUsesConfigDB(t *testing.T) {
	root := t.TempDir()
	configPath := writeCLIConfig(t, root, "greedy", false)
	if _, err := captureStdout(t, func() error {
		return run([]string{"aletheia", "init", "--config", configPath})
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "memory.sqlite")); err != nil {
		t.Fatal(err)
	}
}

func TestRunSolveUsesConfigAndFlagOverrides(t *testing.T) {
	root := t.TempDir()
	taskPath, repo := writeCLIBuggyTask(t, root)
	configPath := writeCLIConfig(t, root, "beam", true)
	out, err := captureStdout(t, func() error {
		return run([]string{"aletheia", "solve", "--config", configPath, "--task", taskPath, "--trace"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "source=beam") || !strings.Contains(out, "verifiers=static_go_parse,go_test") {
		t.Fatalf("solve did not use config defaults:\n%s", out)
	}
	got, err := os.ReadFile(filepath.Join(repo, "calculator.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "return a + b") {
		t.Fatalf("repo not patched:\n%s", got)
	}

	overrideRoot := t.TempDir()
	overrideTaskPath, _ := writeCLIBuggyTask(t, overrideRoot)
	overrideConfigPath := writeCLIConfig(t, overrideRoot, "beam", true)
	overrideOut, err := captureStdout(t, func() error {
		return run([]string{"aletheia", "solve", "--config", overrideConfigPath, "--task", overrideTaskPath, "--search", "greedy", "--verifier", "go_test", "--trace"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(overrideOut, "source=mock") || strings.Contains(overrideOut, "source=beam") || !strings.Contains(overrideOut, "verifiers=go_test") {
		t.Fatalf("explicit flags did not override config:\n%s", overrideOut)
	}
}

func TestRunIndexUsesConfigMemoryDefaults(t *testing.T) {
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(docs, "note.md"), "# Note\n\nConfig can disable graph writes.\n")
	configPath := filepath.Join(root, "config.yaml")
	writeCLIFile(t, configPath, `project:
  memory_db: "`+filepath.Join(root, "memory.sqlite")+`"
model:
  vocab_size: 512
memory:
  chunk_size: 24
  chunk_overlap: 4
  embedding: hashing
  graph_enabled: false
`)
	if _, err := captureStdout(t, func() error {
		return run([]string{"aletheia", "index", docs, "--config", configPath})
	}); err != nil {
		t.Fatal(err)
	}
	store, err := memory.Open(filepath.Join(root, "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	stats, err := store.Inspect(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Documents != 1 || stats.Chunks == 0 || stats.Nodes != 0 || stats.Edges != 0 {
		t.Fatalf("index stats = %+v", stats)
	}
}

func TestRunMemoryGraphListsFilteredNodes(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "memory.sqlite")
	store, err := memory.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	episodeID, err := store.CreateEpisode(context.Background(), "fix")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordCausalNode(context.Background(), episodeID, "patch_candidate", "001", map[string]any{"status": "candidate_patch"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		return run([]string{"aletheia", "memory", "graph", "--db", dbPath, "--type", "patch_candidate"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "nodes: 1") || !strings.Contains(out, "type=patch_candidate") {
		t.Fatalf("memory graph output:\n%s", out)
	}
}

func TestRunLearnExportsMemoryDatasets(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "memory.sqlite")
	store, err := memory.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	episodeID, err := store.CreateEpisode(context.Background(), "learn")
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"snapshot":{"max_tool_calls":8},"candidates":[{"action":"<ACT_RUN_TESTS>","log_prob":-0.1}],"chosen":"<ACT_RUN_TESTS>","reward":1}`
	if _, err := store.RecordSelectorExample(context.Background(), episodeID, "1", payload); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(root, "generated")
	out, err := captureStdout(t, func() error {
		return run([]string{"aletheia", "learn", "--db", dbPath, "--out", outDir})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "selector_examples: 1") {
		t.Fatalf("learn output:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(outDir, "selector_examples.jsonl")); err != nil {
		t.Fatal(err)
	}
}

func TestRunDatasetBuildAndTokenizerTrain(t *testing.T) {
	root := t.TempDir()
	datasetPath := filepath.Join(root, "mikros_v1.jsonl")
	out, err := captureStdout(t, func() error {
		return run([]string{"aletheia", "dataset", "build", "--profile", "mikros-v1", "--out", datasetPath})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "profile: mikros-v1") || !strings.Contains(out, "examples:") {
		t.Fatalf("dataset output:\n%s", out)
	}
	tokenizerPath := filepath.Join(root, "tokenizer.json")
	out, err = captureStdout(t, func() error {
		return run([]string{"aletheia", "tokenizer", "train", "--dataset", datasetPath, "--out", tokenizerPath, "--vocab-size", "512"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "tokenizer:") || !strings.Contains(out, "vocab_size:") {
		t.Fatalf("tokenizer output:\n%s", out)
	}
	if _, err := os.Stat(tokenizerPath); err != nil {
		t.Fatal(err)
	}

	curriculumPath := filepath.Join(root, "mikros_curriculum.jsonl")
	out, err = captureStdout(t, func() error {
		return run([]string{"aletheia", "dataset", "build", "--profile", "mikros-curriculum-v1", "--out", curriculumPath})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "profile: mikros-curriculum-v1") {
		t.Fatalf("curriculum output:\n%s", out)
	}
	raw, err := os.ReadFile(curriculumPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(raw), "\n"); got < 1000 {
		t.Fatalf("curriculum examples = %d, want >= 1000", got)
	}
	if !strings.Contains(string(raw), `"expected_mode":"research"`) || !strings.Contains(string(raw), `"negative":true`) {
		sample := string(raw)
		if len(sample) > 512 {
			sample = sample[:512]
		}
		t.Fatalf("curriculum missing metadata:\n%s", sample)
	}

	livePath := filepath.Join(root, "mikros_live.jsonl")
	out, err = captureStdout(t, func() error {
		return run([]string{"aletheia", "dataset", "build", "--profile", "mikros-live-v1", "--out", livePath})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "profile: mikros-live-v1") {
		t.Fatalf("live output:\n%s", out)
	}
	liveRaw, err := os.ReadFile(livePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(liveRaw), `"expected_mode":"answerer:coding"`) || strings.Contains(string(liveRaw), "[curriculum-") {
		t.Fatalf("live dataset metadata:\n%s", liveRaw)
	}
}

func TestRunTrainRouterWritesCheckpoint(t *testing.T) {
	root := t.TempDir()
	dataset := filepath.Join(root, "router.jsonl")
	writeCLIFile(t, dataset, strings.Join([]string{
		`{"text":"hola","intent":"smalltalk"}`,
		`{"text":"como leo un csv en python","intent":"coding_help"}`,
		`{"text":"cuanto es 17 por 23","intent":"math"}`,
		`{"text":"traduce al ingles: no tengo evidencia suficiente","intent":"translation"}`,
		`{"text":"quien gano el mundial brasil 2014","intent":"factual_research"}`,
		`{"text":"blorf zibble quantum vegetable","intent":"abstain"}`,
	}, "\n")+"\n")
	outDir := filepath.Join(root, "router")
	out, err := captureStdout(t, func() error {
		return run([]string{"aletheia", "train-router", "--dataset", dataset, "--out", outDir, "--epochs", "80", "--learning-rate", "0.12"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "router_checkpoint:") || !strings.Contains(out, "train_accuracy:") {
		t.Fatalf("train-router output:\n%s", out)
	}
	if !strings.Contains(out, "validation_accuracy:") {
		t.Fatalf("train-router should report validation accuracy:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(outDir, "router.json")); err != nil {
		t.Fatal(err)
	}
}

func TestRunResearchBackgroundAndStatus(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "memory.sqlite")
	out, err := captureStdout(t, func() error {
		return run([]string{"aletheia", "research", "--db", dbPath, "--query", "what is mcp", "--background"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "status: queued") || !strings.Contains(out, "job_id:") {
		t.Fatalf("research output:\n%s", out)
	}
	jobsOut, err := captureStdout(t, func() error {
		return run([]string{"aletheia", "jobs", "--db", dbPath})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(jobsOut, "jobs: 1") || !strings.Contains(jobsOut, "what is mcp") {
		t.Fatalf("jobs output:\n%s", jobsOut)
	}
}

func TestRunEvalJSON(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return run([]string{"aletheia", "eval", "--suite", "../../evals/bootstrap", "--json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"verified_success_rate"`) || !strings.Contains(out, `"go_compile"`) {
		t.Fatalf("json eval output:\n%s", out)
	}
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := fn()
	closeErr := w.Close()
	os.Stdout = old
	out, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	return string(out), runErr
}

func writeCLIConfig(t *testing.T, root string, strategy string, static bool) string {
	t.Helper()
	path := filepath.Join(root, "config.yaml")
	staticEnabled := "false"
	if static {
		staticEnabled = "true"
	}
	text := `project:
  name: test
  data_dir: "` + filepath.Join(root, "data") + `"
  checkpoint_dir: "` + filepath.Join(root, "checkpoints") + `"
  memory_db: "` + filepath.Join(root, "memory.sqlite") + `"
model:
  vocab_size: 512
search:
  strategy: ` + strategy + `
  beam_width: 4
  max_depth: 8
verifiers:
  static_go_parse:
    enabled: ` + staticEnabled + `
  go_test:
    enabled: true
    command: "go test ./..."
    timeout_seconds: 20
memory:
  chunk_size: 800
  chunk_overlap: 80
  embedding: hashing
  graph_enabled: true
`
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeCLIBuggyTask(t *testing.T, root string) (string, string) {
	t.Helper()
	repo := filepath.Join(root, "buggy")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLIFile(t, filepath.Join(repo, "go.mod"), "module example.com/buggy\n\ngo 1.26\n")
	writeCLIFile(t, filepath.Join(repo, "calculator.go"), `package calculator

func Add(a, b int) int {
	return a - b
}
`)
	writeCLIFile(t, filepath.Join(repo, "calculator_test.go"), `package calculator

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2, 3) = %d", got)
	}
}
`)
	task := struct {
		Goal    string `json:"goal"`
		Repo    string `json:"repo"`
		Success string `json:"success"`
	}{
		Goal:    "Fix the Go project so all tests pass.",
		Repo:    repo,
		Success: "go test ./...",
	}
	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(root, "task.json")
	writeCLIFile(t, taskPath, string(raw))
	return taskPath, repo
}

func writeCLIFile(t *testing.T, path string, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
