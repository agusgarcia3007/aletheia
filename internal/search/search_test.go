package search

import (
	"context"
	"errors"
	"testing"

	"aletheia/internal/selector"
)

func TestBeamRespectsWidthAndKeepsBestStates(t *testing.T) {
	gotExpanded := 0
	result, err := Beam(context.Background(), Options[int]{
		Initial:   0,
		BeamWidth: 2,
		MaxDepth:  1,
		Candidates: func(context.Context, Node[int]) ([]selector.Candidate, error) {
			return []selector.Candidate{
				{Action: "a", LogProb: 1, Source: "test"},
				{Action: "b", LogProb: 5, Source: "test"},
				{Action: "c", LogProb: 3, Source: "test"},
			}, nil
		},
		Step: func(_ context.Context, _ Node[int], candidate selector.Candidate) (StepResult[int], error) {
			gotExpanded++
			return StepResult[int]{State: int(candidate.Action[0]), Reward: candidate.LogProb}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotExpanded != 2 {
		t.Fatalf("expanded = %d, want 2", gotExpanded)
	}
	if result.Best.Action != "b" {
		t.Fatalf("best action = %q, want b", result.Best.Action)
	}
}

func TestBeamIgnoresNonFunctionalCandidates(t *testing.T) {
	var actions []string
	_, err := Beam(context.Background(), Options[int]{
		Initial:   0,
		BeamWidth: 3,
		MaxDepth:  1,
		IsFunctional: func(action string) bool {
			return action != "bad"
		},
		Candidates: func(context.Context, Node[int]) ([]selector.Candidate, error) {
			return []selector.Candidate{
				{Action: "bad", LogProb: 100},
				{Action: "good", LogProb: 1},
			}, nil
		},
		Step: func(_ context.Context, _ Node[int], candidate selector.Candidate) (StepResult[int], error) {
			actions = append(actions, candidate.Action)
			return StepResult[int]{State: 1, Reward: 1}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0] != "good" {
		t.Fatalf("actions = %+v, want only good", actions)
	}
}

func TestBeamBranchErrorDoesNotAbortSiblings(t *testing.T) {
	result, err := Beam(context.Background(), Options[int]{
		Initial:   0,
		BeamWidth: 2,
		MaxDepth:  1,
		Candidates: func(context.Context, Node[int]) ([]selector.Candidate, error) {
			return []selector.Candidate{
				{Action: "bad", LogProb: 10},
				{Action: "good", LogProb: 0},
			}, nil
		},
		Step: func(_ context.Context, _ Node[int], candidate selector.Candidate) (StepResult[int], error) {
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

func TestBeamReturnsExhaustedWithoutVerifiedSolution(t *testing.T) {
	result, err := Beam(context.Background(), Options[int]{
		Initial:   0,
		BeamWidth: 1,
		MaxDepth:  2,
		Candidates: func(context.Context, Node[int]) ([]selector.Candidate, error) {
			return []selector.Candidate{{Action: "step", LogProb: 1}}, nil
		},
		Step: func(_ context.Context, parent Node[int], _ selector.Candidate) (StepResult[int], error) {
			return StepResult[int]{State: parent.State + 1, Reward: 1}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Found || !result.Exhausted {
		t.Fatalf("result = %+v, want exhausted without verified", result)
	}
	if result.Best.State != 2 {
		t.Fatalf("best state = %d, want 2", result.Best.State)
	}
}
