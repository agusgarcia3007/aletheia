package cognitivevm

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"aletheia/internal/model"
	"aletheia/internal/runner"
	"aletheia/internal/selector"
	"aletheia/internal/tokenizer"
)

func TestSolverFixesToyBugAndRecordsEvidence(t *testing.T) {
	root, taskPath, repo := writeBuggyTask(t, true)

	solver := Solver{
		DBPath:          filepath.Join(root, "memory.sqlite"),
		VerifierTimeout: 20 * time.Second,
		MaxSteps:        8,
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
	wantTrace := []ActionToken{ActionRunTests, ActionParseCode, ActionMutateCode, ActionVerify, ActionRespond}
	if len(result.Trace) != len(wantTrace) {
		t.Fatalf("trace len = %d, want %d: %+v", len(result.Trace), len(wantTrace), result.Trace)
	}
	for i, want := range wantTrace {
		if result.Trace[i].Action != want {
			t.Fatalf("trace[%d] = %s, want %s", i, result.Trace[i].Action, want)
		}
	}
}

func TestMutateCreatesCandidateWithoutWriting(t *testing.T) {
	root, _, repo := writeBuggyTask(t, true)
	state := State{
		Goal:     "Fix tests",
		RepoPath: repo,
		Success:  "go test ./...",
	}
	vm := VM{}
	if err := vm.Execute(context.Background(), &state, ActionParseCode); err != nil {
		t.Fatal(err)
	}
	if err := vm.Execute(context.Background(), &state, ActionMutateCode); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "buggy", "calculator.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "return a - b") {
		t.Fatalf("file was modified before verify:\n%s", got)
	}
	if state.CandidatePatch == nil || !strings.Contains(state.CandidatePatch.Diff, "+\treturn a + b") {
		t.Fatalf("missing candidate patch: %+v", state.CandidatePatch)
	}
}

func TestVerifyRollsBackOnFailure(t *testing.T) {
	_, _, repo := writeBuggyTask(t, false)
	state := State{
		Goal:     "Fix tests",
		RepoPath: repo,
		Success:  "go test ./...",
	}
	vm := VM{VerifierTimeout: 20 * time.Second}
	if err := vm.Execute(context.Background(), &state, ActionParseCode); err != nil {
		t.Fatal(err)
	}
	if err := vm.Execute(context.Background(), &state, ActionMutateCode); err != nil {
		t.Fatal(err)
	}
	if err := vm.Execute(context.Background(), &state, ActionVerify); err != nil {
		t.Fatal(err)
	}
	if state.Verified {
		t.Fatal("state should not be verified")
	}
	got, err := os.ReadFile(filepath.Join(repo, "calculator.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "return a - b") {
		t.Fatalf("rollback did not restore original file:\n%s", got)
	}
}

func TestModelPlannerExtractsFunctionalCandidate(t *testing.T) {
	tok := tokenizer.New()
	m, err := model.New(model.Config{
		Name:          "planner-test",
		VocabSize:     512,
		ContextLength: 64,
		NLayers:       1,
		NHeads:        2,
		DModel:        16,
		DFF:           32,
		Seed:          4,
	})
	if err != nil {
		t.Fatal(err)
	}
	runTestsID, ok := tok.ID(string(ActionRunTests))
	if !ok {
		t.Fatal("missing action token")
	}
	m.Bias[runTestsID] = 10
	planner := ModelPlanner{Runner: runner.New(m, tok), TopK: 3}
	candidates, err := planner.Candidates(context.Background(), State{Goal: "Fix the Go project so all tests pass."})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) == 0 || candidates[0].Action != selector.ActRunTests {
		t.Fatalf("candidates = %+v, want first %s", candidates, selector.ActRunTests)
	}
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeBuggyTask(t *testing.T, fixShouldPass bool) (string, string, string) {
	t.Helper()
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
	want := 5
	if !fixShouldPass {
		want = 6
	}
	writeFile(t, filepath.Join(repo, "calculator_test.go"), `package calculator

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != `+fmtInt(want)+` {
		t.Fatalf("Add(2, 3) = %d, want `+fmtInt(want)+`", got)
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
	return root, taskPath, repo
}

func fmtInt(v int) string {
	return strconv.Itoa(v)
}
