package learning

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"aletheia/internal/memory"
)

// TestRouterHarvestAndPromotionGate proves the learning loop: harvested
// real-usage router examples are exported, a candidate is trained on
// base+harvested, and it is promoted only if it does not regress on a shared
// held-out set.
func TestRouterHarvestAndPromotionGate(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "memory.sqlite")
	store, err := memory.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}

	harvested := []struct{ text, intent string }{
		{"cuanto es 12 por 12?", "math"},
		{"cuanto es 5 mas 9?", "math"},
		{"en python como ordeno una lista?", "coding_help"},
		{"como hago una funcion en go?", "coding_help"},
		{"quien es el presidente de brasil?", "factual_research"},
		{"cual es la capital de chile?", "factual_research"},
		{"blorf zibble", "abstain"},
		{"hola, todo bien?", "smalltalk"},
	}
	for _, h := range harvested {
		payload, _ := json.Marshal(map[string]string{"text": h.text, "intent": h.intent})
		if _, err := store.EnsureNode(context.Background(), "router_example", "router:"+h.text, string(payload)); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()

	base, err := filepath.Abs("../../datasets/router_mikros.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	routerOut := filepath.Join(t.TempDir(), "router-candidate")
	report, err := Run(context.Background(), Options{
		DBPath:            dbPath,
		OutDir:            t.TempDir(),
		TrainRouterOut:    routerOut,
		RouterBaseDataset: base,
		Epochs:            120,
	})
	if err != nil {
		t.Fatalf("learn failed: %v", err)
	}
	if report.RouterExamples != len(harvested) {
		t.Fatalf("expected %d harvested examples, got %d", len(harvested), report.RouterExamples)
	}
	if report.RouterPromotionReason == "" {
		t.Fatal("expected a promotion decision")
	}
	t.Logf("examples=%d base=%.3f cand=%.3f promoted=%v reason=%q",
		report.RouterExamples, report.RouterBaseAccuracy, report.RouterCandidateAcc, report.RouterPromoted, report.RouterPromotionReason)

	if report.RouterPromoted && report.RouterCandidateAcc+1e-9 < report.RouterBaseAccuracy {
		t.Fatalf("promoted a worse router: cand %.3f < base %.3f", report.RouterCandidateAcc, report.RouterBaseAccuracy)
	}
}
