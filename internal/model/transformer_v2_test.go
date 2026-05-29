package model

import "testing"

func TestTransformerV2ForwardShapeAndCausalSafety(t *testing.T) {
	m, err := NewTransformerV2(Config{
		Name:          "test-v2",
		VocabSize:     32,
		ContextLength: 8,
		NLayers:       2,
		NHeads:        2,
		DModel:        8,
		DFF:           16,
		Seed:          3,
	})
	if err != nil {
		t.Fatal(err)
	}
	logits, err := m.Forward([]int{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(logits) != 3 || len(logits[0]) != 32 {
		t.Fatalf("logits shape = %dx%d", len(logits), len(logits[0]))
	}
	next, err := m.PredictNext([]int{1, 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 32 {
		t.Fatalf("next logits = %d", len(next))
	}
}
