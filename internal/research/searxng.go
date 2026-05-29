package research

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type SearXNGProvider struct {
	BaseURL string
	Client  *http.Client
}

func (p SearXNGProvider) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 5
	}
	base := strings.TrimRight(p.BaseURL, "/")
	if base == "" {
		return nil, fmt.Errorf("searxng url is required")
	}
	u, err := url.Parse(base + "/search")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("format", "json")
	u.RawQuery = q.Encode()
	reqCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("searxng status %d", resp.StatusCode)
	}
	var raw struct {
		Results []struct {
			Title   string  `json:"title"`
			URL     string  `json:"url"`
			Content string  `json:"content"`
			Engine  string  `json:"engine"`
			Score   float64 `json:"score"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode searxng json: %w", err)
	}
	seen := map[string]bool{}
	out := make([]SearchResult, 0, limit)
	for _, item := range raw.Results {
		canonical := strings.TrimSpace(item.URL)
		if canonical == "" || seen[canonical] {
			continue
		}
		seen[canonical] = true
		out = append(out, SearchResult{
			Title:   strings.TrimSpace(item.Title),
			URL:     canonical,
			Snippet: strings.TrimSpace(item.Content),
			Engine:  strings.TrimSpace(item.Engine),
			Rank:    len(out) + 1,
			Score:   item.Score,
		})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}
