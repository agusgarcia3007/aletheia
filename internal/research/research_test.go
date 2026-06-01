package research

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aletheia/internal/memory"
	"aletheia/internal/retriever"
)

func TestSearXNGProviderParsesAndDeduplicates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" || r.URL.Query().Get("format") != "json" {
			t.Fatalf("unexpected request: %s", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"One","url":"https://example.com/1","content":"a","engine":"test","score":1},{"title":"Dup","url":"https://example.com/1","content":"b","engine":"test","score":0.5},{"title":"Two","url":"https://example.com/2","content":"c","engine":"test","score":0.4}]}`))
	}))
	defer server.Close()
	results, err := (SearXNGProvider{BaseURL: server.URL}).Search(context.Background(), "mcp", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Rank != 1 || results[1].URL != "https://example.com/2" {
		t.Fatalf("results = %+v", results)
	}
}

func TestSearXNGProviderHandlesErrors(t *testing.T) {
	badStatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer badStatus.Close()
	if _, err := (SearXNGProvider{BaseURL: badStatus.URL}).Search(context.Background(), "x", 1); err == nil {
		t.Fatal("expected non-200 error")
	}

	badJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{`))
	}))
	defer badJSON.Close()
	if _, err := (SearXNGProvider{BaseURL: badJSON.URL}).Search(context.Background(), "x", 1); err == nil {
		t.Fatal("expected invalid JSON error")
	}
}

func TestHTTPFetcherPolicy(t *testing.T) {
	fetcher := HTTPFetcher{BlockedDomains: []string{"blocked.test"}, MaxBytes: 8}
	if _, err := fetcher.Fetch(context.Background(), "https://blocked.test/page"); err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("blocked error = %v", err)
	}

	large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("0123456789abcdef"))
	}))
	defer large.Close()
	if _, err := fetcher.Fetch(context.Background(), large.URL); err == nil || !strings.Contains(err.Error(), "max bytes") {
		t.Fatalf("max bytes error = %v", err)
	}
}

func TestExtractorClaimsAndWorkerStoreEvidence(t *testing.T) {
	ctx := context.Background()
	store, err := memory.Open(filepath.Join(t.TempDir(), "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	job, err := store.CreateResearchJob(ctx, memory.ResearchJob{Query: "what is mcp", MaxSources: 2})
	if err != nil {
		t.Fatal(err)
	}
	page := FetchedPage{
		URL:         "https://docs.example.com/mcp",
		FinalURL:    "https://docs.example.com/mcp",
		StatusCode:  http.StatusOK,
		ContentType: "text/html",
		FetchedAt:   time.Now().UTC(),
		Body:        []byte(`<html><head><title>MCP docs</title><script>noise()</script></head><body><nav>skip</nav><h1>MCP protocol</h1><p>MCP is a protocol for connecting agents to tools and data sources.</p><p>It requires a client and a server.</p></body></html>`),
	}
	worker := NewWorker(store, Options{
		Enabled:               true,
		MaxSources:            2,
		MinSourcesForVerified: 2,
		MinTrustScore:         0.1,
		JobTimeout:            time.Second,
	})
	worker.Searcher = FakeSearchProvider{Results: []SearchResult{{Title: "MCP docs", URL: page.URL, Snippet: "protocol", Rank: 1}}}
	worker.Fetcher = FakeFetcher{Pages: map[string]FetchedPage{page.URL: page}}
	result, err := worker.RunJob(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourcesStored != 1 || result.ClaimsStored == 0 || result.ChunksStored == 0 {
		t.Fatalf("result = %+v", result)
	}
	stats, err := store.Inspect(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Documents != 1 || stats.Chunks == 0 {
		t.Fatalf("stats = %+v", stats)
	}
	claims, err := store.WebClaimsByJob(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) == 0 || !strings.Contains(claims[0].Claim, "MCP") {
		t.Fatalf("claims = %+v", claims)
	}
	answer, err := (retriever.Retriever{Store: store}).Answer(ctx, "what is mcp", retriever.SearchOptions{TopK: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !answer.Verified {
		t.Fatalf("answer = %+v", answer)
	}
}

func TestWorkerBuildsAnswerFromClaimNotTitle(t *testing.T) {
	ctx := context.Background()
	store, err := memory.Open(filepath.Join(t.TempDir(), "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	job, err := store.CreateResearchJob(ctx, memory.ResearchJob{Query: "what is mcp in agents", MaxSources: 1})
	if err != nil {
		t.Fatal(err)
	}
	page := FetchedPage{
		URL:         "https://docs.example.com/mcp",
		FinalURL:    "https://docs.example.com/mcp",
		StatusCode:  http.StatusOK,
		ContentType: "text/html",
		FetchedAt:   time.Now().UTC(),
		Body:        []byte(`<html><head><title>Model Context Protocol</title></head><body><h1>Model Context Protocol</h1><p>The Model Context Protocol lets agents connect to tools and data sources through a standard client-server interface.</p></body></html>`),
	}
	worker := NewWorker(store, Options{Enabled: true, MaxSources: 1, MinTrustScore: 0.1, JobTimeout: time.Second})
	worker.Searcher = FakeSearchProvider{Results: []SearchResult{{Title: "Model Context Protocol", URL: page.URL, Snippet: "agents tools", Rank: 1}}}
	worker.Fetcher = FakeFetcher{Pages: map[string]FetchedPage{page.URL: page}}
	result, err := worker.RunJob(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Answer, "lets agents connect") || strings.Contains(result.Answer, "Evidence status") || strings.Contains(result.Answer, "Source:") {
		t.Fatalf("answer = %q", result.Answer)
	}
}

func TestWorkerFutureOutcomeIsInsufficientEvidence(t *testing.T) {
	ctx := context.Background()
	store, err := memory.Open(filepath.Join(t.TempDir(), "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	job, err := store.CreateResearchJob(ctx, memory.ResearchJob{Query: "quien gano la copa mundial de futbol 2038?", MaxSources: 1})
	if err != nil {
		t.Fatal(err)
	}
	page := FetchedPage{
		URL:         "https://example.com/world-cup-2038",
		FinalURL:    "https://example.com/world-cup-2038",
		StatusCode:  http.StatusOK,
		ContentType: "text/html",
		FetchedAt:   time.Now().UTC(),
		Body:        []byte(`<html><head><title>Copa Mundial 2038</title></head><body><p>La Copa Mundial 2038 todavia no tiene ganador confirmado.</p></body></html>`),
	}
	worker := NewWorker(store, Options{Enabled: true, MaxSources: 1, MinTrustScore: 0.1, JobTimeout: time.Second})
	worker.Searcher = FakeSearchProvider{Results: []SearchResult{{Title: "Copa Mundial 2038", URL: page.URL, Snippet: "ganador", Rank: 1}}}
	worker.Fetcher = FakeFetcher{Pages: map[string]FetchedPage{page.URL: page}}
	result, err := worker.RunJob(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	if result.EvidenceStatus != StatusInsufficientEvidence || result.Confidence != 0 || !strings.Contains(result.Answer, "resultado futuro") {
		t.Fatalf("result = %+v", result)
	}
}

func TestWorkerExtractsSupportedAnswerFromEvidence(t *testing.T) {
	ctx := context.Background()
	store, err := memory.Open(filepath.Join(t.TempDir(), "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	job, err := store.CreateResearchJob(ctx, memory.ResearchJob{Query: "quien gano la copa america 2024", MaxSources: 1})
	if err != nil {
		t.Fatal(err)
	}
	page := FetchedPage{
		URL:         "https://copaamerica.example/final-2024",
		FinalURL:    "https://copaamerica.example/final-2024",
		StatusCode:  http.StatusOK,
		ContentType: "text/html",
		FetchedAt:   time.Now().UTC(),
		Body: []byte(`<html><head><title>Final Copa America 2024</title></head><body>
			<p>La Copa America 2024 la gano Argentina al vencer a Colombia en la final disputada en Miami.</p>
		</body></html>`),
	}
	worker := NewWorker(store, Options{Enabled: true, MaxSources: 1, MinTrustScore: 0.1, JobTimeout: time.Second})
	worker.Searcher = FakeSearchProvider{Results: []SearchResult{{Title: "Final Copa America 2024", URL: page.URL, Snippet: "La Copa America 2024 la gano Argentina al vencer a Colombia en la final.", Rank: 1}}}
	worker.Fetcher = FakeFetcher{Pages: map[string]FetchedPage{page.URL: page}}
	result, err := worker.RunJob(ctx, job)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(result.Answer, "Argentina") || !strings.Contains(result.Answer, "Colombia") {
		t.Fatalf("answer = %q", result.Answer)
	}
}

func TestCanonicalAnswerPicksMostSupportedSentence(t *testing.T) {
	answer, ok := CanonicalAnswer("quien gano la copa america 2024", []string{
		"El torneo se jugo durante el verano en varias sedes de Estados Unidos.",
		"La Copa America 2024 la gano Argentina tras vencer a Colombia en la final.",
	})
	if !ok || !strings.Contains(answer, "Argentina") || !strings.Contains(answer, "Colombia") {
		t.Fatalf("answer=%q ok=%v", answer, ok)
	}
}

func TestCanonicalAnswerCleansTruncatedProse(t *testing.T) {
	answer, ok := CanonicalAnswer("que es la fotosintesis", []string{
		"La fotosintesis es un proceso quimico que convierte materia inorganica en materia organica gracias a la energia que ...",
	})
	if !ok {
		t.Fatal("expected an answer, got abstain")
	}
	if strings.Contains(answer, "...") || strings.Contains(answer, "…") {
		t.Fatalf("answer still carries a truncated tail: %q", answer)
	}
	if strings.HasSuffix(strings.TrimSuffix(answer, "."), " que") {
		t.Fatalf("answer ends with a dangling connector: %q", answer)
	}
	if !strings.Contains(answer, "proceso quimico") {
		t.Fatalf("answer lost its substance: %q", answer)
	}
}

func TestCanonicalAnswerAbstainsWithoutSupport(t *testing.T) {
	if answer, ok := CanonicalAnswer("quien gano la copa america 2024", []string{
		"Las entradas para el proximo concierto ya estan a la venta.",
		"El clima estuvo soleado durante toda la semana en la ciudad.",
	}); ok {
		t.Fatalf("expected abstention when evidence does not support the query, got %q", answer)
	}
}
