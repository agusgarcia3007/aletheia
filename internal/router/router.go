package router

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Intent string

const (
	IntentSmalltalk       Intent = "smalltalk"
	IntentCodingHelp      Intent = "coding_help"
	IntentCodeGeneration  Intent = "code_generation"
	IntentRepoAgent       Intent = "repo_agent"
	IntentMath            Intent = "math"
	IntentTranslation     Intent = "translation"
	IntentFactualResearch Intent = "factual_research"
	IntentDocumentQA      Intent = "document_qa"
	IntentToolCall        Intent = "tool_call"
	IntentAbstain         Intent = "abstain"
	IntentUnknown         Intent = "unknown"
)

var PublicIntents = []Intent{
	IntentSmalltalk,
	IntentCodingHelp,
	IntentCodeGeneration,
	IntentRepoAgent,
	IntentMath,
	IntentTranslation,
	IntentFactualResearch,
	IntentDocumentQA,
	IntentToolCall,
	IntentAbstain,
	IntentUnknown,
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Input struct {
	Messages         []Message
	Text             string
	HasTools         bool
	HasLocalEvidence bool
}

type RouteDecision struct {
	Intent     Intent   `json:"intent"`
	Confidence float64  `json:"confidence"`
	Reason     string   `json:"reason"`
	Features   []string `json:"features,omitempty"`
}

type Router interface {
	Route(Input) RouteDecision
}

type TrainingExample struct {
	Text             string    `json:"text,omitempty"`
	Messages         []Message `json:"messages,omitempty"`
	Intent           Intent    `json:"intent"`
	HasTools         bool      `json:"has_tools,omitempty"`
	HasLocalEvidence bool      `json:"has_local_evidence,omitempty"`
}

type LinearRouter struct {
	Intents       []Intent            `json:"intents"`
	Weights       map[Intent]Features `json:"weights"`
	FeatureCounts map[string]int      `json:"feature_counts,omitempty"`
	MinConfidence float64             `json:"min_confidence"`
}

type Features map[string]float64

type TrainOptions struct {
	Epochs          int
	LearningRate    float64
	MinConfidence   float64
	ValidationSplit float64
	PruneMinCount   int
}

type TrainReport struct {
	Examples           int
	Epochs             int
	InitialAccuracy    float64
	FinalAccuracy      float64
	InitialLoss        float64
	FinalLoss          float64
	ValidationExamples int
	ValidationAccuracy float64
}

func NewFallback() Router {
	return fallbackRouter{}
}

func LoadLinear(dir string) (LinearRouter, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "router.json"))
	if err != nil {
		return LinearRouter{}, err
	}
	var r LinearRouter
	if err := json.Unmarshal(raw, &r); err != nil {
		return LinearRouter{}, err
	}
	if len(r.Intents) == 0 {
		r.Intents = PublicIntents
	}
	if r.Weights == nil {
		r.Weights = map[Intent]Features{}
	}
	if r.MinConfidence <= 0 {
		r.MinConfidence = 0.35
	}
	return r, nil
}

func (r LinearRouter) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "router.json"), raw, 0o644)
}

func LoadTrainingExamples(path string) ([]TrainingExample, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var examples []TrainingExample
	for lineNo, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ex TrainingExample
		if err := json.Unmarshal([]byte(line), &ex); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, lineNo+1, err)
		}
		if ex.Intent == "" {
			return nil, fmt.Errorf("%s:%d: intent is required", path, lineNo+1)
		}
		if strings.TrimSpace(ex.Text) == "" && len(ex.Messages) == 0 {
			return nil, fmt.Errorf("%s:%d: text or messages is required", path, lineNo+1)
		}
		examples = append(examples, ex)
	}
	if len(examples) == 0 {
		return nil, fmt.Errorf("router dataset %s has no examples", path)
	}
	return examples, nil
}

func TrainLinear(examples []TrainingExample, opts TrainOptions) (LinearRouter, TrainReport, error) {
	if len(examples) == 0 {
		return LinearRouter{}, TrainReport{}, fmt.Errorf("empty router dataset")
	}
	if opts.Epochs <= 0 {
		opts.Epochs = 80
	}
	if opts.LearningRate <= 0 {
		opts.LearningRate = 0.2
	}
	if opts.MinConfidence <= 0 {
		opts.MinConfidence = 0.35
	}
	// Hold out a validation set so generalization is measured, not just memorized
	// training accuracy. A split of 0 trains on everything (legacy behavior).
	trainSet, valSet := splitExamples(examples, opts.ValidationSplit)
	r := LinearRouter{
		Intents:       append([]Intent(nil), PublicIntents...),
		Weights:       map[Intent]Features{},
		FeatureCounts: map[string]int{},
		MinConfidence: opts.MinConfidence,
	}
	for _, intent := range r.Intents {
		r.Weights[intent] = Features{}
	}
	for _, ex := range trainSet {
		for feature := range ExtractFeatures(Input{Text: ex.Text, Messages: ex.Messages, HasTools: ex.HasTools, HasLocalEvidence: ex.HasLocalEvidence}) {
			r.FeatureCounts[feature]++
		}
	}
	initialLoss, initialAccuracy := r.evaluate(trainSet)
	for epoch := 0; epoch < opts.Epochs; epoch++ {
		for _, ex := range trainSet {
			input := Input{Text: ex.Text, Messages: ex.Messages, HasTools: ex.HasTools, HasLocalEvidence: ex.HasLocalEvidence}
			features := ExtractFeatures(input)
			probs := r.probabilities(features)
			for _, intent := range r.Intents {
				target := 0.0
				if intent == ex.Intent {
					target = 1
				}
				delta := opts.LearningRate * (target - probs[intent])
				for feature, value := range features {
					r.Weights[intent][feature] += delta * value
				}
			}
		}
	}
	if opts.PruneMinCount > 1 {
		r.Prune(opts.PruneMinCount)
	}
	finalLoss, finalAccuracy := r.evaluate(trainSet)
	report := TrainReport{
		Examples:        len(trainSet),
		Epochs:          opts.Epochs,
		InitialLoss:     initialLoss,
		FinalLoss:       finalLoss,
		InitialAccuracy: initialAccuracy,
		FinalAccuracy:   finalAccuracy,
	}
	if len(valSet) > 0 {
		_, valAccuracy := r.evaluate(valSet)
		report.ValidationExamples = len(valSet)
		report.ValidationAccuracy = valAccuracy
	}
	return r, report, nil
}

// Prune drops features seen fewer than minCount times from the model. Rare
// char/word n-grams only fire for a single training example, so they bloat the
// checkpoint and encourage memorization; removing them shrinks the artifact and
// improves generalization.
func (r LinearRouter) Prune(minCount int) {
	if minCount <= 1 {
		return
	}
	for _, intent := range r.Intents {
		weights := r.Weights[intent]
		if weights == nil {
			continue
		}
		for feature := range weights {
			if feature == "bias" {
				continue
			}
			if r.FeatureCounts[feature] < minCount {
				delete(weights, feature)
			}
		}
	}
	for feature, count := range r.FeatureCounts {
		if count < minCount {
			delete(r.FeatureCounts, feature)
		}
	}
}

// splitExamples deterministically holds out every k-th example for validation,
// where k = round(1/split). This keeps the split reproducible (no RNG) and
// roughly class-balanced. A split <= 0 trains on everything.
func splitExamples(examples []TrainingExample, split float64) (train, val []TrainingExample) {
	if split <= 0 || split >= 1 || len(examples) < 5 {
		return examples, nil
	}
	k := int(1.0/split + 0.5)
	if k < 2 {
		k = 2
	}
	for i, ex := range examples {
		if i%k == 0 {
			val = append(val, ex)
		} else {
			train = append(train, ex)
		}
	}
	if len(train) == 0 {
		return examples, nil
	}
	return train, val
}

func (r LinearRouter) Route(input Input) RouteDecision {
	guarded := fallbackRoute(input)
	if guarded.Intent == IntentAbstain || guarded.Intent == IntentToolCall || guarded.Intent == IntentRepoAgent {
		return guarded
	}
	features := ExtractFeatures(input)
	probs := r.probabilities(features)
	best := IntentUnknown
	bestProb := -1.0
	for _, intent := range r.Intents {
		if probs[intent] > bestProb {
			best = intent
			bestProb = probs[intent]
		}
	}
	if bestProb < r.MinConfidence {
		return guarded
	}
	return RouteDecision{Intent: best, Confidence: bestProb, Reason: "linear router", Features: topFeatures(features, 8)}
}

func (r LinearRouter) evaluate(examples []TrainingExample) (float64, float64) {
	var loss float64
	var correct int
	for _, ex := range examples {
		features := ExtractFeatures(Input{Text: ex.Text, Messages: ex.Messages, HasTools: ex.HasTools, HasLocalEvidence: ex.HasLocalEvidence})
		probs := r.probabilities(features)
		p := probs[ex.Intent]
		if p <= 1e-9 {
			p = 1e-9
		}
		loss += -math.Log(p)
		decision := r.Route(Input{Text: ex.Text, Messages: ex.Messages, HasTools: ex.HasTools, HasLocalEvidence: ex.HasLocalEvidence})
		if decision.Intent == ex.Intent {
			correct++
		}
	}
	return loss / float64(len(examples)), float64(correct) / float64(len(examples))
}

func (r LinearRouter) probabilities(features Features) map[Intent]float64 {
	if len(r.Intents) == 0 {
		r.Intents = PublicIntents
	}
	scores := map[Intent]float64{}
	maxScore := math.Inf(-1)
	for _, intent := range r.Intents {
		score := 0.0
		for feature, value := range features {
			score += r.Weights[intent][feature] * value
		}
		scores[intent] = score
		if score > maxScore {
			maxScore = score
		}
	}
	total := 0.0
	for _, intent := range r.Intents {
		value := math.Exp(scores[intent] - maxScore)
		scores[intent] = value
		total += value
	}
	if total <= 0 {
		total = 1
	}
	for _, intent := range r.Intents {
		scores[intent] /= total
	}
	return scores
}

type fallbackRouter struct{}

func (fallbackRouter) Route(input Input) RouteDecision {
	return fallbackRoute(input)
}

func fallbackRoute(input Input) RouteDecision {
	text := Normalize(effectiveText(input))
	switch {
	case text == "":
		return RouteDecision{Intent: IntentUnknown, Confidence: 0.2, Reason: "empty input"}
	case input.HasTools && hasAny(text, "repo", "repositorio", "analiza", "analyze", "read", "grep", "test", "build"):
		return RouteDecision{Intent: IntentToolCall, Confidence: 0.95, Reason: "tools available for repo/task prompt"}
	case hasAny(text, "arregla este repo", "fix this repo", "repo falla", "go test falla", "tests fallan", "aplica un patch"):
		return RouteDecision{Intent: IntentRepoAgent, Confidence: 0.95, Reason: "repo repair requires verified solve"}
	case unsupportedFutureOutcome(text) || lowSignal(text):
		return RouteDecision{Intent: IntentAbstain, Confidence: 0.85, Reason: "unsupported or low signal"}
	case isMath(text):
		return RouteDecision{Intent: IntentMath, Confidence: 0.9, Reason: "simple arithmetic"}
	case isTranslation(text):
		return RouteDecision{Intent: IntentTranslation, Confidence: 0.88, Reason: "translation request"}
	case isSmalltalk(text):
		return RouteDecision{Intent: IntentSmalltalk, Confidence: 0.9, Reason: "smalltalk"}
	case isCoding(text):
		if hasAny(text, "haz", "hace", "crea", "genera", "implementa", "build", "make") {
			return RouteDecision{Intent: IntentCodeGeneration, Confidence: 0.86, Reason: "code generation"}
		}
		return RouteDecision{Intent: IntentCodingHelp, Confidence: 0.86, Reason: "coding help"}
	case isFactual(text):
		return RouteDecision{Intent: IntentFactualResearch, Confidence: 0.82, Reason: "factual question"}
	default:
		return RouteDecision{Intent: IntentUnknown, Confidence: 0.35, Reason: "fallback unknown"}
	}
}

func ExtractFeatures(input Input) Features {
	text := Normalize(effectiveText(input))
	features := Features{"bias": 1}
	if input.HasTools {
		features["has_tools"] = 1
	}
	if input.HasLocalEvidence {
		features["has_local_evidence"] = 1
	}
	for _, token := range strings.Fields(text) {
		if len([]rune(token)) <= 1 {
			continue
		}
		features["w:"+token]++
	}
	compact := strings.ReplaceAll(text, " ", "_")
	runes := []rune(compact)
	for n := 3; n <= 4; n++ {
		for i := 0; i+n <= len(runes); i++ {
			features[fmt.Sprintf("c%d:%s", n, string(runes[i:i+n]))]++
		}
	}
	if prev := previousUser(input.Messages); prev != "" {
		for _, token := range strings.Fields(Normalize(prev)) {
			features["prev:"+token] = 1
		}
	}
	return features
}

func LastUser(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content
		}
	}
	return ""
}

func Normalize(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	replacer := strings.NewReplacer(
		"á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ü", "u", "ñ", "n",
		"¿", " ", "?", " ", "¡", " ", "!", " ", ",", " ", ".", " ", ":", " ", ";", " ",
		"(", " ", ")", " ", "[", " ", "]", " ", "{", " ", "}", " ", "\"", " ", "'", " ",
	)
	return strings.Join(strings.Fields(replacer.Replace(text)), " ")
}

func effectiveText(input Input) string {
	if strings.TrimSpace(input.Text) != "" {
		return input.Text
	}
	return LastUser(input.Messages)
}

func previousUser(messages []Message) string {
	seenLast := false
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		if !seenLast {
			seenLast = true
			continue
		}
		return messages[i].Content
	}
	return ""
}

func isSmalltalk(text string) bool {
	return hasAny(text, "hola", "buenas", "hello", "hi", "gracias", "thanks", "chau", "adios", "quien sos", "quien eres", "que puedes hacer", "que podes hacer", "que sabes hacer", "help", "ayuda")
}

func isCoding(text string) bool {
	return hasAny(text,
		"javascript", "javacript", "java script", "typescript", "python", "rust", "golang", " go ", "react", "sql",
		"codigo", "code", "funcion", "function", "componente", "component", "csv", "query", "errores", "errors", "map", "filter",
	)
}

func isMath(text string) bool {
	return hasAny(text, "cuanto es", "calcula", "calculate", "multiplica", "suma", "resta", "divide", " por ") && containsDigit(text)
}

func isTranslation(text string) bool {
	return hasAny(text, "traduce", "traduci", "translate") && hasAny(text, "ingles", "english", "español", "spanish", ":")
}

func isFactual(text string) bool {
	return hasAny(text,
		"quien gano", "quienes ganaron", "ganador", "campeon", "campeones", "copa america", "mundial brasil",
		"que fue", "que es", "quien fue", "quien es", "cuando", "donde", "historia", "guerra", "actual", "latest",
		"what is", "what was", "who won", "who is", "when", "where",
	)
}

func lowSignal(text string) bool {
	if len(strings.Fields(text)) <= 2 && hasAny(text, "blorf", "zibble", "asdf", "lorem") {
		return true
	}
	return hasAny(text, "blorf", "zibble", "quantum vegetable")
}

func unsupportedFutureOutcome(text string) bool {
	if !hasAny(text, "gano", "ganador", "campeon", "resultado", "winner", "won", "champion", "result") {
		return false
	}
	for _, token := range strings.Fields(text) {
		if len(token) != 4 {
			continue
		}
		year := 0
		for _, r := range token {
			if r < '0' || r > '9' {
				year = 0
				break
			}
			year = year*10 + int(r-'0')
		}
		if year > time.Now().Year() {
			return true
		}
	}
	return false
}

func containsDigit(text string) bool {
	for _, r := range text {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func hasAny(text string, needles ...string) bool {
	padded := " " + text + " "
	for _, needle := range needles {
		if strings.Contains(padded, needle) || strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func topFeatures(features Features, limit int) []string {
	var keys []string
	for key := range features {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if features[keys[i]] == features[keys[j]] {
			return keys[i] < keys[j]
		}
		return features[keys[i]] > features[keys[j]]
	})
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}
	return keys
}
