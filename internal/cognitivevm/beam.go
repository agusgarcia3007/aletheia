package cognitivevm

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"aletheia/internal/memory"
	"aletheia/internal/search"
	"aletheia/internal/selector"
	"aletheia/internal/verifier"
)

func (s Solver) runBeam(ctx context.Context, task Task, repoPath string, planner Planner, vm VM) (State, error) {
	depth := s.MaxDepth
	if depth <= 0 {
		depth = s.MaxSteps
	}
	if depth <= 0 {
		depth = 8
	}
	width := s.BeamWidth
	if width <= 0 {
		width = 4
	}

	tempRoots := []string{}
	defer func() {
		for _, root := range tempRoots {
			_ = os.RemoveAll(root)
		}
	}()

	initial := newState(task, repoPath, depth)
	initialTrace := TraceEntry{
		Step:   1,
		Action: ActionRunTests,
		Reason: "initial verifier evidence before beam search",
		Source: "beam",
	}
	if err := vm.Execute(ctx, &initial, ActionRunTests); err != nil {
		initialTrace.Status = "error"
		initial.ActionTrace = append(initial.ActionTrace, initialTrace)
		return initial, err
	}
	initialTrace.Status = initial.FinalStatus
	if len(initial.VerifierResults) > 0 {
		last := initial.VerifierResults[len(initial.VerifierResults)-1]
		initialTrace.VerifierStatus = last.Status
		initialTrace.Verifiers = append([]string(nil), last.Artifacts...)
	}
	initial.ActionTrace = append(initial.ActionTrace, initialTrace)
	if initial.lastVerifierStatus() == verifier.StatusPass {
		if err := vm.Execute(ctx, &initial, ActionRespond); err != nil {
			return initial, err
		}
		initial.ActionTrace = append(initial.ActionTrace, TraceEntry{
			Step:           len(initial.ActionTrace) + 1,
			Action:         ActionRespond,
			Reason:         "initial verifier already passes",
			Source:         "beam",
			Status:         initial.FinalStatus,
			VerifierStatus: initial.lastVerifierStatus(),
		})
		return initial, nil
	}

	result, err := search.Beam(ctx, search.Options[State]{
		Initial:      initial,
		BeamWidth:    width,
		MaxDepth:     depth,
		ErrorReward:  -30,
		IsFunctional: selector.IsFunctional,
		Candidates: func(ctx context.Context, node search.Node[State]) ([]selector.Candidate, error) {
			return planner.Candidates(ctx, node.State)
		},
		Step: func(ctx context.Context, node search.Node[State], candidate selector.Candidate) (search.StepResult[State], error) {
			tempRoot, workspace, err := copyRepo(node.State.RepoPath)
			if err != nil {
				return search.StepResult[State]{}, err
			}
			tempRoots = append(tempRoots, tempRoot)
			child := cloneStateForRepo(node.State, workspace)
			action, err := ParseAction(candidate.Action)
			if err != nil {
				action = ActionAbstain
			}

			trace := TraceEntry{
				Step:   len(child.ActionTrace) + 1,
				Action: action,
				Reason: fmt.Sprintf("beam expanded candidate logprob %.4f", candidate.LogProb),
				Source: candidate.Source,
			}
			executeErr := vm.Execute(ctx, &child, action)
			if executeErr != nil {
				trace.Status = "error"
				child.FinalStatus = "error"
				child.ActionTrace = append(child.ActionTrace, trace)
				return search.StepResult[State]{
					State:    child,
					Reward:   beamReward(child, node.Depth+1, executeErr.Error()),
					Terminal: true,
					Status:   "error",
					Error:    executeErr.Error(),
				}, nil
			}
			trace.Status = child.FinalStatus
			if len(child.VerifierResults) > 0 {
				last := child.VerifierResults[len(child.VerifierResults)-1]
				trace.VerifierStatus = last.Status
				trace.Verifiers = append([]string(nil), last.Artifacts...)
			}
			child.ActionTrace = append(child.ActionTrace, trace)

			verified := child.Verified || child.lastVerifierStatus() == verifier.StatusPass
			return search.StepResult[State]{
				State:    child,
				Reward:   beamReward(child, node.Depth+1, ""),
				Terminal: child.Completed || verified,
				Verified: verified,
				Status:   child.FinalStatus,
			}, nil
		},
	})
	if err != nil {
		return State{}, err
	}
	if err := persistBeamTrajectory(ctx, vm.Store, vm.EpisodeID, result); err != nil {
		return State{}, err
	}
	if !result.Found {
		return abstainedBeamState(ctx, repoPath, vm, result.Best)
	}
	return materializeBeamWinner(ctx, repoPath, vm, result.Verified.State)
}

func beamReward(state State, depth int, branchError string) float64 {
	reward := 0.0
	if state.Verified || state.lastVerifierStatus() == verifier.StatusPass {
		reward += 100
	}
	if state.CandidatePatch != nil {
		reward += 25
	}
	if state.PatternFound {
		reward += 15
	}
	if len(state.Evidence) > 0 || len(state.VerifierResults) > 0 {
		reward += 10
	}
	if branchError != "" {
		reward -= 30
	}
	if state.FinalStatus == "verify_failed_rollback" {
		reward -= 25
	}
	if state.FinalStatus == "abstained" || (state.Completed && !(state.Verified || state.lastVerifierStatus() == verifier.StatusPass)) {
		reward -= 20
	}
	reward -= 2 * float64(depth)
	return reward
}

func materializeBeamWinner(ctx context.Context, repoPath string, vm VM, winner State) (State, error) {
	state := cloneStateForRepo(winner, repoPath)
	if state.CandidatePatch == nil {
		return state, nil
	}
	state.Verified = false
	state.Completed = false
	state.FinalStatus = ""

	trace := TraceEntry{
		Step:   len(state.ActionTrace) + 1,
		Action: ActionVerify,
		Reason: "final verification on original repo",
		Source: "beam",
	}
	if err := vm.Execute(ctx, &state, ActionVerify); err != nil {
		trace.Status = "error"
		state.ActionTrace = append(state.ActionTrace, trace)
		return state, err
	}
	trace.Status = state.FinalStatus
	if len(state.VerifierResults) > 0 {
		last := state.VerifierResults[len(state.VerifierResults)-1]
		trace.VerifierStatus = last.Status
		trace.Verifiers = append([]string(nil), last.Artifacts...)
	}
	state.ActionTrace = append(state.ActionTrace, trace)
	return state, nil
}

func abstainedBeamState(ctx context.Context, repoPath string, vm VM, best search.Node[State]) (State, error) {
	state := cloneStateForRepo(best.State, repoPath)
	if err := vm.Execute(ctx, &state, ActionAbstain); err != nil {
		return state, err
	}
	state.ActionTrace = append(state.ActionTrace, TraceEntry{
		Step:   len(state.ActionTrace) + 1,
		Action: ActionAbstain,
		Reason: "no verified solution found with beam budget",
		Source: "beam",
		Status: state.FinalStatus,
	})
	return state, nil
}

func persistBeamTrajectory(ctx context.Context, store *memory.Store, episodeID int64, result search.Result[State]) error {
	if store == nil || episodeID == 0 {
		return nil
	}
	selectedIDs := map[int]bool{}
	selected := result.Best
	if result.Found {
		selected = result.Verified
	}
	for _, node := range result.Path(selected) {
		selectedIDs[node.ID] = true
	}

	records := make([]memory.TrajectoryRecord, 0, len(result.Nodes))
	for _, node := range result.Nodes {
		status := node.Status
		if status == "" {
			status = node.State.FinalStatus
		}
		records = append(records, memory.TrajectoryRecord{
			SearchNodeID:       node.ID,
			ParentSearchNodeID: node.ParentID,
			Action:             node.Action,
			Source:             node.Source,
			Depth:              node.Depth,
			Reward:             node.Reward,
			Score:              node.Score,
			Status:             status,
			VerifierStatus:     node.State.lastVerifierStatus(),
			Error:              node.Error,
			Verified:           node.Verified || node.State.Verified || node.State.lastVerifierStatus() == verifier.StatusPass,
			Completed:          node.Terminal || node.State.Completed,
			Selected:           selectedIDs[node.ID],
		})
	}
	return store.RecordTrajectory(ctx, episodeID, records)
}

func cloneStateForRepo(state State, repoPath string) State {
	oldRepoPath := state.RepoPath
	state.RepoPath = repoPath
	state.WorkingMemory = append([]string(nil), state.WorkingMemory...)
	state.Evidence = append([]verifier.Evidence(nil), state.Evidence...)
	state.VerifierResults = append([]verifier.Evidence(nil), state.VerifierResults...)
	state.ActionTrace = cloneTrace(state.ActionTrace)
	if state.CandidatePatch != nil {
		patch := *state.CandidatePatch
		patch.Path = rewriteRepoPath(oldRepoPath, repoPath, patch.Path)
		state.CandidatePatch = &patch
	}
	return state
}

func cloneTrace(trace []TraceEntry) []TraceEntry {
	out := make([]TraceEntry, len(trace))
	for i, entry := range trace {
		out[i] = entry
		out[i].Verifiers = append([]string(nil), entry.Verifiers...)
	}
	return out
}

func rewriteRepoPath(oldRepoPath string, newRepoPath string, path string) string {
	rel, err := filepath.Rel(oldRepoPath, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}
	return filepath.Join(newRepoPath, rel)
}

func copyRepo(src string) (string, string, error) {
	tempRoot, err := os.MkdirTemp("", "aletheia-beam-*")
	if err != nil {
		return "", "", err
	}
	dst := filepath.Join(tempRoot, "repo")
	if err := copyDir(src, dst); err != nil {
		_ = os.RemoveAll(tempRoot)
		return "", "", err
	}
	return tempRoot, dst, nil
}

func copyDir(src string, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			if err := os.Symlink(target, dstPath); err != nil {
				return err
			}
			continue
		}
		if info.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		if info.Mode().IsRegular() {
			if err := copyFile(srcPath, dstPath, info.Mode().Perm()); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src string, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
