package repair

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aletheia/internal/verifier"
)

func TestExtractCounterexample(t *testing.T) {
	got, ok := ExtractCounterexample([]verifier.Evidence{
		{Verifier: verifier.GoTestName, Status: verifier.StatusPass},
		{Verifier: verifier.GoTestName, Status: verifier.StatusFail, Stderr: "\nboom\nmore\n"},
	})
	if !ok || got.Verifier != verifier.GoTestName || got.Summary != "boom" {
		t.Fatalf("counterexample = %+v ok=%v", got, ok)
	}
}

func TestBuildCandidateRepairsSimpleGoReturn(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, "calculator.go")
	if err := os.WriteFile(path, []byte("package calculator\n\nfunc Add(a,b int) int { return a - b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidate, err := BuildCandidate(repo, Counterexample{Summary: "Add returned -1"})
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Path != path || !strings.Contains(candidate.NewText, "return a + b") {
		t.Fatalf("candidate = %+v", candidate)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "return a + b") {
		t.Fatal("repair candidate should not write the file")
	}
}

func TestBuildCandidateRejectsUnknownPattern(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "calculator.go"), []byte("package calculator\n\nfunc Add(a,b int) int { return a + b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildCandidate(repo, Counterexample{}); err == nil {
		t.Fatal("expected no repair rule")
	}
}

func TestBuildCandidateRenamesKnownUndefinedFunction(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package p\n\nfunc Sum(a,b int) int { return a + b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidate, err := BuildCandidate(repo, Counterexample{Stderr: "undefined: Add"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(candidate.NewText, "func Add(") {
		t.Fatalf("candidate = %+v", candidate)
	}
}

func TestBuildCandidateAddsMissingImport(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package p\n\nfunc Trim(s string) string { return strings.TrimSpace(s) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidate, err := BuildCandidate(repo, Counterexample{Stderr: "undefined: strings"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(candidate.NewText, `import "strings"`) {
		t.Fatalf("candidate = %+v", candidate)
	}
}

func TestBuildCandidateRemovesUnusedImport(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package p\n\nimport \"fmt\"\n\nfunc Value() int { return 1 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidate, err := BuildCandidate(repo, Counterexample{Stderr: "\"fmt\" imported and not used"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(candidate.NewText, `import "fmt"`) {
		t.Fatalf("candidate = %+v", candidate)
	}
}

func TestBuildCandidateRepairsSimpleIntReturn(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package p\n\nfunc Value() int { return \"0\" }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidate, err := BuildCandidate(repo, Counterexample{Stderr: "cannot use \"0\" (untyped string constant) as int value in return statement"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(candidate.NewText, "return 0") {
		t.Fatalf("candidate = %+v", candidate)
	}
}

func TestBuildCandidateAddsNilGuard(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package p\n\nfunc Value(value *int) int { return *value }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	candidate, err := BuildCandidate(repo, Counterexample{Stderr: "panic: runtime error: invalid memory address or nil pointer dereference"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(candidate.NewText, "if value == nil") {
		t.Fatalf("candidate = %+v", candidate)
	}
}
