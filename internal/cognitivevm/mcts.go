package cognitivevm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"aletheia/internal/memory"
	"aletheia/internal/search"
	"aletheia/internal/selector"
	"aletheia/internal/verifier"
)

func (s Solver) runMCTS(ctx context.Context, task Task, repoPath string, planner Planner, actionSelector ActionSelector, useSelector bool, vm VM) (State, error) {
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
	iterations := width * depth * 2

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
		Reason: "initial verifier evidence before mcts search",
		Source: "mcts",
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
			Source:         "mcts",
			Status:         initial.FinalStatus,
			VerifierStatus: initial.lastVerifierStatus(),
		})
		return initial, nil
	}

	candidatesByNode := map[int][]selector.Candidate{}
	result, err := search.MCTS(ctx, search.MCTSOptions[State]{
		Initial:      initial,
		Iterations:   iterations,
		MaxDepth:     depth,
		ErrorReward:  -30,
		IsFunctional: selector.IsFunctional,
		Candidates: func(ctx context.Context, node search.MCTSNode[State]) ([]selector.Candidate, error) {
			candidates, err := planner.Candidates(ctx, node.State)
			if err != nil {
				return nil, err
			}
			candidatesByNode[node.ID] = append([]selector.Candidate(nil), candidates...)
			if useSelector && actionSelector != nil {
				return selectorAdjustedCandidates(node.State.Snapshot(), candidates, actionSelector), nil
			}
			return candidates, nil
		},
		Step: func(ctx context.Context, node search.MCTSNode[State], candidate selector.Candidate) (search.StepResult[State], error) {
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
				Reason: fmt.Sprintf("mcts expanded candidate logprob %.4f", candidate.LogProb),
				Source: candidate.Source,
			}
			verifierCount := len(child.VerifierResults)
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
			if len(child.VerifierResults) > verifierCount {
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
	if err := persistMCTSTrajectory(ctx, vm.Store, vm.EpisodeID, result); err != nil {
		return State{}, err
	}
	if err := persistMCTSSelectorExamples(ctx, vm.Store, vm.EpisodeID, result, candidatesByNode); err != nil {
		return State{}, err
	}
	if !result.Found {
		return abstainedMCTSState(ctx, repoPath, vm, result.Best)
	}
	return materializeMCTSWinner(ctx, repoPath, vm, result.Verified.State)
}

func persistMCTSTrajectory(ctx context.Context, store *memory.Store, episodeID int64, result search.MCTSResult[State]) error {
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
			Visits:             node.Visits,
			Prior:              node.Prior,
			Value:              node.Value,
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

func persistMCTSSelectorExamples(ctx context.Context, store *memory.Store, episodeID int64, result search.MCTSResult[State], candidatesByNode map[int][]selector.Candidate) error {
	if store == nil || episodeID == 0 {
		return nil
	}
	for _, node := range result.Nodes {
		if node.ParentID == 0 || node.Action == "" {
			continue
		}
		parent, ok := mctsNodeByID(result.Nodes, node.ParentID)
		if !ok {
			continue
		}
		candidates := candidatesByNode[parent.ID]
		if len(candidates) == 0 {
			continue
		}
		payload, err := json.Marshal(selector.TrainingExample{
			Snapshot:   parent.State.Snapshot(),
			Candidates: candidates,
			Chosen:     node.Action,
			Reward:     node.Reward,
		})
		if err != nil {
			return err
		}
		if _, err := store.RecordSelectorExample(ctx, episodeID, fmt.Sprintf("mcts:%d", node.ID), string(payload)); err != nil {
			return err
		}
	}
	return nil
}

func mctsNodeByID(nodes []search.MCTSNode[State], id int) (search.MCTSNode[State], bool) {
	for _, node := range nodes {
		if node.ID == id {
			return node, true
		}
	}
	return search.MCTSNode[State]{}, false
}

func materializeMCTSWinner(ctx context.Context, repoPath string, vm VM, winner State) (State, error) {
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
		Source: "mcts",
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

func abstainedMCTSState(ctx context.Context, repoPath string, vm VM, best search.MCTSNode[State]) (State, error) {
	state := cloneStateForRepo(best.State, repoPath)
	if err := vm.Execute(ctx, &state, ActionAbstain); err != nil {
		return state, err
	}
	state.ActionTrace = append(state.ActionTrace, TraceEntry{
		Step:   len(state.ActionTrace) + 1,
		Action: ActionAbstain,
		Reason: "no verified solution found with mcts budget",
		Source: "mcts",
		Status: state.FinalStatus,
	})
	return state, nil
}
