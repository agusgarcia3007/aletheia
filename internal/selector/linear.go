package selector

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

const (
	LinearCheckpointFile = "selector.json"
	DefaultMinConfidence = 0.51
)

type TrainingExample struct {
	Snapshot   Snapshot    `json:"snapshot"`
	Candidates []Candidate `json:"candidates"`
	Chosen     string      `json:"chosen"`
	Reward     float64     `json:"reward"`
}

type TrainOptions struct {
	Epochs        int
	LearningRate  float64
	MinConfidence float64
}

type TrainReport struct {
	InitialLoss     float64
	FinalLoss       float64
	InitialAccuracy float64
	FinalAccuracy   float64
	Epochs          int
}

type LinearSelector struct {
	FeatureNames  []string  `json:"feature_names"`
	Weights       []float64 `json:"weights"`
	MinConfidence float64   `json:"min_confidence"`
}

type linearCheckpoint struct {
	Version       int       `json:"version"`
	FeatureNames  []string  `json:"feature_names"`
	Weights       []float64 `json:"weights"`
	MinConfidence float64   `json:"min_confidence"`
}

func LoadTrainingExamples(path string) ([]TrainingExample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var examples []TrainingExample
	scanner := bufio.NewScanner(f)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ex TrainingExample
		if err := json.Unmarshal(line, &ex); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
		if err := validateExample(ex); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo, err)
		}
		examples = append(examples, ex)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(examples) == 0 {
		return nil, fmt.Errorf("dataset %s has no examples", path)
	}
	return examples, nil
}

func TrainLinear(examples []TrainingExample, opts TrainOptions) (LinearSelector, TrainReport, error) {
	if len(examples) == 0 {
		return LinearSelector{}, TrainReport{}, fmt.Errorf("training examples are required")
	}
	for i, ex := range examples {
		if err := validateExample(ex); err != nil {
			return LinearSelector{}, TrainReport{}, fmt.Errorf("example %d: %w", i+1, err)
		}
	}
	epochs := opts.Epochs
	if epochs <= 0 {
		epochs = 300
	}
	lr := opts.LearningRate
	if lr <= 0 {
		lr = 0.1
	}
	minConfidence := opts.MinConfidence
	if minConfidence <= 0 {
		minConfidence = DefaultMinConfidence
	}
	model := LinearSelector{
		FeatureNames:  featureNames(),
		Weights:       make([]float64, len(featureNames())),
		MinConfidence: minConfidence,
	}
	initial := model.Evaluate(examples)
	for epoch := 0; epoch < epochs; epoch++ {
		for _, ex := range examples {
			choices := functionalCandidates(ex.Candidates)
			chosen := chosenIndex(choices, ex.Chosen)
			probs := model.probabilities(ex.Snapshot, choices)
			weight := ex.Reward
			if weight <= 0 {
				weight = 1
			}
			for i, candidate := range choices {
				target := 0.0
				if i == chosen {
					target = 1
				}
				scale := weight * (probs[i] - target)
				features := candidateFeatures(ex.Snapshot, candidate)
				for j, value := range features {
					model.Weights[j] -= lr * scale * value
				}
			}
		}
	}
	final := model.Evaluate(examples)
	return model, TrainReport{
		InitialLoss:     initial.Loss,
		FinalLoss:       final.Loss,
		InitialAccuracy: initial.Accuracy,
		FinalAccuracy:   final.Accuracy,
		Epochs:          epochs,
	}, nil
}

type Evaluation struct {
	Loss     float64
	Accuracy float64
}

func (m LinearSelector) Evaluate(examples []TrainingExample) Evaluation {
	if len(examples) == 0 {
		return Evaluation{}
	}
	totalLoss := 0.0
	correct := 0
	count := 0
	for _, ex := range examples {
		choices := functionalCandidates(ex.Candidates)
		chosen := chosenIndex(choices, ex.Chosen)
		if chosen < 0 {
			continue
		}
		probs := m.probabilities(ex.Snapshot, choices)
		if len(probs) == 0 {
			continue
		}
		p := probs[chosen]
		if p < 1e-12 {
			p = 1e-12
		}
		totalLoss += -math.Log(p)
		if bestIndex(probs) == chosen {
			correct++
		}
		count++
	}
	if count == 0 {
		return Evaluation{}
	}
	return Evaluation{
		Loss:     totalLoss / float64(count),
		Accuracy: float64(correct) / float64(count),
	}
}

func (m LinearSelector) Select(snapshot Snapshot, candidates []Candidate) Decision {
	choices := functionalCandidates(candidates)
	if len(choices) == 0 {
		return (HeuristicSelector{}).Select(snapshot, candidates)
	}
	probs := m.probabilities(snapshot, choices)
	best := bestIndex(probs)
	if best < 0 || probs[best] < m.confidenceThreshold() {
		decision := (HeuristicSelector{}).Select(snapshot, candidates)
		decision.Reason = fmt.Sprintf("linear selector confidence below threshold; %s", decision.Reason)
		return decision
	}
	return Decision{
		Action:     choices[best].Action,
		Confidence: probs[best],
		Reason:     "linear selector chose highest learned score",
		Source:     "linear_selector",
	}
}

func (m LinearSelector) Save(dir string) error {
	if dir == "" {
		return fmt.Errorf("selector checkpoint directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	checkpoint := linearCheckpoint{
		Version:       1,
		FeatureNames:  m.FeatureNames,
		Weights:       m.Weights,
		MinConfidence: m.confidenceThreshold(),
	}
	raw, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, LinearCheckpointFile), append(raw, '\n'), 0o644)
}

func LoadLinear(dir string) (LinearSelector, error) {
	raw, err := os.ReadFile(filepath.Join(dir, LinearCheckpointFile))
	if err != nil {
		return LinearSelector{}, err
	}
	var checkpoint linearCheckpoint
	if err := json.Unmarshal(raw, &checkpoint); err != nil {
		return LinearSelector{}, err
	}
	if checkpoint.Version != 1 {
		return LinearSelector{}, fmt.Errorf("unsupported selector checkpoint version %d", checkpoint.Version)
	}
	expected := featureNames()
	if len(checkpoint.FeatureNames) != len(expected) {
		return LinearSelector{}, fmt.Errorf("selector checkpoint feature count = %d, want %d", len(checkpoint.FeatureNames), len(expected))
	}
	for i := range expected {
		if checkpoint.FeatureNames[i] != expected[i] {
			return LinearSelector{}, fmt.Errorf("selector checkpoint feature %d = %q, want %q", i, checkpoint.FeatureNames[i], expected[i])
		}
	}
	if len(checkpoint.Weights) != len(expected) {
		return LinearSelector{}, fmt.Errorf("selector checkpoint weight count = %d, want %d", len(checkpoint.Weights), len(expected))
	}
	return LinearSelector{
		FeatureNames:  checkpoint.FeatureNames,
		Weights:       checkpoint.Weights,
		MinConfidence: checkpoint.MinConfidence,
	}, nil
}

func (m LinearSelector) probabilities(snapshot Snapshot, candidates []Candidate) []float64 {
	if len(candidates) == 0 {
		return nil
	}
	scores := make([]float64, len(candidates))
	maxScore := -math.MaxFloat64
	for i, candidate := range candidates {
		scores[i] = dot(m.Weights, candidateFeatures(snapshot, candidate))
		if scores[i] > maxScore {
			maxScore = scores[i]
		}
	}
	sum := 0.0
	for i := range scores {
		scores[i] = math.Exp(scores[i] - maxScore)
		sum += scores[i]
	}
	if sum == 0 {
		return scores
	}
	for i := range scores {
		scores[i] /= sum
	}
	return scores
}

func (m LinearSelector) confidenceThreshold() float64 {
	if m.MinConfidence <= 0 {
		return DefaultMinConfidence
	}
	return m.MinConfidence
}

func validateExample(ex TrainingExample) error {
	if ex.Chosen == "" {
		return fmt.Errorf("chosen action is required")
	}
	if !IsFunctional(ex.Chosen) {
		return fmt.Errorf("chosen action %q is not functional", ex.Chosen)
	}
	choices := functionalCandidates(ex.Candidates)
	if len(choices) == 0 {
		return fmt.Errorf("at least one functional candidate is required")
	}
	if chosenIndex(choices, ex.Chosen) < 0 {
		return fmt.Errorf("chosen action %q is not present in candidates", ex.Chosen)
	}
	return nil
}

func functionalCandidates(candidates []Candidate) []Candidate {
	out := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if IsFunctional(candidate.Action) {
			out = append(out, candidate)
		}
	}
	return out
}

func chosenIndex(candidates []Candidate, chosen string) int {
	for i, candidate := range candidates {
		if candidate.Action == chosen {
			return i
		}
	}
	return -1
}

func bestIndex(values []float64) int {
	best := -1
	bestValue := -math.MaxFloat64
	for i, value := range values {
		if best < 0 || value > bestValue {
			best = i
			bestValue = value
		}
	}
	return best
}

func dot(weights []float64, features []float64) float64 {
	total := 0.0
	for i := range weights {
		total += weights[i] * features[i]
	}
	return total
}

func featureNames() []string {
	actions := actionList()
	names := []string{
		"bias",
		"log_prob",
		"has_run_tests",
		"last_verifier_pass",
		"last_verifier_fail",
		"parsed",
		"pattern_found",
		"has_candidate_patch",
		"verified",
		"completed",
		"tool_budget_used",
	}
	for _, action := range actions {
		names = append(names, "action:"+action)
	}
	for _, action := range actions {
		names = append(names, "action:"+action+"|has_run_tests")
		names = append(names, "action:"+action+"|last_verifier_fail")
		names = append(names, "action:"+action+"|last_verifier_pass")
		names = append(names, "action:"+action+"|parsed")
		names = append(names, "action:"+action+"|pattern_found")
		names = append(names, "action:"+action+"|has_candidate_patch")
		names = append(names, "action:"+action+"|verified")
	}
	return names
}

func candidateFeatures(snapshot Snapshot, candidate Candidate) []float64 {
	features := make([]float64, len(featureNames()))
	i := 0
	features[i] = 1
	i++
	features[i] = candidate.LogProb
	i++
	features[i] = boolFloat(snapshot.HasRunTests)
	i++
	features[i] = boolFloat(snapshot.LastVerifierStatus == "pass")
	i++
	features[i] = boolFloat(snapshot.LastVerifierStatus == "fail")
	i++
	features[i] = boolFloat(snapshot.Parsed)
	i++
	features[i] = boolFloat(snapshot.PatternFound)
	i++
	features[i] = boolFloat(snapshot.HasCandidatePatch)
	i++
	features[i] = boolFloat(snapshot.Verified)
	i++
	features[i] = boolFloat(snapshot.Completed)
	i++
	if snapshot.MaxToolCalls > 0 {
		features[i] = float64(snapshot.ToolCalls) / float64(snapshot.MaxToolCalls)
	}
	i++
	for _, action := range actionList() {
		features[i] = boolFloat(candidate.Action == action)
		i++
	}
	for _, action := range actionList() {
		match := candidate.Action == action
		features[i] = boolFloat(match && snapshot.HasRunTests)
		i++
		features[i] = boolFloat(match && snapshot.LastVerifierStatus == "fail")
		i++
		features[i] = boolFloat(match && snapshot.LastVerifierStatus == "pass")
		i++
		features[i] = boolFloat(match && snapshot.Parsed)
		i++
		features[i] = boolFloat(match && snapshot.PatternFound)
		i++
		features[i] = boolFloat(match && snapshot.HasCandidatePatch)
		i++
		features[i] = boolFloat(match && snapshot.Verified)
		i++
	}
	return features
}

func actionList() []string {
	return []string{ActRunTests, ActParseCode, ActMutateCode, ActVerify, ActRespond, ActAbstain}
}

func boolFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}
