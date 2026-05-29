package research

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"aletheia/internal/memory"
	"aletheia/internal/retriever"
)

type Worker struct {
	Store     *memory.Store
	Searcher  SearchProvider
	Fetcher   Fetcher
	Extractor Extractor
	Claims    ClaimExtractor
	Ranker    SourceRanker
	Options   Options
}

func NewWorker(store *memory.Store, opts Options) Worker {
	if opts.MaxSources <= 0 {
		opts.MaxSources = 5
	}
	if opts.JobTimeout <= 0 {
		opts.JobTimeout = 120 * time.Second
	}
	if opts.FetchTimeout <= 0 {
		opts.FetchTimeout = 10 * time.Second
	}
	if opts.MaxFetchBytes <= 0 {
		opts.MaxFetchBytes = 1 << 20
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "AletheiaResearchBot/0.1"
	}
	if len(opts.BlockedDomains) == 0 {
		opts.BlockedDomains = []string{"facebook.com", "instagram.com", "tiktok.com", "x.com", "twitter.com", "reddit.com"}
	}
	return Worker{
		Store: store,
		Searcher: SearXNGProvider{
			BaseURL: opts.SearXNGURL,
		},
		Fetcher: HTTPFetcher{
			Timeout:        opts.FetchTimeout,
			MaxBytes:       opts.MaxFetchBytes,
			UserAgent:      opts.UserAgent,
			BlockedDomains: opts.BlockedDomains,
			AllowedDomains: opts.AllowedDomains,
		},
		Extractor: SimpleHTMLExtractor{},
		Claims:    ClaimExtractor{},
		Ranker:    SourceRanker{MinTrustScore: opts.MinTrustScore},
		Options:   opts,
	}
}

func (w Worker) ProcessNext(ctx context.Context) (bool, error) {
	if w.Store == nil {
		return false, fmt.Errorf("research store is required")
	}
	job, ok, err := w.Store.ClaimQueuedResearchJob(ctx)
	if err != nil || !ok {
		return ok, err
	}
	_, err = w.RunJob(ctx, job)
	return true, err
}

func (w Worker) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = w.ProcessNext(ctx)
			}
		}
	}()
}

func (w Worker) RunJob(ctx context.Context, job memory.ResearchJob) (ResearchResult, error) {
	if w.Store == nil || w.Searcher == nil || w.Fetcher == nil || w.Extractor == nil {
		return ResearchResult{}, fmt.Errorf("research worker is not configured")
	}
	timeout := w.Options.JobTimeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	jobCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	results, err := w.Searcher.Search(jobCtx, job.Query, maxNonZero(job.MaxSources, w.Options.MaxSources))
	if err != nil {
		job.Status = "failed"
		job.Error = err.Error()
		job.CompletedAt = time.Now().UTC()
		_ = w.Store.UpdateResearchJob(ctx, job)
		return ResearchResult{}, err
	}

	report := ResearchResult{JobID: job.ID, Query: job.Query}
	seenHashes := map[string]bool{}
	for _, result := range results {
		source := RankedSource{SearchResult: result, Status: "candidate"}
		page, err := w.Fetcher.Fetch(jobCtx, result.URL)
		if err != nil {
			source.Status = "fetch_failed"
			source.Error = err.Error()
			report.Sources = append(report.Sources, source)
			continue
		}
		doc, err := w.Extractor.Extract(jobCtx, page)
		if err != nil {
			source.Status = "extract_failed"
			source.Error = err.Error()
			report.Sources = append(report.Sources, source)
			continue
		}
		duplicate := seenHashes[page.ContentHash]
		seenHashes[page.ContentHash] = true
		score, trust := w.Ranker.Rank(job.Query, result, page, doc, duplicate)
		source.Fetched = page
		source.Extracted = doc
		source.RankScore = score
		source.Trust = trust
		source.Status = "fetched"
		sourceID := sourceID(job.ID, result.Rank, result.URL)
		source.Claims = w.Claims.Extract(job.Query, sourceID, doc, 6)
		if score >= w.Options.MinTrustScore && !duplicate {
			if err := w.storeSource(job, sourceID, source); err != nil {
				source.Status = "store_failed"
				source.Error = err.Error()
			} else {
				report.SourcesStored++
				report.ClaimsStored += len(source.Claims)
				report.ChunksStored += len(ChunkText(doc.Text, 1600, 200))
				source.Status = "stored"
			}
		} else {
			source.Status = "low_rank"
		}
		report.Sources = append(report.Sources, source)
	}

	report.EvidenceStatus = evidenceStatus(report.SourcesStored, w.Options.MinSourcesForVerified)
	if unsupportedFutureOutcomeQuery(job.Query) {
		report.EvidenceStatus = StatusInsufficientEvidence
		report.Confidence = 0
		report.Answer = fmt.Sprintf("No hay evidencia web suficiente para responder sobre %q. La pregunta pide un resultado futuro o no confirmado.", job.Query)
	} else {
		report.Confidence = confidence(report.Sources)
		report.Answer = answerFromSources(job.Query, report)
	}
	job.Status = "completed"
	job.CompletedAt = time.Now().UTC()
	job.Answer = report.Answer
	job.Confidence = report.Confidence
	if err := w.Store.UpdateResearchJob(ctx, job); err != nil {
		return report, err
	}
	return report, nil
}

func (w Worker) storeSource(job memory.ResearchJob, id string, source RankedSource) error {
	ctx := context.Background()
	title := source.Extracted.Title
	if title == "" {
		title = source.Title
	}
	stored, err := w.Store.UpsertWebSource(ctx, memory.WebSource{
		ID:          id,
		JobID:       job.ID,
		URL:         source.URL,
		FinalURL:    source.Fetched.FinalURL,
		Title:       title,
		Snippet:     source.Snippet,
		SourceRank:  source.RankScore,
		FetchedAt:   source.Fetched.FetchedAt,
		Status:      "stored",
		ContentHash: source.Fetched.ContentHash,
		TrustScore:  source.Trust,
		ByteSize:    source.Fetched.ByteSize,
		ContentType: source.Fetched.ContentType,
	})
	if err != nil {
		return err
	}
	text := fmt.Sprintf("%s\n\n%s\n\nSource URL: %s\nFetched: %s", title, source.Extracted.Text, source.Fetched.FinalURL, source.Fetched.FetchedAt.Format(time.RFC3339))
	hash := sha256.Sum256([]byte(text))
	docPath := filepath.Join("web", id+".txt")
	doc, _, err := w.Store.UpsertDocument(ctx, docPath, hex.EncodeToString(hash[:]), text)
	if err != nil {
		return err
	}
	chunkTexts := ChunkText(text, 1600, 200)
	chunks := make([]memory.Chunk, 0, len(chunkTexts))
	offset := 0
	for _, chunkText := range chunkTexts {
		chunks = append(chunks, memory.Chunk{
			OffsetStart: offset,
			OffsetEnd:   offset + len([]rune(chunkText)),
			Text:        chunkText,
			EmbeddingID: retriever.EmbeddingID,
		})
		offset += len([]rune(chunkText))
	}
	storedChunks, err := w.Store.ReplaceChunks(ctx, doc.ID, chunks)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(stored)
	sourceNode, err := w.Store.EnsureNode(ctx, "web_source", stored.ID, string(payload))
	if err != nil {
		return err
	}
	pagePayload, _ := json.Marshal(map[string]any{"document_id": doc.ID, "url": stored.FinalURL, "title": title})
	pageNode, err := w.Store.EnsureNode(ctx, "web_page", stored.FinalURL, string(pagePayload))
	if err != nil {
		return err
	}
	if _, err := w.Store.EnsureEdge(ctx, pageNode, sourceNode, "derived_from", 1); err != nil {
		return err
	}
	for _, chunk := range storedChunks {
		chunkNode, err := w.Store.EnsureNode(ctx, "chunk", fmt.Sprintf("chunk:%d", chunk.ID), fmt.Sprintf(`{"chunk_id":%d,"document_id":%d}`, chunk.ID, doc.ID))
		if err != nil {
			return err
		}
		if _, err := w.Store.EnsureEdge(ctx, chunkNode, sourceNode, "derived_from", 1); err != nil {
			return err
		}
	}
	for _, claim := range source.Claims {
		storedClaim, err := w.Store.RecordWebClaim(ctx, memory.WebClaim{
			ID:         claim.ID,
			SourceID:   stored.ID,
			Claim:      claim.Text,
			Confidence: claim.Confidence,
			CreatedAt:  claim.CreatedAt,
		})
		if err != nil {
			return err
		}
		claimPayload, _ := json.Marshal(storedClaim)
		claimNode, err := w.Store.EnsureNode(ctx, "web_claim", storedClaim.ID, string(claimPayload))
		if err != nil {
			return err
		}
		if _, err := w.Store.EnsureEdge(ctx, claimNode, sourceNode, "supported_by", storedClaim.Confidence); err != nil {
			return err
		}
	}
	return nil
}

func evidenceStatus(sourcesStored int, minVerified int) string {
	if sourcesStored <= 0 {
		return StatusInsufficientEvidence
	}
	if sourcesStored == 1 {
		return StatusSingleSourceUnverified
	}
	if sourcesStored >= minVerified {
		return StatusWebVerified
	}
	return StatusWebSupported
}

func confidence(sources []RankedSource) float64 {
	var total float64
	var count int
	for _, source := range sources {
		if source.Status == "stored" {
			total += source.RankScore
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func answerFromSources(query string, report ResearchResult) string {
	best := ""
	bestScore := -1
	queryTokens := keywordSet(query)
	for _, source := range report.Sources {
		if source.Status != "stored" || len(source.Claims) == 0 {
			continue
		}
		sourceTitle := strings.TrimSpace(source.Extracted.Title)
		if sourceTitle == "" {
			sourceTitle = strings.TrimSpace(source.Title)
		}
		if candidate := canonicalClaimAnswer(query, source.Snippet); candidate != "" && !likelyTitle(candidate) {
			score := int(overlapScore(queryTokens, keywordSet(candidate)) * 100)
			if score > bestScore {
				best = candidate
				bestScore = score
			}
		}
		for _, claim := range source.Claims {
			text := strings.TrimSpace(claim.Text)
			if text == "" || strings.EqualFold(text, sourceTitle) || likelyTitle(text) {
				continue
			}
			text = canonicalClaimAnswer(query, text)
			if text == "" {
				continue
			}
			score := int(overlapScore(queryTokens, keywordSet(text)) * 100)
			if score > bestScore {
				best = text
				bestScore = score
			}
		}
	}
	if best != "" {
		for _, source := range report.Sources {
			if source.Status == "stored" {
				return fmt.Sprintf("%s\n\nEvidence status: %s\nSource: %s", best, report.EvidenceStatus, source.Fetched.FinalURL)
			}
		}
	}
	return fmt.Sprintf("No hay evidencia web suficiente para responder sobre %q.", query)
}

func canonicalClaimAnswer(query string, text string) string {
	text = cleanExtractedSentence(text)
	queryTokens := keywordSet(query)
	best := ""
	bestScore := 0.0
	for _, sentence := range splitSentences(text) {
		sentence = strings.TrimSpace(sentence)
		if len([]rune(sentence)) < 35 || likelyTitle(sentence) {
			continue
		}
		score := overlapScore(queryTokens, keywordSet(sentence))
		if score > bestScore {
			best = sentence
			bestScore = score
		}
	}
	if best == "" {
		best = text
	}
	best = trimBeforeCoreTerm(query, best)
	if len([]rune(best)) > 420 {
		runes := []rune(best)
		best = string(runes[:420])
		if idx := strings.LastIndex(best, " "); idx > 180 {
			best = best[:idx]
		}
		best += "."
	}
	return strings.TrimSpace(best)
}

func cleanExtractedSentence(text string) string {
	replacements := []string{
		"WhatsApp", "Twitter", "Facebook", "Linkedin", "LinkedIn", "Telegram",
		"Copiar URL", "Beloud", "Lo Ultimo", "Lo Último",
	}
	for _, value := range replacements {
		text = strings.ReplaceAll(text, value, " ")
	}
	return strings.Join(strings.Fields(text), " ")
}

func splitSentences(text string) []string {
	parts := regexp.MustCompile(`[.!?]\s+`).Split(text, -1)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func trimBeforeCoreTerm(query string, text string) string {
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "whatsapp") &&
		!strings.Contains(lower, "copiar") &&
		!strings.Contains(lower, "fotografia") &&
		!strings.Contains(lower, "redaccion") &&
		!strings.Contains(lower, "publicado") &&
		!strings.Contains(lower, "lo ultimo") {
		return text
	}
	for token := range keywordSet(query) {
		if len(token) < 4 {
			continue
		}
		if idx := strings.Index(lower, token); idx > 24 && idx < len(text) {
			best := idx
			for other := range keywordSet(query) {
				if len(other) < 4 {
					continue
				}
				if otherIdx := strings.Index(lower, other); otherIdx > 24 && otherIdx < best {
					best = otherIdx
				}
			}
			return strings.TrimSpace(text[best:])
		}
	}
	return text
}

func likelyTitle(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || len([]rune(text)) > 220 {
		return false
	}
	if questionLikeTitle(text) {
		return true
	}
	return !strings.HasSuffix(text, ".") || strings.Contains(text, " - ") || strings.Contains(text, " | ")
}

func questionLikeTitle(text string) bool {
	normalized := NormalizeIntentText(text)
	normalized = strings.Trim(normalized, "¿? ")
	return strings.HasPrefix(normalized, "quien ") ||
		strings.HasPrefix(normalized, "que ") ||
		strings.HasPrefix(normalized, "como ") ||
		strings.HasPrefix(normalized, "who ") ||
		strings.HasPrefix(normalized, "what ") ||
		strings.HasPrefix(normalized, "how ")
}

var researchYearRe = regexp.MustCompile(`\b(19|20|21)\d{2}\b`)

func unsupportedFutureOutcomeQuery(query string) bool {
	normalized := strings.ToLower(query)
	if !(strings.Contains(normalized, "gano") ||
		strings.Contains(normalized, "ganador") ||
		strings.Contains(normalized, "campeon") ||
		strings.Contains(normalized, "resultado") ||
		strings.Contains(normalized, "winner") ||
		strings.Contains(normalized, "won") ||
		strings.Contains(normalized, "champion") ||
		strings.Contains(normalized, "result")) {
		return false
	}
	for _, year := range researchYearRe.FindAllString(normalized, -1) {
		value, err := strconv.Atoi(year)
		if err == nil && value > time.Now().Year() {
			return true
		}
	}
	return false
}

func sourceID(jobID string, rank int, rawURL string) string {
	hash := sha256.Sum256([]byte(jobID + rawURL))
	return fmt.Sprintf("source_%s_%d", hex.EncodeToString(hash[:8]), rank)
}

func maxNonZero(a, b int) int {
	if a > 0 {
		return a
	}
	return b
}

type FakeSearchProvider struct {
	Results []SearchResult
	Err     error
}

func (f FakeSearchProvider) Search(_ context.Context, _ string, limit int) ([]SearchResult, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	if limit > 0 && len(f.Results) > limit {
		return f.Results[:limit], nil
	}
	return f.Results, nil
}

type FakeFetcher struct {
	Pages map[string]FetchedPage
	Err   error
}

func (f FakeFetcher) Fetch(_ context.Context, rawURL string) (FetchedPage, error) {
	if f.Err != nil {
		return FetchedPage{}, f.Err
	}
	page, ok := f.Pages[rawURL]
	if !ok {
		return FetchedPage{}, fmt.Errorf("missing fake page %s", rawURL)
	}
	if page.URL == "" {
		page.URL = rawURL
	}
	if page.FinalURL == "" {
		page.FinalURL = rawURL
	}
	if page.FetchedAt.IsZero() {
		page.FetchedAt = time.Now().UTC()
	}
	if page.ContentHash == "" {
		hash := sha256.Sum256(page.Body)
		page.ContentHash = hex.EncodeToString(hash[:])
	}
	if page.ByteSize == 0 {
		page.ByteSize = int64(len(page.Body))
	}
	return page, nil
}

func NormalizeIntentText(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(text)), " ")
}
