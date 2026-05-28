package model

import (
	"math"
	"path/filepath"
	"testing"
)

func testConfig() Config {
	return Config{
		Name:          "test",
		VocabSize:     64,
		ContextLength: 16,
		NLayers:       1,
		NHeads:        2,
		DModel:        12,
		DFF:           24,
		Seed:          3,
	}
}

func TestForwardShapeAndCausalMask(t *testing.T) {
	m, err := New(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	a, err := m.Forward([]int{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Forward([]int{1, 2, 4})
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 3 || len(a[0]) != m.Config.VocabSize {
		t.Fatalf("bad logits shape")
	}
	for pos := 0; pos < 2; pos++ {
		for i := range a[pos] {
			if a[pos][i] != b[pos][i] {
				t.Fatalf("future token changed logits at pos %d", pos)
			}
		}
	}
}

func TestTrainBatchReducesLoss(t *testing.T) {
	m, err := New(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	samples := []Sample{{
		Tokens:   []int{1, 2, 3, 4},
		LossMask: []bool{false, true, true, true},
	}}
	initial, err := m.Evaluate(samples)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 80; i++ {
		if _, err := m.TrainBatch(samples, 0.08, 0, 5); err != nil {
			t.Fatal(err)
		}
	}
	final, err := m.Evaluate(samples)
	if err != nil {
		t.Fatal(err)
	}
	if final.Loss >= initial.Loss {
		t.Fatalf("loss did not drop: initial=%v final=%v", initial.Loss, final.Loss)
	}
}

func TestCheckpointRoundTrip(t *testing.T) {
	m, err := New(testConfig())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "ckpt")
	if err := m.Save(dir, 27, 9, 1.25); err != nil {
		t.Fatal(err)
	}
	loaded, manifest, err := Load(dir, 27)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Step != 9 || math.Abs(manifest.Loss-1.25) > 1e-9 {
		t.Fatalf("bad manifest: %+v", manifest)
	}
	before, err := m.PredictNext([]int{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	after, err := loaded.PredictNext([]int{1, 2, 3})
	if err != nil {
		t.Fatal(err)
	}
	for i := range before {
		if math.Abs(float64(before[i]-after[i])) > 1e-7 {
			t.Fatalf("logit %d changed: %v vs %v", i, before[i], after[i])
		}
	}
	if _, _, err := Load(dir, 28); err == nil {
		t.Fatal("expected tokenizer vocab mismatch")
	}
}
