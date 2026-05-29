package research

import (
	"net/url"
	"strings"
	"time"
)

type SourceRanker struct {
	MinTrustScore float64
}

func (r SourceRanker) Rank(query string, result SearchResult, page FetchedPage, doc ExtractedDocument, duplicate bool) (float64, float64) {
	queryTokens := keywordSet(query)
	textTokens := keywordSet(result.Title + " " + result.Snippet + " " + doc.Text)
	keyword := overlapScore(queryTokens, textTokens)
	title := overlapScore(queryTokens, keywordSet(result.Title)) * 0.25
	trust := domainTrust(page.FinalURL)
	freshness := 0.05
	if !page.FetchedAt.IsZero() && time.Since(page.FetchedAt) < 30*24*time.Hour {
		freshness = 0.10
	}
	penalty := 0.0
	if duplicate {
		penalty += 0.25
	}
	score := keyword*0.45 + title + trust*0.25 + freshness - penalty
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score, trust
}

func domainTrust(rawURL string) float64 {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return 0.30
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case strings.HasSuffix(host, ".gov"), strings.HasSuffix(host, ".edu"):
		return 0.90
	case strings.Contains(host, "docs."), strings.Contains(host, "developer."), strings.Contains(host, "pkg.go.dev"):
		return 0.85
	case strings.Contains(host, "github.com"), strings.Contains(host, "wikipedia.org"):
		return 0.75
	case strings.Contains(host, "blog"), strings.Contains(host, "medium.com"):
		return 0.45
	default:
		return 0.55
	}
}
