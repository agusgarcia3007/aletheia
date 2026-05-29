package training

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aletheia/internal/model"
	"aletheia/internal/runner"
	"aletheia/internal/tokenizer"
)

func TestLoadDatasetAndRejectOverContext(t *testing.T) {
	tok := tokenizer.New()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.jsonl")
	if err := os.WriteFile(path, []byte(`{"prompt":"<USER>x<ASSISTANT>","completion":"<ACT_RESPOND>"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	samples, err := LoadDataset(path, tok, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 1 {
		t.Fatalf("samples = %d, want 1", len(samples))
	}
	if !samples[0].LossMask[len(samples[0].LossMask)-1] {
		t.Fatal("completion token is not masked for loss")
	}
	if _, err := LoadDataset(path, tok, 2); err == nil {
		t.Fatal("expected over-context error")
	}
}

func TestLoadChatBasicDataset(t *testing.T) {
	tok := tokenizer.New()
	path := "../../datasets/aletheia_mikros.jsonl"
	samples, err := LoadDataset(path, tok, 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) < 20 {
		t.Fatalf("chat samples = %d, want at least 20", len(samples))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "<ACT_") {
		t.Fatal("chat dataset should not train action tokens")
	}
	if !strings.Contains(string(raw), "aletheia-mikros") {
		t.Fatal("chat dataset should document the public chat model slug")
	}
}

func TestTrainTinyAndGenerate(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "tiny.yaml")
	datasetPath := filepath.Join(dir, "data.jsonl")
	outDir := filepath.Join(dir, "ckpt")
	writeTrainingFile(t, configPath, `project:
  name: test
  checkpoint_dir: `+dir+`
model:
  name: tiny-test
  vocab_size: 512
  context_length: 48
  n_layers: 1
  n_heads: 2
  d_model: 24
  d_ff: 48
  seed: 5
training:
  batch_size: 4
  learning_rate: 0.09
  max_steps: 120
  grad_clip: 5
inference:
  max_tokens: 8
  top_k: 4
`)
	var data strings.Builder
	for i := 0; i < 8; i++ {
		data.WriteString(`{"prompt":"<USER>fix failing go test<ASSISTANT>","completion":"<ACT_RUN_TESTS><ACT_PARSE_CODE><ACT_MUTATE_CODE><ACT_VERIFY><ACT_RESPOND>"}` + "\n")
	}
	writeTrainingFile(t, datasetPath, data.String())

	report, err := Train(context.Background(), Options{
		ConfigPath:  configPath,
		DatasetPath: datasetPath,
		OutDir:      outDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.FinalLoss >= report.InitialLoss*0.8 {
		t.Fatalf("loss did not drop enough: initial=%v final=%v", report.InitialLoss, report.FinalLoss)
	}
	if _, err := os.Stat(filepath.Join(outDir, "chat_examples.jsonl")); err != nil {
		t.Fatalf("chat examples artifact missing: %v", err)
	}
	tok := tokenizer.New()
	m, _, err := model.Load(outDir, tok.VocabSize())
	if err != nil {
		t.Fatal(err)
	}
	r := runner.New(m, tok)
	actRespond, _ := tok.ID("<ACT_RESPOND>")
	tokens, err := r.Generate("<USER>fix failing go test<ASSISTANT>", runner.Options{
		MaxTokens:  8,
		StopTokens: []int{actRespond},
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := tok.Decode(tokens)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(decoded, "<ACT_RUN_TESTS>") || !strings.Contains(decoded, "<ACT_RESPOND>") {
		t.Fatalf("generated sequence missing expected actions: %s", decoded)
	}
}

func TestTrainBasicChatSubsetAndGenerate(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "chat.yaml")
	datasetPath := filepath.Join(dir, "chat.jsonl")
	outDir := filepath.Join(dir, "ckpt")
	writeTrainingFile(t, configPath, `project:
  name: test
  checkpoint_dir: `+dir+`
model:
  name: aletheia-mikros-test
  vocab_size: 512
  context_length: 64
  n_layers: 1
  n_heads: 2
  d_model: 24
  d_ff: 48
  seed: 13
training:
  batch_size: 4
  learning_rate: 0.09
  max_steps: 120
  grad_clip: 5
inference:
  max_tokens: 16
  top_k: 4
`)
	var data strings.Builder
	for i := 0; i < 8; i++ {
		data.WriteString(`{"prompt":"<USER>hola<ASSISTANT>","completion":"Hola.<EOS>"}` + "\n")
	}
	writeTrainingFile(t, datasetPath, data.String())

	report, err := Train(context.Background(), Options{
		ConfigPath:  configPath,
		DatasetPath: datasetPath,
		OutDir:      outDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.FinalLoss >= report.InitialLoss*0.8 {
		t.Fatalf("loss did not drop enough: initial=%v final=%v", report.InitialLoss, report.FinalLoss)
	}
	if _, err := os.Stat(filepath.Join(outDir, "chat_examples.jsonl")); err != nil {
		t.Fatalf("chat examples artifact missing: %v", err)
	}
	tok := tokenizer.New()
	m, manifest, err := model.Load(outDir, tok.VocabSize())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Config.Name != "aletheia-mikros-test" {
		t.Fatalf("model name = %q", manifest.Config.Name)
	}
	r := runner.New(m, tok)
	eos, _ := tok.ID("<EOS>")
	tokens, err := r.Generate("<USER>hola<ASSISTANT>", runner.Options{
		MaxTokens:  16,
		StopTokens: []int{eos},
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := tok.Decode(tokens)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(decoded, "Hola") || strings.Contains(decoded, "<ACT_") {
		t.Fatalf("generated chat response is not clean: %s", decoded)
	}
}

func writeTrainingFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
