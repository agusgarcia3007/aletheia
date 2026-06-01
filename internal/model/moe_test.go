package model

import (
	"math"
	"testing"
)

func moeConfig(numExperts, topK int) Config {
	return Config{
		Name:          "moe-test",
		VocabSize:     32,
		ContextLength: 16,
		NLayers:       2,
		NHeads:        2,
		DModel:        8,
		DFF:           16,
		Seed:          7,
		NumExperts:    numExperts,
		TopKExperts:   topK,
	}
}

func TestTransformerV2MoEForwardIsValidAndDeterministic(t *testing.T) {
	m, err := NewTransformerV2(moeConfig(4, 2))
	if err != nil {
		t.Fatal(err)
	}

	for i, layer := range m.Layers {
		if len(layer.Experts) != 4 {
			t.Fatalf("layer %d has %d experts, want 4", i, len(layer.Experts))
		}
		if len(layer.MoEGate) != m.Config.DModel*m.Config.NumExperts {
			t.Fatalf("layer %d gate size %d", i, len(layer.MoEGate))
		}
	}
	tokens := []int{1, 5, 9, 2}
	a, err := m.Forward(tokens)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(tokens) || len(a[0]) != m.Config.VocabSize {
		t.Fatalf("unexpected logits shape %dx%d", len(a), len(a[0]))
	}
	for _, row := range a {
		for _, v := range row {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				t.Fatalf("non-finite logit %v", v)
			}
		}
	}
	b, err := m.Forward(tokens)
	if err != nil {
		t.Fatal(err)
	}
	for i := range a {
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				t.Fatalf("MoE forward not deterministic at [%d][%d]", i, j)
			}
		}
	}
}

func TestTransformerV2DenseWhenNoExperts(t *testing.T) {
	m, err := NewTransformerV2(moeConfig(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	for i, layer := range m.Layers {
		if len(layer.Experts) != 0 || layer.MoEGate != nil {
			t.Fatalf("layer %d should be dense (no experts)", i)
		}
	}
	if _, err := m.Forward([]int{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
}

func TestTopKExpertsClampAndIndices(t *testing.T) {
	m, _ := NewTransformerV2(moeConfig(3, 9))
	if got := m.topKExperts(); got != 3 {
		t.Fatalf("topKExperts clamp = %d, want 3", got)
	}
	idx := topKIndices([]float32{0.1, 0.9, 0.3, 0.7}, 2)
	if len(idx) != 2 || idx[0] != 1 || idx[1] != 3 {
		t.Fatalf("topKIndices = %v, want [1 3]", idx)
	}
}

func TestMoEAuxLoss(t *testing.T) {

	frac := []float32{0.25, 0.25, 0.25, 0.25}
	prob := []float32{0.25, 0.25, 0.25, 0.25}
	if loss := MoEAuxLoss(frac, prob); math.Abs(float64(loss)-1.0) > 1e-6 {
		t.Fatalf("balanced aux loss = %v, want 1.0", loss)
	}

	collapsed := MoEAuxLoss([]float32{1, 0, 0, 0}, []float32{1, 0, 0, 0})
	if collapsed <= 1.0 {
		t.Fatalf("collapsed aux loss = %v, want > 1.0", collapsed)
	}
}
