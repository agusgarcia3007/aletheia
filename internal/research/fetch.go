package research

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type HTTPFetcher struct {
	Timeout        time.Duration
	MaxBytes       int64
	UserAgent      string
	BlockedDomains []string
	AllowedDomains []string
}

func (f HTTPFetcher) Fetch(ctx context.Context, rawURL string) (FetchedPage, error) {
	if f.Timeout <= 0 {
		f.Timeout = 10 * time.Second
	}
	if f.MaxBytes <= 0 {
		f.MaxBytes = 1 << 20
	}
	if f.UserAgent == "" {
		f.UserAgent = "AletheiaResearchBot/0.1"
	}
	if err := f.allowedURL(rawURL); err != nil {
		return FetchedPage{}, err
	}
	client := &http.Client{
		Timeout: f.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return f.allowedURL(req.URL.String())
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return FetchedPage{}, err
	}
	req.Header.Set("User-Agent", f.UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return FetchedPage{}, err
	}
	defer resp.Body.Close()
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if !supportedContentType(contentType) {
		return FetchedPage{}, fmt.Errorf("unsupported content-type %q", contentType)
	}
	var buf bytes.Buffer
	limited := io.LimitReader(resp.Body, f.MaxBytes+1)
	n, err := io.Copy(&buf, limited)
	if err != nil {
		return FetchedPage{}, err
	}
	if n > f.MaxBytes {
		return FetchedPage{}, fmt.Errorf("response exceeds max bytes")
	}
	body := buf.Bytes()
	hash := sha256.Sum256(body)
	return FetchedPage{
		URL:         rawURL,
		FinalURL:    resp.Request.URL.String(),
		StatusCode:  resp.StatusCode,
		ContentType: contentType,
		FetchedAt:   time.Now().UTC(),
		ContentHash: hex.EncodeToString(hash[:]),
		ByteSize:    int64(len(body)),
		Body:        body,
	}, nil
}

func (f HTTPFetcher) allowedURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported url scheme %q", parsed.Scheme)
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return fmt.Errorf("url host is required")
	}
	for _, domain := range f.BlockedDomains {
		if domainMatches(host, domain) {
			return fmt.Errorf("blocked domain %s", domain)
		}
	}
	if len(f.AllowedDomains) > 0 {
		for _, domain := range f.AllowedDomains {
			if domainMatches(host, domain) {
				return nil
			}
		}
		return fmt.Errorf("domain %s is not allowed", host)
	}
	return nil
}

func supportedContentType(contentType string) bool {
	if contentType == "" {
		return true
	}
	return strings.Contains(contentType, "text/html") ||
		strings.Contains(contentType, "text/plain") ||
		strings.Contains(contentType, "application/json")
}

func domainMatches(host string, domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	return host == domain || strings.HasSuffix(host, "."+domain)
}
