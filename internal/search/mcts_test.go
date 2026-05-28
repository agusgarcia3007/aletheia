package search

import (
	"context"
	"errors"
	"testing"

	"aletheia/internal/selector"
)

func TestMCTSFindsVerifiedBranchBehindNoisyPrior(t *testing.T) {
	result, err := MCTS(context.Background(), MCTSOptions[int]{
		Initial:    0,
		Iterations: 8,
		MaxDepth:   3,
		Candidates: func(context.Context, MCTSNode[int]) ([]selector.Candidate, error) {
			return []selector.Candidate{
				{Action: "bad", LogProb: 0, Source: "noisy"},
				{Action: "good", LogProb: -0.1, Source: "noisy"},
			}, nil
		},
		Step: func(_ context.Context, parent MCTSNode[int], candidate selector.Candidate) (StepResult[int], error) {
			if candidate.Action == "bad" {
				return StepResult[int]{State: parent.State, Reward: -10, Terminal: true, Status: "bad"}, nil
			}
			next := parent.State + 1
			return StepResult[int]{
				State:    next,
				Reward:   10 + float64(next),
				Terminal: next == 3,
				Verified: next == 3,
				Status:   "good",
			}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Found || result.Verified.Action != "good" || result.Verified.State != 3 {
		t.Fatalf("result = %+v, want verified good branch", result)
	}
	if result.Verified.Visits == 0 || result.Verified.Value == 0 {
		t.Fatalf("mcts stats not populated: %+v", result.Verified)
	}
}

func TestMCTSBranchErrorDoesNotAbortSiblings(t *testing.T) {
	result, err := MCTS(context.Background(), MCTSOptions[int]{
		Initial:    0,
		Iterations: 2,
		MaxDepth:   1,
		Candidates: func(context.Context, MCTSNode[int]) ([]selector.Candidate, error) {
			return []selector.Candidate{
				{Action: "bad", LogProb: 1},
				{Action: "good", LogProb: 0},
			}, nil
		},
		Step: func(_ context.Context, _ MCTSNode[int], candidate selector.Candidate) (StepResult[int], error) {
			if candidate.Action == "bad" {
				return StepResult[int]{}, errors.New("branch failed")
			}
			return StepResult[int]{State: 1, Reward: 50, Verified: true, Terminal: true}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Found || result.Verified.Action != "good" {
		t.Fatalf("result = %+v, want verified good branch", result)
	}
}

func TestMCTSExhaustsWithoutVerifiedSolution(t *testing.T) {
	result, err := MCTS(context.Background(), MCTSOptions[int]{
		Initial:    0,
		Iterations: 3,
		MaxDepth:   2,
		Candidates: func(context.Context, MCTSNode[int]) ([]selector.Candidate, error) {
			return []selector.Candidate{{Action: "step", LogProb: 0}}, nil
		},
		Step: func(_ context.Context, parent MCTSNode[int], _ selector.Candidate) (StepResult[int], error) {
			return StepResult[int]{State: parent.State + 1, Reward: 1, Status: "step"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Found || !result.Exhausted {
		t.Fatalf("result = %+v, want exhausted without verified", result)
	}
}
