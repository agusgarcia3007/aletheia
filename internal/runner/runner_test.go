package runner

import (
	"testing"

	"aletheia/internal/model"
	"aletheia/internal/tokenizer"
)

func TestTopKAndScore(t *testing.T) {
	tok := tokenizer.New()
	m, err := model.New(model.Config{
		Name:          "runner-test",
		VocabSize:     512,
		ContextLength: 16,
		NLayers:       1,
		NHeads:        2,
		DModel:        16,
		DFF:           32,
		Seed:          8,
	})
	if err != nil {
		t.Fatal(err)
	}
	r := New(m, tok)
	prompt := tok.Encode("<USER>x<ASSISTANT>")
	logits, err := m.PredictNext(prompt)
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := r.TopK(logits, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 5 {
		t.Fatalf("topk len = %d", len(candidates))
	}
	if candidates[0].LogProb > 0 {
		t.Fatalf("logprob should be <= 0: %v", candidates[0].LogProb)
	}
	score, err := r.Score(prompt)
	if err != nil {
		t.Fatal(err)
	}
	if score >= 0 {
		t.Fatalf("score = %v, want negative logprob sum", score)
	}
}

func TestGenerateRestrictsToTokenizerVocabulary(t *testing.T) {
	tok := tokenizer.New()
	m, err := model.New(model.Config{
		Name:          "runner-vocab-test",
		VocabSize:     512,
		ContextLength: 32,
		NLayers:       1,
		NHeads:        2,
		DModel:        16,
		DFF:           32,
		Seed:          9,
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := tok.VocabSize(); i < len(m.Bias); i++ {
		m.Bias[i] = 100
	}
	r := New(m, tok)
	tokens, err := r.Generate("<USER>x<ASSISTANT>", Options{MaxTokens: 4})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range tokens {
		if id >= tok.VocabSize() {
			t.Fatalf("generated token id %d outside tokenizer vocab %d", id, tok.VocabSize())
		}
	}
	if _, err := tok.Decode(tokens); err != nil {
		t.Fatal(err)
	}
}
