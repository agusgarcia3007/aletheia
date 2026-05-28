package cognitivevm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSolverFixesToyBugAndRecordsEvidence(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "buggy")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "go.mod"), "module example.com/buggy\n\ngo 1.26\n")
	writeFile(t, filepath.Join(repo, "calculator.go"), `package calculator

func Add(a, b int) int {
	return a - b
}
`)
	writeFile(t, filepath.Join(repo, "calculator_test.go"), `package calculator

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2, 3) = %d, want 5", got)
	}
}
`)

	task := Task{
		Goal:    "Fix the Go project so all tests pass.",
		Repo:    "./buggy",
		Success: "go test ./...",
	}
	taskBytes, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	taskPath := filepath.Join(root, "task.json")
	writeFile(t, taskPath, string(taskBytes))

	solver := Solver{
		DBPath:          filepath.Join(root, "memory.sqlite"),
		VerifierTimeout: 20 * time.Second,
	}
	result, err := solver.SolveFile(context.Background(), taskPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Initial.Status != "fail" {
		t.Fatalf("initial status = %q, want fail", result.Initial.Status)
	}
	if result.Final.Status != "pass" {
		t.Fatalf("final status = %q, stderr:\n%s", result.Final.Status, result.Final.Stderr)
	}
	if !strings.Contains(result.Diff, "-\treturn a - b") {
		t.Fatalf("diff does not contain removed bug:\n%s", result.Diff)
	}
	if !strings.Contains(result.Diff, "+\treturn a + b") {
		t.Fatalf("diff does not contain added fix:\n%s", result.Diff)
	}

	got, err := os.ReadFile(filepath.Join(repo, "calculator.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "return a + b") {
		t.Fatalf("solver did not patch calculator.go:\n%s", got)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
