package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	result, err := runCandidateGreedyVsBeam(ctx)
	if err != nil {
		return BootstrapReport{}, err
	}
	return BootstrapReport{
		Suite: info,
		Cases: []CaseResult{{
			Name:                  "candidate_greedy_vs_beam",
			CandidateGreedyStatus: result.greedyStatus,
			BeamStatus:            result.beamStatus,
			Improved:              result.improved,
		}},
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
	greedyStatus string
	beamStatus   string
	improved     bool
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
		Selector:        topCandidateSelector{},
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

func passStatus(pass bool) string {
	if pass {
		return "pass"
	}
	return "failed"
}

type topCandidateSelector struct{}

func (topCandidateSelector) Select(_ selector.Snapshot, candidates []selector.Candidate) selector.Decision {
	filtered := append([]selector.Candidate(nil), candidates...)
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].LogProb > filtered[j].LogProb
	})
	for _, candidate := range filtered {
		if selector.IsFunctional(candidate.Action) {
			return selector.Decision{
				Action:     candidate.Action,
				Confidence: 1,
				Reason:     "selected highest-probability functional candidate",
				Source:     candidate.Source,
			}
		}
	}
	return selector.Decision{Action: selector.ActAbstain, Confidence: 0.1, Reason: "no functional candidate", Source: "candidate_greedy"}
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
