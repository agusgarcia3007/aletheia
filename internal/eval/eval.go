package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"aletheia/internal/cognitivevm"
	"aletheia/internal/selector"
)

type SuiteInfo struct {
	Path string
}

type BootstrapReport struct {
	Suite SuiteInfo
	Cases []CaseResult
}

type CaseResult struct {
	Name                  string
	CandidateGreedyStatus string
	BeamStatus            string
	LearnedSelectorStatus string
	SkillReuseStatus      string
	BaselineToolCalls     int
	SkillToolCalls        int
	Improved              bool
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
	info, err := ValidateSuite(path)
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
	return BootstrapReport{
		Suite: info,
		Cases: []CaseResult{
			{
				Name:                  "candidate_greedy_vs_beam",
				CandidateGreedyStatus: beamResult.greedyStatus,
				BeamStatus:            beamResult.beamStatus,
				Improved:              beamResult.improved,
			},
			{
				Name:                  "learned_selector_vs_candidate_greedy",
				CandidateGreedyStatus: learnedResult.greedyStatus,
				LearnedSelectorStatus: learnedResult.learnedStatus,
				Improved:              learnedResult.improved,
			},
			{
				Name:              "skill_reuse_cost_reduction",
				SkillReuseStatus:  skillResult.skillStatus,
				BaselineToolCalls: skillResult.baselineToolCalls,
				SkillToolCalls:    skillResult.skillToolCalls,
				Improved:          skillResult.improved,
			},
		},
	}, nil
}

func (r BootstrapReport) Improved() bool {
	if len(r.Cases) == 0 {
		return false
	}
	for _, c := range r.Cases {
		if !c.Improved {
			return false
		}
	}
	return true
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
