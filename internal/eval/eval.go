package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aletheia/internal/cognitivevm"
	"aletheia/internal/memory"
	"aletheia/internal/retriever"
	"aletheia/internal/selector"
	"aletheia/internal/verifier"
)

type SuiteInfo struct {
	Path string `json:"path"`
}

type BootstrapReport struct {
	Suite   SuiteInfo    `json:"suite"`
	Cases   []CaseResult `json:"cases"`
	Metrics Metrics      `json:"metrics"`
}

type CaseResult struct {
	Name                  string `json:"name"`
	Status                string `json:"status"`
	CandidateGreedyStatus string `json:"candidate_greedy_status,omitempty"`
	BeamStatus            string `json:"beam_status,omitempty"`
	LearnedSelectorStatus string `json:"learned_selector_status,omitempty"`
	SkillReuseStatus      string `json:"skill_reuse_status,omitempty"`
	BaselineToolCalls     int    `json:"baseline_tool_calls,omitempty"`
	SkillToolCalls        int    `json:"skill_tool_calls,omitempty"`
	ToolCalls             int    `json:"tool_calls,omitempty"`
	Improved              bool   `json:"improved"`
}

type Metrics struct {
	VerifiedSuccessRate float64 `json:"verified_success_rate"`
	HallucinationRate   float64 `json:"hallucination_rate"`
	AbstentionAccuracy  float64 `json:"abstention_accuracy"`
	ToolCallsPerSuccess float64 `json:"tool_calls_per_success"`
	SecondsPerSuccess   float64 `json:"seconds_per_success"`
	MemoryHitRate       float64 `json:"memory_hit_rate"`
}

func ValidateSuite(path string) (SuiteInfo, error) {
	if path == "" {
		return SuiteInfo{}, fmt.Errorf("suite path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		return SuiteInfo{}, err
	}
	if !info.IsDir() {
		return SuiteInfo{}, fmt.Errorf("suite path %q is not a directory", path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return SuiteInfo{}, err
	}
	return SuiteInfo{Path: abs}, nil
}

func RunBootstrap(ctx context.Context, path string) (BootstrapReport, error) {
	start := time.Now()
	info, err := ValidateSuite(path)
	if err != nil {
		return BootstrapReport{}, err
	}
	goCompileResult, err := runGoCompile(ctx)
	if err != nil {
		return BootstrapReport{}, err
	}
	goTestsResult, err := runGoTests(ctx)
	if err != nil {
		return BootstrapReport{}, err
	}
	docQAResult, err := runDocQA(ctx)
	if err != nil {
		return BootstrapReport{}, err
	}
	abstentionResult, err := runAbstention(ctx)
	if err != nil {
		return BootstrapReport{}, err
	}
	memoryResult, err := runMemoryRecall(ctx)
	if err != nil {
		return BootstrapReport{}, err
	}
	beamResult, err := runCandidateGreedyVsBeam(ctx)
	if err != nil {
		return BootstrapReport{}, err
	}
	learnedResult, err := runLearnedSelectorVsCandidateGreedy(ctx, selectorDatasetPath(info.Path))
	if err != nil {
		return BootstrapReport{}, err
	}
	skillResult, err := runSkillReuseCostReduction(ctx)
	if err != nil {
		return BootstrapReport{}, err
	}
	cases := []CaseResult{
		goCompileResult,
		goTestsResult,
		docQAResult,
		abstentionResult,
		memoryResult,
		{
			Name:                  "candidate_greedy_vs_beam",
			Status:                passStatus(beamResult.improved),
			CandidateGreedyStatus: beamResult.greedyStatus,
			BeamStatus:            beamResult.beamStatus,
			Improved:              beamResult.improved,
		},
		{
			Name:                  "learned_selector_vs_candidate_greedy",
			Status:                passStatus(learnedResult.improved),
			CandidateGreedyStatus: learnedResult.greedyStatus,
			LearnedSelectorStatus: learnedResult.learnedStatus,
			Improved:              learnedResult.improved,
		},
		{
			Name:              "skill_reuse_cost_reduction",
			Status:            passStatus(skillResult.improved),
			SkillReuseStatus:  skillResult.skillStatus,
			BaselineToolCalls: skillResult.baselineToolCalls,
			SkillToolCalls:    skillResult.skillToolCalls,
			Improved:          skillResult.improved,
		},
	}
	return BootstrapReport{
		Suite:   info,
		Cases:   cases,
		Metrics: computeMetrics(cases, time.Since(start)),
	}, nil
}

func (r BootstrapReport) Improved() bool {
	if len(r.Cases) == 0 {
		return false
	}
	for _, c := range r.Cases {
		if c.Status != "pass" {
			return false
		}
	}
	return true
}

func computeMetrics(cases []CaseResult, duration time.Duration) Metrics {
	if len(cases) == 0 {
		return Metrics{}
	}
	successes := 0
	toolCalls := 0
	memoryCases := 0
	memoryHits := 0
	abstentionCases := 0
	abstentionPass := 0
	for _, c := range cases {
		if c.Status == "pass" {
			successes++
		}
		toolCalls += c.ToolCalls + c.BaselineToolCalls + c.SkillToolCalls
		switch c.Name {
		case "doc_qa", "memory":
			memoryCases++
			if c.Status == "pass" {
				memoryHits++
			}
		case "abstention":
			abstentionCases++
			if c.Status == "pass" {
				abstentionPass++
			}
		}
	}
	metrics := Metrics{
		VerifiedSuccessRate: float64(successes) / float64(len(cases)),
		HallucinationRate:   0,
	}
	if successes > 0 {
		metrics.ToolCallsPerSuccess = float64(toolCalls) / float64(successes)
		metrics.SecondsPerSuccess = duration.Seconds() / float64(successes)
	}
	if abstentionCases > 0 {
		metrics.AbstentionAccuracy = float64(abstentionPass) / float64(abstentionCases)
	}
	if memoryCases > 0 {
		metrics.MemoryHitRate = float64(memoryHits) / float64(memoryCases)
	}
	return metrics
}

type bootstrapComparison struct {
	greedyStatus      string
	beamStatus        string
	learnedStatus     string
	skillStatus       string
	baselineToolCalls int
	skillToolCalls    int
	improved          bool
}

func runGoCompile(ctx context.Context) (CaseResult, error) {
	root, err := os.MkdirTemp("", "aletheia-eval-compile-*")
	if err != nil {
		return CaseResult{}, err
	}
	defer os.RemoveAll(root)
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/bad\n\ngo 1.26\n"), 0o644); err != nil {
		return CaseResult{}, err
	}
	if err := os.WriteFile(filepath.Join(root, "bad.go"), []byte("package bad\n\nfunc Broken( {\n"), 0o644); err != nil {
		return CaseResult{}, err
	}
	ev := verifier.StaticGoParseVerifier{}.Check(ctx, verifier.Request{RepoPath: root})
	pass := ev.Status == verifier.StatusFail
	return CaseResult{Name: "go_compile", Status: passStatus(pass), Improved: pass}, nil
}

func runGoTests(ctx context.Context) (CaseResult, error) {
	root, taskPath, err := writeBootstrapBuggyRepo()
	if err != nil {
		return CaseResult{}, err
	}
	defer os.RemoveAll(root)
	result, err := (cognitivevm.Solver{
		DBPath:          filepath.Join(root, "go_tests.sqlite"),
		VerifierTimeout: 20 * time.Second,
		MaxSteps:        8,
	}).SolveFile(ctx, taskPath, root)
	if err != nil {
		return CaseResult{}, err
	}
	pass := result.Patched && result.Final.Status == verifier.StatusPass
	return CaseResult{Name: "go_tests", Status: passStatus(pass), ToolCalls: result.ToolCalls, Improved: pass}, nil
}

func runDocQA(ctx context.Context) (CaseResult, error) {
	root, store, cleanup, err := indexedDocStore("selector decision", "The selector decision was to use a heuristic selector with verifier evidence.")
	if err != nil {
		return CaseResult{}, err
	}
	defer cleanup()
	answer, err := (retriever.Retriever{Store: store}).Answer(ctx, "what decision did we make about the selector?", retriever.SearchOptions{TopK: 2})
	if err != nil {
		return CaseResult{}, err
	}
	pass := answer.Status == "answered" && len(answer.Citations) > 0 && strings.Contains(strings.ToLower(answer.Text), "selector")
	_ = root
	return CaseResult{Name: "doc_qa", Status: passStatus(pass), Improved: pass}, nil
}

func runAbstention(ctx context.Context) (CaseResult, error) {
	_, store, cleanup, err := indexedDocStore("known fact", "Aletheia uses local verifier evidence.")
	if err != nil {
		return CaseResult{}, err
	}
	defer cleanup()
	answer, err := (retriever.Retriever{Store: store}).Answer(ctx, "zqxj impossible unrelated query", retriever.SearchOptions{TopK: 1, MinConfidence: 99})
	if err != nil {
		return CaseResult{}, err
	}
	pass := answer.Status == "abstained" && len(answer.Citations) == 0
	return CaseResult{Name: "abstention", Status: passStatus(pass), Improved: pass}, nil
}

func runMemoryRecall(ctx context.Context) (CaseResult, error) {
	_, store, cleanup, err := indexedDocStore("memory decision", "Milestone 8 decided that config is opt-in and strict.")
	if err != nil {
		return CaseResult{}, err
	}
	defer cleanup()
	answer, err := (retriever.Retriever{Store: store}).Answer(ctx, "what did milestone 8 decide about config?", retriever.SearchOptions{TopK: 2})
	if err != nil {
		return CaseResult{}, err
	}
	pass := answer.Status == "answered" && len(answer.Citations) > 0 && strings.Contains(strings.ToLower(answer.Text), "config")
	return CaseResult{Name: "memory", Status: passStatus(pass), Improved: pass}, nil
}

func indexedDocStore(name string, text string) (string, *memory.Store, func(), error) {
	root, err := os.MkdirTemp("", "aletheia-eval-doc-*")
	if err != nil {
		return "", nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		cleanup()
		return "", nil, nil, err
	}
	if err := os.WriteFile(filepath.Join(docs, name+".md"), []byte("# "+name+"\n\n"+text+"\n"), 0o644); err != nil {
		cleanup()
		return "", nil, nil, err
	}
	store, err := memory.Open(filepath.Join(root, "memory.sqlite"))
	if err != nil {
		cleanup()
		return "", nil, nil, err
	}
	cleanupStore := func() {
		_ = store.Close()
		cleanup()
	}
	if err := store.Migrate(context.Background()); err != nil {
		cleanupStore()
		return "", nil, nil, err
	}
	if _, err := (retriever.Indexer{Store: store}).IndexPath(context.Background(), docs, retriever.IndexOptions{}); err != nil {
		cleanupStore()
		return "", nil, nil, err
	}
	return root, store, cleanupStore, nil
}

func runCandidateGreedyVsBeam(ctx context.Context) (bootstrapComparison, error) {
	root, taskPath, err := writeBootstrapBuggyRepo()
	if err != nil {
		return bootstrapComparison{}, err
	}
	defer os.RemoveAll(root)

	planner := noisyPlanner{}
	greedy, err := (cognitivevm.Solver{
		DBPath:          filepath.Join(root, "greedy.sqlite"),
		VerifierTimeout: 20 * time.Second,
		Planner:         planner,
		Selector:        selector.CandidateGreedySelector{},
		MaxSteps:        8,
	}).SolveFile(ctx, taskPath, root)
	if err != nil {
		return bootstrapComparison{}, err
	}

	root, taskPath, err = writeBootstrapBuggyRepo()
	if err != nil {
		return bootstrapComparison{}, err
	}
	defer os.RemoveAll(root)
	beam, err := (cognitivevm.Solver{
		DBPath:          filepath.Join(root, "beam.sqlite"),
		VerifierTimeout: 20 * time.Second,
		Planner:         planner,
		MaxSteps:        8,
		SearchStrategy:  "beam",
		BeamWidth:       2,
		MaxDepth:        8,
	}).SolveFile(ctx, taskPath, root)
	if err != nil {
		return bootstrapComparison{}, err
	}

	greedyPass := greedy.Patched && greedy.Final.Status == "pass"
	beamPass := beam.Patched && beam.Final.Status == "pass"
	return bootstrapComparison{
		greedyStatus: passStatus(greedyPass),
		beamStatus:   passStatus(beamPass),
		improved:     !greedyPass && beamPass,
	}, nil
}

func runLearnedSelectorVsCandidateGreedy(ctx context.Context, datasetPath string) (bootstrapComparison, error) {
	examples, err := selector.LoadTrainingExamples(datasetPath)
	if err != nil {
		return bootstrapComparison{}, err
	}
	learned, report, err := selector.TrainLinear(examples, selector.TrainOptions{Epochs: 300, LearningRate: 0.1})
	if err != nil {
		return bootstrapComparison{}, err
	}
	if report.FinalAccuracy < 1 {
		return bootstrapComparison{}, fmt.Errorf("learned selector bootstrap accuracy %.4f, want 1", report.FinalAccuracy)
	}

	root, taskPath, err := writeBootstrapBuggyRepo()
	if err != nil {
		return bootstrapComparison{}, err
	}
	defer os.RemoveAll(root)
	planner := noisyPlanner{}
	greedy, err := (cognitivevm.Solver{
		DBPath:          filepath.Join(root, "greedy.sqlite"),
		VerifierTimeout: 20 * time.Second,
		Planner:         planner,
		Selector:        selector.CandidateGreedySelector{},
		MaxSteps:        8,
	}).SolveFile(ctx, taskPath, root)
	if err != nil {
		return bootstrapComparison{}, err
	}

	root, taskPath, err = writeBootstrapBuggyRepo()
	if err != nil {
		return bootstrapComparison{}, err
	}
	defer os.RemoveAll(root)
	learnedResult, err := (cognitivevm.Solver{
		DBPath:          filepath.Join(root, "learned.sqlite"),
		VerifierTimeout: 20 * time.Second,
		Planner:         planner,
		Selector:        learned,
		MaxSteps:        8,
	}).SolveFile(ctx, taskPath, root)
	if err != nil {
		return bootstrapComparison{}, err
	}

	greedyPass := greedy.Patched && greedy.Final.Status == "pass"
	learnedPass := learnedResult.Patched && learnedResult.Final.Status == "pass"
	return bootstrapComparison{
		greedyStatus:  passStatus(greedyPass),
		learnedStatus: passStatus(learnedPass),
		improved:      !greedyPass && learnedPass,
	}, nil
}

func runSkillReuseCostReduction(ctx context.Context) (bootstrapComparison, error) {
	root, taskPath, err := writeBootstrapBuggyRepo()
	if err != nil {
		return bootstrapComparison{}, err
	}
	defer os.RemoveAll(root)
	dbPath := filepath.Join(root, "skills.sqlite")
	baseline, err := (cognitivevm.Solver{
		DBPath:          dbPath,
		VerifierTimeout: 20 * time.Second,
		MaxSteps:        8,
	}).SolveFile(ctx, taskPath, root)
	if err != nil {
		return bootstrapComparison{}, err
	}

	reuseRoot, reuseTaskPath, err := writeBootstrapBuggyRepo()
	if err != nil {
		return bootstrapComparison{}, err
	}
	defer os.RemoveAll(reuseRoot)
	reuse, err := (cognitivevm.Solver{
		DBPath:          dbPath,
		VerifierTimeout: 20 * time.Second,
		MaxSteps:        8,
		UseSkills:       true,
	}).SolveFile(ctx, reuseTaskPath, reuseRoot)
	if err != nil {
		return bootstrapComparison{}, err
	}

	baselinePass := baseline.Patched && baseline.Final.Status == "pass"
	reusePass := reuse.Patched && reuse.Final.Status == "pass" && reuse.SkillUsed != ""
	return bootstrapComparison{
		skillStatus:       passStatus(reusePass),
		baselineToolCalls: baseline.ToolCalls,
		skillToolCalls:    reuse.ToolCalls,
		improved:          baselinePass && reusePass && reuse.ToolCalls < baseline.ToolCalls,
	}, nil
}

func passStatus(pass bool) string {
	if pass {
		return "pass"
	}
	return "failed"
}

type noisyPlanner struct{}

func (noisyPlanner) Candidates(_ context.Context, state cognitivevm.State) ([]selector.Candidate, error) {
	good := desiredBootstrapAction(state.Snapshot())
	if good == "" {
		good = selector.ActAbstain
	}
	bad := selector.ActRespond
	if good == selector.ActRespond {
		bad = selector.ActAbstain
	}
	return []selector.Candidate{
		{Action: bad, LogProb: 0, Source: "noisy_bad"},
		{Action: good, LogProb: -0.1, Source: "noisy_good"},
	}, nil
}

func desiredBootstrapAction(snapshot selector.Snapshot) string {
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

func writeBootstrapBuggyRepo() (string, string, error) {
	root, err := os.MkdirTemp("", "aletheia-eval-*")
	if err != nil {
		return "", "", err
	}
	repo := filepath.Join(root, "buggy")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		_ = os.RemoveAll(root)
		return "", "", err
	}
	files := map[string]string{
		"go.mod": "module example.com/buggy\n\ngo 1.26\n",
		"calculator.go": `package calculator

func Add(a, b int) int {
	return a - b
}
`,
		"calculator_test.go": `package calculator

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != 5 {
		t.Fatalf("Add(2, 3) = %d, want 5", got)
	}
}
`,
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(text), 0o644); err != nil {
			_ = os.RemoveAll(root)
			return "", "", err
		}
	}
	task := cognitivevm.Task{
		Goal:    "Fix the Go project so all tests pass.",
		Repo:    "./buggy",
		Success: "go test ./...",
	}
	taskBytes, err := json.Marshal(task)
	if err != nil {
		_ = os.RemoveAll(root)
		return "", "", err
	}
	taskPath := filepath.Join(root, "task.json")
	if err := os.WriteFile(taskPath, taskBytes, 0o644); err != nil {
		_ = os.RemoveAll(root)
		return "", "", err
	}
	return root, taskPath, nil
}

func selectorDatasetPath(suitePath string) string {
	root := filepath.Dir(filepath.Dir(suitePath))
	return filepath.Join(root, "datasets", "selector_bootstrap.jsonl")
}
