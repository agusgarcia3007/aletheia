package datasetbuilder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildMikrosV1Dataset(t *testing.T) {
	out := filepath.Join(t.TempDir(), "mikros_v1.jsonl")
	report, err := Build(MikrosV1Profile, out)
	if err != nil {
		t.Fatal(err)
	}
	if report.Examples < 20 {
		t.Fatalf("examples = %d, want at least 20", report.Examples)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"hablame de rust", "funcion en javascript", "diferencia hay entre python y js"} {
		if !strings.Contains(text, want) {
			t.Fatalf("dataset missing %q", want)
		}
	}
}

func TestBuildMikrosLiveDatasetHasStructuredExamples(t *testing.T) {
	out := filepath.Join(t.TempDir(), "mikros_live.jsonl")
	report, err := Build(MikrosLiveProfile, out)
	if err != nil {
		t.Fatal(err)
	}
	if report.Examples < 10 {
		t.Fatalf("examples = %d, want at least 10", report.Examples)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{`"messages"`, `"slots"`, `"expected_mode":"answerer:coding"`, "como leo un csv en python", "cuanto es 17 por 23"} {
		if !strings.Contains(text, want) {
			t.Fatalf("live dataset missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "[curriculum-") {
		t.Fatalf("live dataset should not use synthetic curriculum suffixes:\n%s", text)
	}
}
