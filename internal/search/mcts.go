package search

import (
	"context"
	"fmt"
	"math"

	"aletheia/internal/selector"
)

type MCTSOptions[S any] struct {
	Initial      S
	Iterations   int
	MaxDepth     int
	Exploration  float64
	ErrorReward  float64
	IsFunctional func(string) bool
	Candidates   func(context.Context, MCTSNode[S]) ([]selector.Candidate, error)
	Step         func(context.Context, MCTSNode[S], selector.Candidate) (StepResult[S], error)
}

type MCTSNode[S any] struct {
	ID       int     `json:"id"`
	ParentID int     `json:"parent_id,omitempty"`
	Depth    int     `json:"depth"`
	State    S       `json:"-"`
	Action   string  `json:"action,omitempty"`
	Source   string  `json:"source,omitempty"`
	LogProb  float64 `json:"logprob"`
	Prior    float64 `json:"prior"`
	Reward   float64 `json:"reward"`
	Score    float64 `json:"score"`
	Visits   int     `json:"visits"`
	Value    float64 `json:"value"`
	Terminal bool    `json:"terminal"`
	Verified bool    `json:"verified"`
	Status   string  `json:"status,omitempty"`
	Error    string  `json:"error,omitempty"`
}

type MCTSResult[S any] struct {
	Found     bool
	Exhausted bool
	Best      MCTSNode[S]
	Verified  MCTSNode[S]
	Nodes     []MCTSNode[S]
}

func MCTS[S any](ctx context.Context, opts MCTSOptions[S]) (MCTSResult[S], error) {
	if opts.Candidates == nil {
		return MCTSResult[S]{}, fmt.Errorf("mcts candidates callback is required")
	}
	if opts.Step == nil {
		return MCTSResult[S]{}, fmt.Errorf("mcts step callback is required")
	}
	iterations := opts.Iterations
	if iterations <= 0 {
		iterations = 32
	}
	maxDepth := opts.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 8
	}
	exploration := opts.Exploration
	if exploration == 0 {
		exploration = 1.4
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
	root := MCTSNode[S]{ID: nextID, State: opts.Initial, Visits: 1}
	result := MCTSResult[S]{
		Best:  root,
		Nodes: []MCTSNode[S]{root},
	}
	indexByID := map[int]int{root.ID: 0}
	childrenByID := map[int][]int{}
	expanded := map[int]bool{}

	for iteration := 0; iteration < iterations; iteration++ {
		leaf := selectMCTSLeaf(result.Nodes, indexByID, childrenByID, expanded, exploration)
		if leaf.ID == 0 || leaf.Terminal || leaf.Depth >= maxDepth {
			continue
		}
		candidates, err := opts.Candidates(ctx, leaf)
		if err != nil {
			return result, err
		}
		candidates = topFunctional(candidates, 0, isFunctional)
		expanded[leaf.ID] = true
		if len(candidates) == 0 {
			idx := indexByID[leaf.ID]
			result.Nodes[idx].Terminal = true
			result.Nodes[idx].Status = "no_functional_candidates"
			continue
		}
		for _, candidate := range candidates {
			nextID++
			child := MCTSNode[S]{
				ID:       nextID,
				ParentID: leaf.ID,
				Depth:    leaf.Depth + 1,
				Action:   candidate.Action,
				Source:   candidate.Source,
				LogProb:  candidate.LogProb,
				Prior:    math.Exp(candidate.LogProb),
			}
			step, err := opts.Step(ctx, leaf, candidate)
			if err != nil {
				child.State = leaf.State
				child.Reward = errorReward
				child.Score = leaf.Score + candidate.LogProb + errorReward
				child.Terminal = true
				child.Status = "error"
				child.Error = err.Error()
			} else {
				child.State = step.State
				child.Reward = step.Reward
				child.Score = leaf.Score + candidate.LogProb + step.Reward
				child.Terminal = step.Terminal
				child.Verified = step.Verified
				child.Status = step.Status
				child.Error = step.Error
			}
			result.Nodes = append(result.Nodes, child)
			indexByID[child.ID] = len(result.Nodes) - 1
			childrenByID[leaf.ID] = append(childrenByID[leaf.ID], child.ID)
			backpropMCTS(result.Nodes, indexByID, child.ID, child.Reward)
			child = result.Nodes[indexByID[child.ID]]
			if betterMCTS(child, result.Best) {
				result.Best = child
			}
			if child.Verified && (!result.Found || betterMCTS(child, result.Verified)) {
				result.Found = true
				result.Verified = child
			}
		}
		if result.Found {
			return result, nil
		}
	}
	result.Exhausted = true
	return result, nil
}

func (r MCTSResult[S]) Path(node MCTSNode[S]) []MCTSNode[S] {
	byID := make(map[int]MCTSNode[S], len(r.Nodes))
	for _, n := range r.Nodes {
		byID[n.ID] = n
	}
	var reversed []MCTSNode[S]
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
	out := make([]MCTSNode[S], len(reversed))
	for i := range reversed {
		out[i] = reversed[len(reversed)-1-i]
	}
	return out
}

func selectMCTSLeaf[S any](nodes []MCTSNode[S], indexByID map[int]int, childrenByID map[int][]int, expanded map[int]bool, exploration float64) MCTSNode[S] {
	if len(nodes) == 0 {
		return MCTSNode[S]{}
	}
	current := nodes[0]
	for {
		if current.Terminal || !expanded[current.ID] {
			return current
		}
		children := childrenByID[current.ID]
		if len(children) == 0 {
			return current
		}
		bestID := children[0]
		bestScore := math.Inf(-1)
		parentVisits := math.Max(1, float64(current.Visits))
		for _, childID := range children {
			child := nodes[indexByID[childID]]
			visits := math.Max(1, float64(child.Visits))
			exploitation := child.Value / visits
			explorationScore := exploration * math.Max(child.Prior, 0.001) * math.Sqrt(math.Log(parentVisits+1)/visits)
			score := exploitation + explorationScore
			if score > bestScore || (score == bestScore && child.ID < bestID) {
				bestID = child.ID
				bestScore = score
			}
		}
		current = nodes[indexByID[bestID]]
	}
}

func backpropMCTS[S any](nodes []MCTSNode[S], indexByID map[int]int, nodeID int, reward float64) {
	for nodeID != 0 {
		idx := indexByID[nodeID]
		nodes[idx].Visits++
		nodes[idx].Value += reward
		nodeID = nodes[idx].ParentID
	}
}

func betterMCTS[S any](left MCTSNode[S], right MCTSNode[S]) bool {
	if left.Score == right.Score {
		return left.ID < right.ID
	}
	return left.Score > right.Score
}
