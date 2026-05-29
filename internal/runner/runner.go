package runner

import (
	"fmt"
	"math"
	"math/rand"
	"sort"

	"aletheia/internal/model"
	"aletheia/internal/tensor"
	"aletheia/internal/tokenizer"
)

type Options struct {
	Temperature float64
	TopK        int
	TopP        float64
	MaxTokens   int
	StopTokens  []int
	Seed        int64
}

type Candidate struct {
	TokenID int
	Token   string
	Logit   float32
	LogProb float64
}

type Runner struct {
	Model     *model.Model
	Tokenizer *tokenizer.Tokenizer
}

func New(m *model.Model, tok *tokenizer.Tokenizer) Runner {
	return Runner{Model: m, Tokenizer: tok}
}

func (r Runner) Forward(tokens []int) ([][]float32, error) {
	return r.Model.Forward(tokens)
}

func (r Runner) Generate(prompt string, opts Options) ([]int, error) {
	if opts.MaxTokens <= 0 {
		opts.MaxTokens = 32
	}
	tokens := r.Tokenizer.Encode(prompt)
	if len(tokens) == 0 {
		return nil, fmt.Errorf("prompt produced no tokens")
	}
	stop := make(map[int]bool, len(opts.StopTokens))
	for _, id := range opts.StopTokens {
		stop[id] = true
	}
	for i := 0; i < opts.MaxTokens; i++ {
		if len(tokens) >= r.Model.Config.ContextLength {
			break
		}
		logits, err := r.Model.PredictNext(tokens)
		if err != nil {
			return nil, err
		}
		next, err := r.nextToken(logits, opts, i)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, next)
		if stop[next] {
			break
		}
	}
	return tokens, nil
}

func (r Runner) TopK(logits []float32, k int) ([]Candidate, error) {
	limit := r.validVocabSize(logits)
	if k <= 0 || k > limit {
		k = limit
	}
	probs, err := tensor.Softmax(logits[:limit])
	if err != nil {
		return nil, err
	}
	indices := make([]int, limit)
	for i := range indices {
		indices[i] = i
	}
	sort.Slice(indices, func(i, j int) bool {
		return logits[indices[i]] > logits[indices[j]]
	})
	out := make([]Candidate, 0, k)
	for _, id := range indices[:k] {
		token, ok := r.Tokenizer.Token(id)
		if !ok && id >= 0 && id < tokenizer.ByteVocabSize {
			token = string(byte(id))
		} else if !ok {
			token = "<UNK>"
		}
		out = append(out, Candidate{
			TokenID: id,
			Token:   token,
			Logit:   logits[id],
			LogProb: math.Log(float64(probs[id])),
		})
	}
	return out, nil
}

func (r Runner) Score(tokens []int) (float64, error) {
	if len(tokens) < 2 {
		return 0, nil
	}
	var score float64
	for pos := 0; pos < len(tokens)-1; pos++ {
		logits, err := r.Model.PredictNext(tokens[:pos+1])
		if err != nil {
			return 0, err
		}
		probs, err := tensor.Softmax(logits)
		if err != nil {
			return 0, err
		}
		target := tokens[pos+1]
		if target < 0 || target >= len(probs) {
			return 0, fmt.Errorf("target token %d outside logits", target)
		}
		score += math.Log(float64(probs[target]))
	}
	return score, nil
}

func (r Runner) nextToken(logits []float32, opts Options, step int) (int, error) {
	limit := r.validVocabSize(logits)
	logits = logits[:limit]
	if opts.Temperature <= 0 {
		return greedy(logits), nil
	}
	topP := opts.TopP
	if topP <= 0 || topP > 1 {
		topP = 1
	}
	scaled := make([]float32, len(logits))
	temp := float32(opts.Temperature)
	for i, logit := range logits {
		scaled[i] = logit / temp
	}
	probs, err := tensor.Softmax(scaled)
	if err != nil {
		return 0, err
	}
	indices := make([]int, len(probs))
	for i := range indices {
		indices[i] = i
	}
	sort.Slice(indices, func(i, j int) bool {
		return probs[indices[i]] > probs[indices[j]]
	})
	if opts.TopK > 0 && opts.TopK < len(indices) {
		indices = indices[:opts.TopK]
	}
	var nucleus []int
	cumulative := float32(0)
	for _, id := range indices {
		nucleus = append(nucleus, id)
		cumulative += probs[id]
		if float64(cumulative) >= topP {
			break
		}
	}
	if len(nucleus) == 0 {
		return greedy(logits), nil
	}
	total := 0.0
	for _, id := range nucleus {
		total += float64(probs[id])
	}
	if total <= 0 {
		return greedy(logits), nil
	}
	seed := opts.Seed
	if seed == 0 {
		seed = r.Model.Config.Seed
	}
	rng := rand.New(rand.NewSource(seed + int64(step+1)*7919))
	draw := rng.Float64() * total
	for _, id := range nucleus {
		draw -= float64(probs[id])
		if draw <= 0 {
			return id, nil
		}
	}
	return nucleus[len(nucleus)-1], nil
}

func greedy(logits []float32) int {
	best := 0
	for i := 1; i < len(logits); i++ {
		if logits[i] > logits[best] {
			best = i
		}
	}
	return best
}

func (r Runner) validVocabSize(logits []float32) int {
	limit := len(logits)
	if r.Tokenizer != nil && r.Tokenizer.VocabSize() < limit {
		limit = r.Tokenizer.VocabSize()
	}
	if limit <= 0 {
		return len(logits)
	}
	return limit
}
