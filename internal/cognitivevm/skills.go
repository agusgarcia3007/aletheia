package cognitivevm

import (
	"context"
	"fmt"

	"aletheia/internal/memory"
	"aletheia/internal/verifier"
)

func (vm VM) RunSkill(ctx context.Context, task Task, repoPath string, skill memory.Skill, maxSteps int) (State, bool, error) {
	if maxSteps <= 0 {
		maxSteps = 8
	}
	state := newState(task, repoPath, maxSteps)
	for _, actionText := range skill.ActionSequence {
		action, err := ParseAction(actionText)
		if err != nil {
			state.ActionTrace = append(state.ActionTrace, TraceEntry{
				Step:   len(state.ActionTrace) + 1,
				Action: ActionAbstain,
				Reason: fmt.Sprintf("skill %s contains unsupported action %q", skill.Name, actionText),
				Source: "skill",
				Status: "error",
			})
			return state, false, nil
		}
		trace := TraceEntry{
			Step:   len(state.ActionTrace) + 1,
			Action: action,
			Reason: fmt.Sprintf("reused skill %s", skill.Name),
			Source: "skill",
		}
		verifierCount := len(state.VerifierResults)
		if err := vm.Execute(ctx, &state, action); err != nil {
			trace.Status = "error"
			state.ActionTrace = append(state.ActionTrace, trace)
			return state, false, nil
		}
		trace.Status = state.FinalStatus
		if len(state.VerifierResults) > verifierCount {
			last := state.VerifierResults[len(state.VerifierResults)-1]
			trace.VerifierStatus = last.Status
			trace.Verifiers = append([]string(nil), last.Artifacts...)
		}
		state.ActionTrace = append(state.ActionTrace, trace)
		if action == ActionVerify && !(state.Verified || state.lastVerifierStatus() == verifier.StatusPass) {
			return state, false, nil
		}
	}
	return state, state.Verified || state.lastVerifierStatus() == verifier.StatusPass, nil
}
