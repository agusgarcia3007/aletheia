package learning

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aletheia/internal/memory"
)

func TestHarvestChatDatasetBuildsGroundedPairs(t *testing.T) {
	ctx := context.Background()
	store, err := memory.Open(filepath.Join(t.TempDir(), "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	// A good verified answer -> becomes a training pair.
	if _, err := store.CreateResearchJob(ctx, memory.ResearchJob{
		ID: "j-good", Query: "que es una derivada", Status: "completed",
		Answer:     "La derivada de una funcion es la razon de cambio instantanea.\n\nFuentes:\n- https://es.wikipedia.org/wiki/Derivada",
		Confidence: 0.85,
	}); err != nil {
		t.Fatal(err)
	}
	// A stub/abstention -> must be skipped.
	if _, err := store.CreateResearchJob(ctx, memory.ResearchJob{
		ID: "j-stub", Query: "que es xyz", Status: "completed",
		Answer: "No tengo evidencia suficiente para responder eso.", Confidence: 0.9,
	}); err != nil {
		t.Fatal(err)
	}
	// Low confidence -> skipped.
	if _, err := store.CreateResearchJob(ctx, memory.ResearchJob{
		ID: "j-low", Query: "algo dudoso", Status: "completed",
		Answer: "Una respuesta poco confiable pero larga de verdad.", Confidence: 0.1,
	}); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "chat.jsonl")
	n, err := HarvestChatDataset(ctx, store, out, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("harvested %d examples, want 1", n)
	}

	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Scan()
	var ex chatTrainingExample
	if err := json.Unmarshal(scanner.Bytes(), &ex); err != nil {
		t.Fatal(err)
	}
	if ex.Prompt != "<USER>que es una derivada<ASSISTANT>" {
		t.Fatalf("prompt = %q", ex.Prompt)
	}
	if !strings.HasSuffix(ex.Completion, "<EOS>") || strings.Contains(ex.Completion, "Fuentes:") {
		t.Fatalf("completion = %q", ex.Completion)
	}
	if !strings.Contains(ex.Completion, "razon de cambio") {
		t.Fatalf("completion lost the answer: %q", ex.Completion)
	}
}
