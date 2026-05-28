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

	"aletheia/internal/memory"
	"aletheia/internal/model"
	"aletheia/internal/runner"
	"aletheia/internal/selector"
	"aletheia/internal/skills"
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
	if len(result.Trace[0].Verifiers) == 0 || result.Trace[0].Verifiers[0] != "go_test" {
		t.Fatalf("trace missing verifier names: %+v", result.Trace[0])
	}
	if result.Trace[1].VerifierStatus != "" || len(result.Trace[1].Verifiers) != 0 {
		t.Fatalf("non-verifier trace entry should not inherit verifier evidence: %+v", result.Trace[1])
	}
	store, err := memory.Open(filepath.Join(root, "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rows, err := store.EvidenceByVerifier(context.Background(), 1, "go_test")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("go_test evidence rows = %d, want 2", len(rows))
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(rows[0].Payload), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["action"] != string(ActionRunTests) || payload["command"] != "go test ./..." {
		t.Fatalf("unexpected payload: %v", payload)
	}
	for _, typ := range []string{"test_failure", "repair_attempt", "patch_candidate", "verified_patch"} {
		count, err := store.NodeCountByType(context.Background(), typ)
		if err != nil {
			t.Fatal(err)
		}
		if count == 0 {
			t.Fatalf("causal node type %s was not recorded", typ)
		}
	}
}

func TestSolverWithStaticParseAndGoTestVerifiers(t *testing.T) {
	root, taskPath, _ := writeBuggyTask(t, true)
	solver := Solver{
		DBPath:          filepath.Join(root, "memory.sqlite"),
		VerifierTimeout: 20 * time.Second,
		MaxSteps:        8,
		VerifierNames:   []string{"static_go_parse", "go_test"},
	}
	result, err := solver.SolveFile(context.Background(), taskPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Patched || result.Final.Status != "pass" {
		t.Fatalf("result = %+v", result)
	}
	if result.Final.Verifier != "bus" {
		t.Fatalf("final verifier = %s, want bus", result.Final.Verifier)
	}
}

func TestSolverBeamFixesToyBugAndRecordsTrajectory(t *testing.T) {
	root, taskPath, repo := writeBuggyTask(t, true)
	dbPath := filepath.Join(root, "memory.sqlite")
	solver := Solver{
		DBPath:          dbPath,
		VerifierTimeout: 20 * time.Second,
		MaxSteps:        8,
		SearchStrategy:  "beam",
		BeamWidth:       4,
		MaxDepth:        8,
		VerifierNames:   []string{"static_go_parse", "go_test"},
	}
	result, err := solver.SolveFile(context.Background(), taskPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Patched || result.Final.Status != "pass" {
		t.Fatalf("result = %+v", result)
	}
	if result.Initial.Status != "fail" {
		t.Fatalf("initial status = %q, want fail", result.Initial.Status)
	}
	if len(result.Trace) == 0 || result.Trace[0].Action != ActionRunTests {
		t.Fatalf("trace should start with initial verifier evidence: %+v", result.Trace)
	}
	for _, entry := range result.Trace {
		if (entry.Action == ActionParseCode || entry.Action == ActionMutateCode) && (entry.VerifierStatus != "" || len(entry.Verifiers) != 0) {
			t.Fatalf("non-verifier beam trace entry inherited verifier evidence: %+v", entry)
		}
	}
	got, err := os.ReadFile(filepath.Join(repo, "calculator.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "return a + b") {
		t.Fatalf("beam did not apply verified patch to original repo:\n%s", got)
	}
	store, err := memory.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	stats, err := store.Inspect(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Nodes == 0 || stats.Edges == 0 {
		t.Fatalf("trajectory graph was not recorded: %+v", stats)
	}
	selectorExamples, err := store.NodeCountByType(context.Background(), "selector_example")
	if err != nil {
		t.Fatal(err)
	}
	if selectorExamples == 0 {
		t.Fatal("beam did not record selector examples")
	}
	edges, err := store.GraphEdges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var selected int
	for _, edge := range edges {
		if edge.Relation == "selected" {
			selected++
		}
	}
	if selected == 0 {
		t.Fatalf("selected trajectory edges missing: %+v", edges)
	}
}

func TestSolverMCTSFixesToyBugAndRecordsTrajectory(t *testing.T) {
	root, taskPath, repo := writeBuggyTask(t, true)
	dbPath := filepath.Join(root, "memory.sqlite")
	solver := Solver{
		DBPath:          dbPath,
		VerifierTimeout: 20 * time.Second,
		Planner:         noisyActionPlanner{},
		MaxSteps:        8,
		SearchStrategy:  "mcts",
		BeamWidth:       2,
		MaxDepth:        8,
		VerifierNames:   []string{"static_go_parse", "go_test"},
	}
	result, err := solver.SolveFile(context.Background(), taskPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Patched || result.Final.Status != "pass" {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Trace) == 0 || result.Trace[0].Source != "mcts" {
		t.Fatalf("trace should start with mcts verifier evidence: %+v", result.Trace)
	}
	got, err := os.ReadFile(filepath.Join(repo, "calculator.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "return a + b") {
		t.Fatalf("mcts did not apply verified patch to original repo:\n%s", got)
	}
	store, err := memory.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	trajectories, err := store.NodeCountByType(context.Background(), "trajectory_state")
	if err != nil {
		t.Fatal(err)
	}
	if trajectories == 0 {
		t.Fatal("mcts did not record trajectory states")
	}
}

func TestSolverLearnedSelectorBeatsCandidateGreedyWithoutBeam(t *testing.T) {
	root, taskPath, _ := writeBuggyTask(t, true)
	greedy := Solver{
		DBPath:          filepath.Join(root, "greedy.sqlite"),
		VerifierTimeout: 20 * time.Second,
		Planner:         noisyActionPlanner{},
		Selector:        selector.CandidateGreedySelector{},
		MaxSteps:        8,
	}
	greedyResult, err := greedy.SolveFile(context.Background(), taskPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if greedyResult.Patched {
		t.Fatalf("candidate greedy unexpectedly patched repo: %+v", greedyResult)
	}

	root, taskPath, repo := writeBuggyTask(t, true)
	learned := learnedSelectorForTest(t)
	solver := Solver{
		DBPath:          filepath.Join(root, "learned.sqlite"),
		VerifierTimeout: 20 * time.Second,
		Planner:         noisyActionPlanner{},
		Selector:        learned,
		MaxSteps:        8,
		VerifierNames:   []string{"static_go_parse", "go_test"},
	}
	result, err := solver.SolveFile(context.Background(), taskPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Patched || result.Final.Status != "pass" {
		t.Fatalf("learned selector result = %+v", result)
	}
	for _, entry := range result.Trace {
		if (entry.Action == ActionParseCode || entry.Action == ActionMutateCode) && (entry.VerifierStatus != "" || len(entry.Verifiers) != 0) {
			t.Fatalf("non-verifier learned-selector trace entry inherited verifier evidence: %+v", entry)
		}
	}
	got, err := os.ReadFile(filepath.Join(repo, "calculator.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "return a + b") {
		t.Fatalf("learned selector did not patch repo:\n%s", got)
	}
}

func TestSolverCompressesAndReusesSkillWithLowerCost(t *testing.T) {
	root, taskPath, _ := writeBuggyTask(t, true)
	dbPath := filepath.Join(root, "memory.sqlite")
	first := Solver{
		DBPath:          dbPath,
		VerifierTimeout: 20 * time.Second,
		MaxSteps:        8,
	}
	firstResult, err := first.SolveFile(context.Background(), taskPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if !firstResult.Patched || firstResult.ToolCalls != 2 {
		t.Fatalf("first result = %+v", firstResult)
	}
	store, err := memory.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	skillList, err := store.ListSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	if len(skillList) != 1 || skillList[0].Name != skills.FixSimpleGoTestFailure {
		t.Fatalf("skills = %+v", skillList)
	}

	secondRoot, secondTaskPath, repo := writeBuggyTask(t, true)
	second := Solver{
		DBPath:          dbPath,
		VerifierTimeout: 20 * time.Second,
		MaxSteps:        8,
		UseSkills:       true,
	}
	secondResult, err := second.SolveFile(context.Background(), secondTaskPath, secondRoot)
	if err != nil {
		t.Fatal(err)
	}
	if secondResult.SkillUsed != skills.FixSimpleGoTestFailure || !secondResult.InitialSkipped {
		t.Fatalf("skill was not used: %+v", secondResult)
	}
	if !secondResult.Patched || secondResult.Final.Status != "pass" {
		t.Fatalf("second result = %+v", secondResult)
	}
	if secondResult.ToolCalls >= firstResult.ToolCalls {
		t.Fatalf("tool calls = %d, want less than %d", secondResult.ToolCalls, firstResult.ToolCalls)
	}
	if len(secondResult.Trace) == 0 || secondResult.Trace[0].Action != ActionParseCode {
		t.Fatalf("skill trace = %+v", secondResult.Trace)
	}
	got, err := os.ReadFile(filepath.Join(repo, "calculator.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "return a + b") {
		t.Fatalf("skill did not patch repo:\n%s", got)
	}
}

func TestSolverSkillFailureFallsBackAndDisablesSkill(t *testing.T) {
	root, taskPath, repo := writeBuggyTask(t, true)
	dbPath := filepath.Join(root, "memory.sqlite")
	store, err := memory.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertSkill(context.Background(), memory.Skill{
		Name:           skills.FixSimpleGoTestFailure,
		Trigger:        skills.TriggerCalculatorSub,
		ActionSequence: []string{selector.ActParseCode},
		SuccessRate:    1,
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	result, err := (Solver{
		DBPath:          dbPath,
		VerifierTimeout: 20 * time.Second,
		MaxSteps:        8,
		UseSkills:       true,
	}).SolveFile(context.Background(), taskPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if result.SkillUsed != "" || !result.Patched || result.Final.Status != "pass" {
		t.Fatalf("fallback result = %+v", result)
	}
	got, err := os.ReadFile(filepath.Join(repo, "calculator.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "return a + b") {
		t.Fatalf("fallback did not patch repo:\n%s", got)
	}
	store, err = memory.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	skill, ok, err := store.BestSkillByTrigger(context.Background(), skills.TriggerCalculatorSub)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || skill.SuccessRate != 0 {
		t.Fatalf("skill after failure = %+v ok=%v", skill, ok)
	}
}

func TestSolverFailedSkillRestoresRepoBeforeFallback(t *testing.T) {
	root, taskPath, repo := writeBuggyTask(t, false)
	dbPath := filepath.Join(root, "memory.sqlite")
	store, err := memory.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertSkill(context.Background(), memory.Skill{
		Name:    skills.FixSimpleGoTestFailure,
		Trigger: skills.TriggerCalculatorSub,
		ActionSequence: []string{
			selector.ActParseCode,
			selector.ActMutateCode,
			selector.ActVerify,
			selector.ActRespond,
		},
		SuccessRate: 1,
	}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()

	result, err := (Solver{
		DBPath:          dbPath,
		VerifierTimeout: 20 * time.Second,
		MaxSteps:        8,
		UseSkills:       true,
	}).SolveFile(context.Background(), taskPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if result.SkillUsed != "" || result.Patched || result.Final.Status == "pass" {
		t.Fatalf("fallback should remain unverified after failed skill: %+v", result)
	}
	got, err := os.ReadFile(filepath.Join(repo, "calculator.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "return a - b") {
		t.Fatalf("failed skill or fallback left partial mutation:\n%s", got)
	}
	store, err = memory.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	skill, ok, err := store.BestSkillByTrigger(context.Background(), skills.TriggerCalculatorSub)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || skill.SuccessRate != 0 {
		t.Fatalf("skill after failed verifier = %+v ok=%v", skill, ok)
	}
}

func TestSolverBeamLeavesOriginalRepoUntouchedWithoutVerifiedSolution(t *testing.T) {
	root, taskPath, repo := writeBuggyTask(t, false)
	solver := Solver{
		DBPath:          filepath.Join(root, "memory.sqlite"),
		VerifierTimeout: 20 * time.Second,
		MaxSteps:        5,
		SearchStrategy:  "beam",
		BeamWidth:       3,
		MaxDepth:        5,
	}
	result, err := solver.SolveFile(context.Background(), taskPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if result.Patched || result.Final.Status == "pass" {
		t.Fatalf("unexpected verified result = %+v", result)
	}
	got, err := os.ReadFile(filepath.Join(repo, "calculator.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "return a - b") {
		t.Fatalf("unverified beam branch mutated original repo:\n%s", got)
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

func TestRunCmdRecordsAllowlistedEvidence(t *testing.T) {
	root, _, repo := writeBuggyTask(t, true)
	dbPath := filepath.Join(root, "memory.sqlite")
	store, err := memory.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	episodeID, err := store.CreateEpisode(context.Background(), "run safe command")
	if err != nil {
		t.Fatal(err)
	}
	state := State{
		Goal:     "Run status",
		RepoPath: repo,
		Success:  "ls",
	}
	vm := VM{Store: store, EpisodeID: episodeID, VerifierTimeout: 20 * time.Second}
	if err := vm.Execute(context.Background(), &state, ActionRunCmd); err != nil {
		t.Fatal(err)
	}
	if state.ToolCalls != 1 || state.FinalStatus != "pass" {
		t.Fatalf("state = %+v", state)
	}
	rows, err := store.EvidenceByVerifier(context.Background(), episodeID, "run_cmd")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Status != "pass" || rows[0].Payload == "" {
		t.Fatalf("run_cmd evidence = %+v", rows)
	}
}

func TestFindCounterexampleAndRepairCreateCandidateWithoutWriting(t *testing.T) {
	root, _, repo := writeBuggyTask(t, true)
	state := State{
		Goal:     "Fix tests",
		RepoPath: repo,
		Success:  "go test ./...",
	}
	vm := VM{VerifierTimeout: 20 * time.Second}
	if err := vm.Execute(context.Background(), &state, ActionRunTests); err != nil {
		t.Fatal(err)
	}
	if err := vm.Execute(context.Background(), &state, ActionFindCounterexample); err != nil {
		t.Fatal(err)
	}
	if state.Counterexample == nil || state.FinalStatus != "counterexample_found" {
		t.Fatalf("counterexample state = %+v", state)
	}
	if err := vm.Execute(context.Background(), &state, ActionRepair); err != nil {
		t.Fatal(err)
	}
	if state.CandidatePatch == nil || !strings.Contains(state.Diff, "+\treturn a + b") {
		t.Fatalf("repair candidate missing: %+v", state.CandidatePatch)
	}
	got, err := os.ReadFile(filepath.Join(root, "buggy", "calculator.go"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "return a + b") {
		t.Fatalf("repair wrote before verify:\n%s", got)
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

type noisyActionPlanner struct{}

func (noisyActionPlanner) Candidates(_ context.Context, state State) ([]selector.Candidate, error) {
	good := desiredActionForTest(state.Snapshot())
	if good == "" {
		good = selector.ActAbstain
	}
	bad := selector.ActRespond
	if good == selector.ActRespond {
		bad = selector.ActAbstain
	}
	out := []selector.Candidate{
		{Action: bad, LogProb: 0, Source: "noisy_bad"},
		{Action: good, LogProb: -0.1, Source: "noisy_good"},
	}
	for _, action := range []string{selector.ActRunTests, selector.ActParseCode, selector.ActMutateCode, selector.ActVerify, selector.ActRespond, selector.ActAbstain} {
		if action == good || action == bad {
			continue
		}
		out = append(out, selector.Candidate{Action: action, LogProb: -0.5, Source: "noisy"})
	}
	return out, nil
}

func desiredActionForTest(snapshot selector.Snapshot) string {
	if snapshot.Completed {
		return ""
	}
	if snapshot.Verified || snapshot.LastVerifierStatus == "pass" {
		return selector.ActRespond
	}
	if !snapshot.HasRunTests {
		return selector.ActRunTests
	}
	if snapshot.LastVerifierStatus == "fail" && !snapshot.Parsed {
		return selector.ActParseCode
	}
	if snapshot.Parsed && snapshot.PatternFound && !snapshot.HasCandidatePatch {
		return selector.ActMutateCode
	}
	if snapshot.HasCandidatePatch && !snapshot.Verified {
		return selector.ActVerify
	}
	return selector.ActAbstain
}

func learnedSelectorForTest(t *testing.T) selector.LinearSelector {
	t.Helper()
	model, report, err := selector.TrainLinear([]selector.TrainingExample{
		{Snapshot: selector.Snapshot{MaxToolCalls: 8}, Candidates: noisyCandidatesForTest(selector.ActRunTests), Chosen: selector.ActRunTests, Reward: 1},
		{Snapshot: selector.Snapshot{HasRunTests: true, LastVerifierStatus: "fail", ToolCalls: 1, MaxToolCalls: 8}, Candidates: noisyCandidatesForTest(selector.ActParseCode), Chosen: selector.ActParseCode, Reward: 1},
		{Snapshot: selector.Snapshot{HasRunTests: true, LastVerifierStatus: "fail", Parsed: true, PatternFound: true, ToolCalls: 1, MaxToolCalls: 8}, Candidates: noisyCandidatesForTest(selector.ActMutateCode), Chosen: selector.ActMutateCode, Reward: 1},
		{Snapshot: selector.Snapshot{HasRunTests: true, LastVerifierStatus: "fail", Parsed: true, PatternFound: true, HasCandidatePatch: true, ToolCalls: 1, MaxToolCalls: 8}, Candidates: noisyCandidatesForTest(selector.ActVerify), Chosen: selector.ActVerify, Reward: 1},
		{Snapshot: selector.Snapshot{HasRunTests: true, LastVerifierStatus: "pass", ToolCalls: 1, MaxToolCalls: 8}, Candidates: noisyCandidatesForTest(selector.ActRespond), Chosen: selector.ActRespond, Reward: 1},
		{Snapshot: selector.Snapshot{Verified: true, ToolCalls: 2, MaxToolCalls: 8}, Candidates: noisyCandidatesForTest(selector.ActRespond), Chosen: selector.ActRespond, Reward: 1},
	}, selector.TrainOptions{Epochs: 300, LearningRate: 0.1})
	if err != nil {
		t.Fatal(err)
	}
	if report.FinalAccuracy < 1 {
		t.Fatalf("learned selector accuracy = %.4f", report.FinalAccuracy)
	}
	return model
}

func noisyCandidatesForTest(good string) []selector.Candidate {
	bad := selector.ActRespond
	if good == selector.ActRespond {
		bad = selector.ActAbstain
	}
	out := []selector.Candidate{
		{Action: bad, LogProb: 0, Source: "noisy_bad"},
		{Action: good, LogProb: -0.1, Source: "noisy_good"},
	}
	for _, action := range []string{selector.ActRunTests, selector.ActParseCode, selector.ActMutateCode, selector.ActVerify, selector.ActRespond, selector.ActAbstain} {
		if action == good || action == bad {
			continue
		}
		out = append(out, selector.Candidate{Action: action, LogProb: -0.5, Source: "noisy"})
	}
	return out
}
