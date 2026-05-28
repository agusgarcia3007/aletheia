package retriever

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aletheia/internal/memory"
)

func TestIndexerIndexesSupportedFilesAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "docs", "decisions.md"), "# Decisions\n\nUse a heuristic selector for safe fallback.\n")
	mustWrite(t, filepath.Join(root, "docs", "notes.txt"), "Local memory stores causal evidence.\n")
	mustWrite(t, filepath.Join(root, "docs", "skip.bin"), string([]byte{0, 1, 2}))
	mustWrite(t, filepath.Join(root, ".git", "ignored.md"), "ignore me")
	mustWrite(t, filepath.Join(root, "checkpoints", "ignored.md"), "ignore me")

	store := openStore(t)
	report, err := (Indexer{Store: store}).IndexPath(ctx, root, IndexOptions{ChunkSize: 24, ChunkOverlap: 4, MaxFileBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if report.Scanned != 3 || report.Indexed != 2 || report.ChunksWritten == 0 {
		t.Fatalf("report = %+v", report)
	}
	stats, err := store.Inspect(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Documents != 2 || stats.Chunks == 0 || stats.Nodes == 0 || stats.Edges == 0 {
		t.Fatalf("stats = %+v", stats)
	}
	second, err := (Indexer{Store: store}).IndexPath(ctx, root, IndexOptions{ChunkSize: 24, ChunkOverlap: 4, MaxFileBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if second.Indexed != 0 || second.SkippedUnchanged != 2 || second.ChunksWritten != 0 {
		t.Fatalf("second report = %+v", second)
	}
}

func TestChunkOffsetsOverlap(t *testing.T) {
	chunks := chunkText("abcdefghijklmnopqrstuvwxyz", 10, 2)
	if len(chunks) != 3 {
		t.Fatalf("chunks len = %d", len(chunks))
	}
	want := [][2]int{{0, 10}, {8, 18}, {16, 26}}
	for i := range chunks {
		got := [2]int{chunks[i].OffsetStart, chunks[i].OffsetEnd}
		if got != want[i] {
			t.Fatalf("chunk %d = %v, want %v", i, got, want[i])
		}
	}
}

func TestIndexerCanDisableGraphWrites(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "docs", "decisions.md"), "# Decisions\n\nGraph can be disabled for config smoke tests.\n")
	store := openStore(t)
	graphEnabled := false
	if _, err := (Indexer{Store: store}).IndexPath(ctx, root, IndexOptions{GraphEnabled: &graphEnabled}); err != nil {
		t.Fatal(err)
	}
	stats, err := store.Inspect(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Documents != 1 || stats.Chunks == 0 {
		t.Fatalf("documents/chunks not indexed: %+v", stats)
	}
	if stats.Nodes != 0 || stats.Edges != 0 {
		t.Fatalf("graph should be disabled: %+v", stats)
	}
}

func TestRetrieverSearchAndAnswer(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "docs", "decisions.md"), `# Decisions

The selector decision was to use a heuristic selector that can fall back safely when model candidates are missing or invalid.

The verifier bus stores structured evidence.
`)
	store := openStore(t)
	if _, err := (Indexer{Store: store}).IndexPath(ctx, root, IndexOptions{}); err != nil {
		t.Fatal(err)
	}
	r := Retriever{Store: store}
	hits, err := r.Search(ctx, "decision selector fallback", SearchOptions{TopK: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 || !strings.Contains(hits[0].Text, "heuristic selector") {
		t.Fatalf("hits = %+v", hits)
	}
	if hits[0].KeywordScore <= 0 || hits[0].SemanticScore == 0 {
		t.Fatalf("score breakdown = %+v", hits[0])
	}
	answer, err := r.Answer(ctx, "qué decisión tomamos sobre el selector?", SearchOptions{TopK: 2})
	if err != nil {
		t.Fatal(err)
	}
	if answer.Status != "answered" || !strings.Contains(answer.Text, "heuristic selector") || len(answer.Citations) == 0 {
		t.Fatalf("answer = %+v", answer)
	}
	noHit, err := r.Answer(ctx, "zqxj impossible unrelated query", SearchOptions{TopK: 1, MinConfidence: 99})
	if err != nil {
		t.Fatal(err)
	}
	if noHit.Status != "abstained" {
		t.Fatalf("no-hit answer = %+v", noHit)
	}
}

func openStore(t *testing.T) *memory.Store {
	t.Helper()
	store, err := memory.Open(filepath.Join(t.TempDir(), "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return store
}

func mustWrite(t *testing.T, path string, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
