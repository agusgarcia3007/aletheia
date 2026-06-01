package apiserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aletheia/internal/memory"
	"aletheia/internal/research"
)

// TestCodingLearnOnDemandLoop proves the "I don't know -> learn -> answer ->
// it sticks" loop for a language Aletheia does NOT ship knowledge for (Swift),
// fully offline using a fake search provider.
func TestCodingLearnOnDemandLoop(t *testing.T) {
	store := newTestStore(t)
	root := t.TempDir()
	writeTestCheckpoint(t, filepath.Join(root, "aletheia-mikros"), "aletheia-mikros", 1, []string{
		`{"prompt":"<USER>hola<ASSISTANT>","completion":"Hola.<EOS>"}`,
	})
	opts := Options{
		APIKey:         "secret",
		CheckpointsDir: root,
		Store:          store,
		KnowledgePath:  t.TempDir(),
		Research: research.Options{
			Enabled:               true,
			AutoOnKnowledgeGap:    true,
			MaxSources:            2,
			MinSourcesForVerified: 1,
			MinTrustScore:         0.0,
		},
	}
	server, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}

	const swiftQuery = "como hago un for loop en swift?"

	first := chatContent(t, server, swiftQuery)
	if !strings.Contains(first, "aprendiendo") || !strings.Contains(first, "Volvé a preguntar") {
		t.Fatalf("first answer should start learning, got: %q", first)
	}

	runFakeLearner(t, store, opts.Research, swiftQuery,
		"Swift: for loop",
		"En Swift un for loop se escribe asi: for i in 1...5 { print(i) }. El rango 1...5 es inclusivo y recorre del 1 al 5.")

	second := chatContent(t, server, swiftQuery)
	if strings.Contains(second, "aprendiendo") {
		t.Fatalf("second answer should come from learned memory, still learning: %q", second)
	}
	if !strings.Contains(strings.ToLower(second), "swift") || !strings.Contains(second, "for") {
		t.Fatalf("second answer should contain the learned Swift knowledge, got: %q", second)
	}

	server2, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	third := chatContent(t, server2, swiftQuery)
	if strings.Contains(third, "aprendiendo") || !strings.Contains(strings.ToLower(third), "swift") {
		t.Fatalf("knowledge did not persist across restart, got: %q", third)
	}
}

func chatContent(t *testing.T, server *Server, message string) string {
	t.Helper()
	rec := serveJSON(t, server, "/v1/chat/completions",
		`{"model":"aletheia-mikros","messages":[{"role":"user","content":"`+message+`"}]}`, "secret")
	if rec.Code != 200 {
		t.Fatalf("chat status %d: %s", rec.Code, rec.Body.String())
	}
	content, _ := extractContent(t, rec.Body.String())
	return content
}

// runFakeLearner processes the queued learning job offline with a fake search
// provider + fetcher, simulating what SearXNG-backed learning does in prod.
func runFakeLearner(t *testing.T, store *memory.Store, opts research.Options, query, title, body string) {
	t.Helper()
	ctx := context.Background()
	jobs, err := store.ListResearchJobs(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	var queued memory.ResearchJob
	found := false
	for _, j := range jobs {
		if j.Status == "queued" {
			queued = j
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected a queued learning job")
	}

	url := "https://docs.example.dev/swift/for-loop"
	html := "<html><head><title>" + title + "</title></head><body><p>" + body + "</p></body></html>"
	hash := sha256.Sum256([]byte(html))
	worker := research.Worker{
		Store:    store,
		Searcher: research.FakeSearchProvider{Results: []research.SearchResult{{URL: url, Title: title, Snippet: body, Rank: 1}}},
		Fetcher: research.FakeFetcher{Pages: map[string]research.FetchedPage{
			url: {
				URL: url, FinalURL: url, Body: []byte(html), ContentType: "text/html",
				ByteSize: int64(len(html)), ContentHash: hex.EncodeToString(hash[:]), FetchedAt: time.Unix(1700000000, 0).UTC(),
			},
		}},
		Extractor: research.SimpleHTMLExtractor{},
		Claims:    research.ClaimExtractor{},
		Ranker:    research.SourceRanker{MinTrustScore: opts.MinTrustScore},
		Options:   opts,
	}
	if _, err := worker.RunJob(ctx, queued); err != nil {
		t.Fatalf("learner failed: %v", err)
	}
}
