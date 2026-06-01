package research

import (
	"context"
	"time"
)

const (
	StatusLocalVerified          = "local_verified"
	StatusWebVerified            = "web_verified"
	StatusWebSupported           = "web_supported"
	StatusSingleSourceUnverified = "single_source_unverified"
	StatusConflictingSources     = "conflicting_sources"
	StatusStale                  = "stale"
	StatusInsufficientEvidence   = "insufficient_evidence"
)

type Options struct {
	Enabled               bool
	AutoOnKnowledgeGap    bool
	BackgroundJobsEnabled bool
	Provider              string
	SearXNGURL            string
	MaxSources            int
	MaxFetchBytes         int64
	FetchTimeout          time.Duration
	JobTimeout            time.Duration
	MinSourcesForVerified int
	MinTrustScore         float64
	UserAgent             string
	BlockedDomains        []string
	AllowedDomains        []string
}

type SearchProvider interface {
	Search(ctx context.Context, query string, limit int) ([]SearchResult, error)
}

type Fetcher interface {
	Fetch(ctx context.Context, url string) (FetchedPage, error)
}

type Extractor interface {
	Extract(ctx context.Context, page FetchedPage) (ExtractedDocument, error)
}

type Store interface {
	StoreResearchResult(ctx context.Context, result ResearchResult) error
}

type SearchResult struct {
	Title   string
	URL     string
	Snippet string
	Engine  string
	Rank    int
	Score   float64
}

type FetchedPage struct {
	URL         string
	FinalURL    string
	StatusCode  int
	ContentType string
	FetchedAt   time.Time
	ContentHash string
	ByteSize    int64
	Body        []byte
}

type ExtractedDocument struct {
	Title string
	Text  string
}

type WebClaim struct {
	ID         string
	SourceID   string
	Text       string
	Confidence float64
	CreatedAt  time.Time
}

type RankedSource struct {
	SearchResult
	Fetched   FetchedPage
	Extracted ExtractedDocument
	Claims    []WebClaim
	RankScore float64
	Trust     float64
	Status    string
	Error     string
}

type ResearchResult struct {
	JobID          string
	Query          string
	EvidenceStatus string
	Answer         string
	Confidence     float64
	Sources        []RankedSource
	SourcesStored  int
	ChunksStored   int
	ClaimsStored   int
}
