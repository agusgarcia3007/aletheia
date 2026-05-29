package apiserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"aletheia/internal/memory"
)

// TestChatHarvestsVerifiedRouterLabels proves the server records verified
// routing labels (from deterministic guardrails) into memory during real chat,
// which `aletheia learn` later harvests to improve the router.
func TestChatHarvestsVerifiedRouterLabels(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	writeTestCheckpoint(t, filepath.Join(root, "aletheia-mikros"), "aletheia-mikros", 1, []string{
		`{"prompt":"<USER>hola<ASSISTANT>","completion":"Hola.<EOS>"}`,
	})
	server, err := New(Options{APIKey: "secret", CheckpointsDir: root, Store: store, KnowledgePath: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]string{
		"cuanto es 12 por 12?":             "math",
		"quien es el presidente de chile?": "factual_research",
		"blorf zibble":                     "abstain",
	}
	for q := range want {
		_ = chatContent(t, server, q)
	}

	nodes, err := store.GraphNodes(context.Background(), "router_example")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, n := range nodes {
		var p map[string]string
		if err := json.Unmarshal([]byte(n.Payload), &p); err != nil {
			t.Fatal(err)
		}
		got[p["text"]] = p["intent"]
	}
	for q, intent := range want {
		if got[q] != intent {
			t.Errorf("query %q recorded intent %q, want %q (all=%v)", q, got[q], intent, got)
		}
	}
	_ = memory.Node{}
}
