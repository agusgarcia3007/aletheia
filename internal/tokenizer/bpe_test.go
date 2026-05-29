package tokenizer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTrainBPEFromJSONL(t *testing.T) {
	dir := t.TempDir()
	dataset := filepath.Join(dir, "dataset.jsonl")
	if err := os.WriteFile(dataset, []byte(`{"prompt":"<USER>hablame de rust<ASSISTANT>","completion":"Rust es seguro y rapido<EOS>"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "tokenizer.json")
	artifact, err := TrainBPEFromJSONL(dataset, out, 128)
	if err != nil {
		t.Fatal(err)
	}
	if artifact.VocabSize == 0 || artifact.Type == "" {
		t.Fatalf("artifact = %+v", artifact)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "rust") {
		t.Fatalf("artifact missing learned token: %s", string(raw))
	}
}
