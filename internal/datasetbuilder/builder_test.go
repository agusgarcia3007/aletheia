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
