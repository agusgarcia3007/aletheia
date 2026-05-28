package search

import (
	"context"
	"fmt"
	"sort"

	"aletheia/internal/selector"
)

type Options[S any] struct {
	Initial      S
	BeamWidth    int
	MaxDepth     int
	ErrorReward  float64
	IsFunctional func(string) bool
	Candidates   func(context.Context, Node[S]) ([]selector.Candidate, error)
	Step         func(context.Context, Node[S], selector.Candidate) (StepResult[S], error)
}

type StepResult[S any] struct {
	State    S
	Reward   float64
	Terminal bool
	Verified bool
	Status   string
	Error    string
}

type Node[S any] struct {
	ID       int
	ParentID int
	Depth    int
	State    S
	Action   string
	Source   string
	LogProb  float64
	Reward   float64
	Score    float64
	Terminal bool
	Verified bool
	Status   string
	Error    string
}

type Result[S any] struct {
	Found     bool
	Exhausted bool
	Best      Node[S]
	Verified  Node[S]
	Nodes     []Node[S]
}

func Beam[S any](ctx context.Context, opts Options[S]) (Result[S], error) {
	if opts.Candidates == nil {
		return Result[S]{}, fmt.Errorf("search candidates callback is required")
	}
	if opts.Step == nil {
		return Result[S]{}, fmt.Errorf("search step callback is required")
	}
	beamWidth := opts.BeamWidth
	if beamWidth <= 0 {
		beamWidth = 1
	}
	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 1
	}
	errorReward := opts.ErrorReward
	if errorReward == 0 {
		errorReward = -30
	}
	isFunctional := opts.IsFunctional
	if isFunctional == nil {
		isFunctional = func(string) bool { return true }
	}

	nextID := 1
	root := Node[S]{ID: nextID, State: opts.Initial}
	result := Result[S]{
		Best:  root,
		Nodes: []Node[S]{root},
	}
	frontier := []Node[S]{root}

	for depth := 0; depth < maxDepth && len(frontier) > 0; depth++ {
		var expanded []Node[S]
		for _, parent := range frontier {
			if parent.Terminal {
				continue
			}
			candidates, err := opts.Candidates(ctx, parent)
			if err != nil {
				return result, err
			}
			candidates = topFunctional(candidates, beamWidth, isFunctional)
			for _, candidate := range candidates {
				nextID++
				child := Node[S]{
					ID:       nextID,
					ParentID: parent.ID,
					Depth:    parent.Depth + 1,
					Action:   candidate.Action,
					Source:   candidate.Source,
					LogProb:  candidate.LogProb,
				}
				step, err := opts.Step(ctx, parent, candidate)
				if err != nil {
					child.State = parent.State
					child.Reward = errorReward
					child.Score = parent.Score + candidate.LogProb + errorReward
					child.Terminal = true
					child.Status = "error"
					child.Error = err.Error()
				} else {
					child.State = step.State
					child.Reward = step.Reward
					child.Score = parent.Score + candidate.LogProb + step.Reward
					child.Terminal = step.Terminal
					child.Verified = step.Verified
					child.Status = step.Status
					child.Error = step.Error
				}
				result.Nodes = append(result.Nodes, child)
				if better(child, result.Best) {
					result.Best = child
				}
				if child.Verified && (!result.Found || better(child, result.Verified)) {
					result.Found = true
					result.Verified = child
				}
				if !child.Terminal {
					expanded = append(expanded, child)
				}
			}
		}
		if result.Found {
			return result, nil
		}
		sortNodes(expanded)
		if len(expanded) > beamWidth {
			expanded = expanded[:beamWidth]
		}
		frontier = expanded
	}
	result.Exhausted = true
	return result, nil
}

func (r Result[S]) Path(node Node[S]) []Node[S] {
	byID := make(map[int]Node[S], len(r.Nodes))
	for _, n := range r.Nodes {
		byID[n.ID] = n
	}
	var reversed []Node[S]
	for node.ID != 0 {
		reversed = append(reversed, node)
		if node.ParentID == 0 {
			break
		}
		var ok bool
		node, ok = byID[node.ParentID]
		if !ok {
			break
		}
	}
	out := make([]Node[S], len(reversed))
	for i := range reversed {
		out[i] = reversed[len(reversed)-1-i]
	}
	return out
}

func topFunctional(candidates []selector.Candidate, limit int, isFunctional func(string) bool) []selector.Candidate {
	filtered := make([]selector.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if isFunctional(candidate.Action) {
			filtered = append(filtered, candidate)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].LogProb > filtered[j].LogProb
	})
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

func sortNodes[S any](nodes []Node[S]) {
	sort.SliceStable(nodes, func(i, j int) bool {
		return better(nodes[i], nodes[j])
	})
}

func better[S any](left Node[S], right Node[S]) bool {
	if left.Score == right.Score {
		return left.ID < right.ID
	}
	return left.Score > right.Score
}
