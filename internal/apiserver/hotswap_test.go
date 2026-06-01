package apiserver

import (
	"path/filepath"
	"testing"

	"aletheia/internal/model"
	"aletheia/internal/tokenizer"
)

func TestSwapModelHotReloads(t *testing.T) {
	store := newTestStore(t)
	server := newTestServer(t, Options{APIKey: "secret", Store: store})

	before, ok := server.model(mikrosModelName)
	if !ok {
		t.Fatal("base model missing")
	}

	// Build a distinct checkpoint (trained: step > 0) and hot-swap it in.
	ckpt := filepath.Join(t.TempDir(), "aletheia-mikros-gen")
	tok := tokenizer.New()
	m, err := model.New(model.Config{
		Name: mikrosModelName, VocabSize: tok.VocabSize(), ContextLength: 64,
		NLayers: 1, NHeads: 2, DModel: 16, DFF: 32, Seed: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Save(ckpt, tok.VocabSize(), 5, 0.5); err != nil {
		t.Fatal(err)
	}

	if err := server.swapModel(mikrosModelName, ckpt); err != nil {
		t.Fatalf("swap: %v", err)
	}
	after, ok := server.model(mikrosModelName)
	if !ok {
		t.Fatal("model missing after swap")
	}
	if after.Checkpoint != ckpt {
		t.Fatalf("checkpoint not swapped: got %q", after.Checkpoint)
	}
	if after.Manifest.Step != 5 {
		t.Fatalf("swapped manifest step = %d, want 5", after.Manifest.Step)
	}
	if before.Checkpoint == after.Checkpoint {
		t.Fatal("expected checkpoint to change after swap")
	}
}
