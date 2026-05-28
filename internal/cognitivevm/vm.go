package cognitivevm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aletheia/internal/memory"
	"aletheia/internal/runner"
	"aletheia/internal/selector"
	"aletheia/internal/verifier"
)

type ActionToken string

const (
	ActionRunTests   ActionToken = selector.ActRunTests
	ActionParseCode  ActionToken = selector.ActParseCode
	ActionMutateCode ActionToken = selector.ActMutateCode
	ActionVerify     ActionToken = selector.ActVerify
	ActionRespond    ActionToken = selector.ActRespond
	ActionAbstain    ActionToken = selector.ActAbstain
)

type Budget struct {
	Tokens    int
	Seconds   int
	ToolCalls int
}

type State struct {
	Goal            string
	RepoPath        string
	Success         string
	Budget          Budget
	WorkingMemory   []string
	Evidence        []verifier.Evidence
	VerifierResults []verifier.Evidence
	CandidatePatch  *PatchCandidate
	ActionTrace     []TraceEntry
	Uncertainty     float64
	Risk            string
	FinalStatus     string
	Diff            string
	Completed       bool
	HasRunTests     bool
	Parsed          bool
	PatternFound    bool
	Verified        bool
	ToolCalls       int
}

type PatchCandidate struct {
	Path    string
	OldText string
	NewText string
	Diff    string
}

type TraceEntry struct {
	Step           int
	Action         ActionToken
	Reason         string
	Source         string
	Status         string
	VerifierStatus string
}

type Planner interface {
	Candidates(ctx context.Context, state State) ([]selector.Candidate, error)
}

type ActionSelector interface {
	Select(snapshot selector.Snapshot, candidates []selector.Candidate) selector.Decision
}

type VM struct {
	Store           *memory.Store
	EpisodeID       int64
	VerifierTimeout time.Duration
}

func (vm VM) Run(ctx context.Context, task Task, repoPath string, planner Planner, actionSelector ActionSelector, maxSteps int) (State, error) {
	if planner == nil {
		planner = MockPlanner{}
	}
	if actionSelector == nil {
		actionSelector = selector.HeuristicSelector{}
	}
	if maxSteps <= 0 {
		maxSteps = 8
	}
	state := State{
		Goal:     task.Goal,
		RepoPath: repoPath,
		Success:  task.Success,
		Budget: Budget{
			Tokens:    4000,
			Seconds:   120,
			ToolCalls: maxSteps,
		},
		Risk: "low",
	}

	for step := 0; step < maxSteps && !state.Completed; step++ {
		candidates, err := planner.Candidates(ctx, state)
		if err != nil {
			return state, err
		}
		decision := actionSelector.Select(state.Snapshot(), candidates)
		action, err := ParseAction(decision.Action)
		if err != nil {
			action = ActionAbstain
			decision.Reason = "selector returned unsupported action; abstaining"
			decision.Source = "vm"
		}
		trace := TraceEntry{
			Step:   step + 1,
			Action: action,
			Reason: decision.Reason,
			Source: decision.Source,
		}
		if err := vm.Execute(ctx, &state, action); err != nil {
			trace.Status = "error"
			state.ActionTrace = append(state.ActionTrace, trace)
			return state, err
		}
		trace.Status = state.FinalStatus
		if len(state.VerifierResults) > 0 {
			trace.VerifierStatus = state.VerifierResults[len(state.VerifierResults)-1].Status
		}
		state.ActionTrace = append(state.ActionTrace, trace)
	}
	if !state.Completed {
		if err := vm.Execute(ctx, &state, ActionAbstain); err != nil {
			return state, err
		}
		state.ActionTrace = append(state.ActionTrace, TraceEntry{
			Step:   len(state.ActionTrace) + 1,
			Action: ActionAbstain,
			Reason: "max steps exhausted",
			Source: "vm",
			Status: state.FinalStatus,
		})
	}
	return state, nil
}

func (vm VM) Execute(ctx context.Context, state *State, action ActionToken) error {
	switch action {
	case ActionRunTests:
		return vm.runTests(ctx, state)
	case ActionParseCode:
		return vm.parseCode(state)
	case ActionMutateCode:
		return vm.mutateCode(state)
	case ActionVerify:
		return vm.verify(ctx, state)
	case ActionRespond:
		state.Completed = true
		if state.Verified || state.lastVerifierStatus() == "pass" {
			state.FinalStatus = "verified"
		} else {
			state.FinalStatus = "unverified"
		}
		return nil
	case ActionAbstain:
		state.Completed = true
		state.FinalStatus = "abstained"
		state.Uncertainty = 1
		return nil
	default:
		return fmt.Errorf("unsupported action %q", action)
	}
}

func (s State) Snapshot() selector.Snapshot {
	return selector.Snapshot{
		HasRunTests:        s.HasRunTests,
		LastVerifierStatus: s.lastVerifierStatus(),
		Parsed:             s.Parsed,
		PatternFound:       s.PatternFound,
		HasCandidatePatch:  s.CandidatePatch != nil,
		Verified:           s.Verified,
		Completed:          s.Completed,
		ToolCalls:          s.ToolCalls,
		MaxToolCalls:       s.Budget.ToolCalls,
	}
}

func ParseAction(action string) (ActionToken, error) {
	switch action {
	case selector.ActRunTests:
		return ActionRunTests, nil
	case selector.ActParseCode:
		return ActionParseCode, nil
	case selector.ActMutateCode:
		return ActionMutateCode, nil
	case selector.ActVerify:
		return ActionVerify, nil
	case selector.ActRespond:
		return ActionRespond, nil
	case selector.ActAbstain:
		return ActionAbstain, nil
	default:
		return "", fmt.Errorf("unsupported functional action %q", action)
	}
}

func (vm VM) runTests(ctx context.Context, state *State) error {
	ev, err := verifier.RunSuccess(ctx, state.RepoPath, state.Success, vm.VerifierTimeout)
	if err != nil {
		return err
	}
	state.ToolCalls++
	state.HasRunTests = true
	state.Evidence = append(state.Evidence, ev)
	state.VerifierResults = append(state.VerifierResults, ev)
	state.FinalStatus = ev.Status
	return vm.recordEvidence(ctx, ev, string(ActionRunTests))
}

func (vm VM) parseCode(state *State) error {
	path := filepath.Join(state.RepoPath, "calculator.go")
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(raw)
	state.Parsed = true
	if strings.Contains(text, "return a - b") {
		state.PatternFound = true
		state.WorkingMemory = append(state.WorkingMemory, "calculator.go contains known toy bug pattern: return a - b")
		state.FinalStatus = "parsed"
		return nil
	}
	state.PatternFound = false
	state.WorkingMemory = append(state.WorkingMemory, "calculator.go did not contain known toy bug pattern")
	state.FinalStatus = "parsed_no_pattern"
	return nil
}

func (vm VM) mutateCode(state *State) error {
	if !state.PatternFound {
		return fmt.Errorf("cannot mutate without known pattern")
	}
	path := filepath.Join(state.RepoPath, "calculator.go")
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	oldText := string(raw)
	newText := strings.Replace(oldText, "return a - b", "return a + b", 1)
	if oldText == newText {
		return fmt.Errorf("toy patch pattern not found in %s", path)
	}
	diff := unifiedDiff("calculator.go", oldText, newText)
	state.CandidatePatch = &PatchCandidate{
		Path:    path,
		OldText: oldText,
		NewText: newText,
		Diff:    diff,
	}
	state.Diff = diff
	state.FinalStatus = "candidate_patch"
	return nil
}

func (vm VM) verify(ctx context.Context, state *State) error {
	if state.CandidatePatch == nil {
		return fmt.Errorf("cannot verify without candidate patch")
	}
	patch := state.CandidatePatch
	current, err := os.ReadFile(patch.Path)
	if err != nil {
		return err
	}
	original := string(current)
	if original != patch.OldText {
		return fmt.Errorf("candidate patch base changed before verify")
	}
	if err := os.WriteFile(patch.Path, []byte(patch.NewText), 0o644); err != nil {
		return err
	}
	ev, verifyErr := verifier.RunSuccess(ctx, state.RepoPath, state.Success, vm.VerifierTimeout)
	state.ToolCalls++
	state.Evidence = append(state.Evidence, ev)
	state.VerifierResults = append(state.VerifierResults, ev)
	if recordErr := vm.recordEvidence(ctx, ev, string(ActionVerify)); recordErr != nil && verifyErr == nil {
		verifyErr = recordErr
	}
	if ev.Status == "pass" {
		state.Verified = true
		state.FinalStatus = "verified"
		return verifyErr
	}
	if err := os.WriteFile(patch.Path, []byte(original), 0o644); err != nil {
		return err
	}
	state.FinalStatus = "verify_failed_rollback"
	return verifyErr
}

func (vm VM) recordEvidence(ctx context.Context, ev verifier.Evidence, action string) error {
	if vm.Store == nil || vm.EpisodeID == 0 {
		return nil
	}
	_, err := vm.Store.RecordEvidence(ctx, memory.Evidence{
		EpisodeID: vm.EpisodeID,
		Verifier:  ev.Verifier,
		Status:    ev.Status,
		Score:     ev.Score,
		Stdout:    ev.Stdout,
		Stderr:    ev.Stderr,
		Payload:   fmt.Sprintf(`{"action":%q}`, action),
		Timestamp: ev.Timestamp,
	})
	return err
}

func (s State) lastVerifierStatus() string {
	if len(s.VerifierResults) == 0 {
		return ""
	}
	return s.VerifierResults[len(s.VerifierResults)-1].Status
}

type MockPlanner struct{}

func (MockPlanner) Candidates(context.Context, State) ([]selector.Candidate, error) {
	return []selector.Candidate{
		{Action: selector.ActRunTests, LogProb: -0.1, Source: "mock"},
		{Action: selector.ActParseCode, LogProb: -0.2, Source: "mock"},
		{Action: selector.ActMutateCode, LogProb: -0.3, Source: "mock"},
		{Action: selector.ActVerify, LogProb: -0.4, Source: "mock"},
		{Action: selector.ActRespond, LogProb: -0.5, Source: "mock"},
		{Action: selector.ActAbstain, LogProb: -2.0, Source: "mock"},
	}, nil
}

type ModelPlanner struct {
	Runner runner.Runner
	TopK   int
}

func (p ModelPlanner) Candidates(_ context.Context, state State) ([]selector.Candidate, error) {
	topK := p.TopK
	if topK <= 0 {
		topK = 8
	}
	prompt := modelPrompt(state)
	tokens := p.Runner.Tokenizer.Encode(prompt)
	logits, err := p.Runner.Model.PredictNext(tokens)
	if err != nil {
		return nil, err
	}
	top, err := p.Runner.TopK(logits, topK)
	if err != nil {
		return nil, err
	}
	out := make([]selector.Candidate, 0, len(top))
	for _, candidate := range top {
		out = append(out, selector.Candidate{
			TokenID: candidate.TokenID,
			Action:  candidate.Token,
			LogProb: candidate.LogProb,
			Source:  "model",
		})
	}
	return out, nil
}

func modelPrompt(state State) string {
	goal := strings.ToLower(state.Goal)
	normalized := state.Goal
	if strings.Contains(goal, "test") || strings.Contains(goal, "go") {
		normalized = "fix failing go test"
	}
	var b strings.Builder
	b.WriteString("<USER>")
	b.WriteString(normalized)
	b.WriteString("<ASSISTANT>")
	for _, trace := range state.ActionTrace {
		b.WriteString(string(trace.Action))
	}
	return b.String()
}
