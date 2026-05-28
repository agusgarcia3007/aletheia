package memory

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigrateAndRecordEvidence(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	episodeID, err := store.CreateEpisode(ctx, "fix failing test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordEvidence(ctx, Evidence{
		EpisodeID: episodeID,
		Verifier:  "go_test",
		Status:    "pass",
		Score:     1,
		Stdout:    "ok",
	}); err != nil {
		t.Fatal(err)
	}

	count, err := store.EvidenceCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("evidence count = %d, want 1", count)
	}
}
