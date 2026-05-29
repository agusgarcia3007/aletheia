package retriever

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"aletheia/internal/memory"
	"aletheia/internal/verifier"
)

const (
	DefaultChunkSize     = 1200
	DefaultChunkOverlap  = 200
	DefaultMaxFileBytes  = 512 * 1024
	DefaultVectorDims    = 64
	DefaultMinConfidence = 0.20
	EmbeddingID          = "hashing-v1:64"
)

type IndexOptions struct {
	ChunkSize    int
	ChunkOverlap int
	MaxFileBytes int64
	GraphEnabled *bool
}

type IndexReport struct {
	Root               string
	Scanned            int
	Indexed            int
	SkippedUnchanged   int
	SkippedUnsupported int
	SkippedTooLarge    int
	ChunksWritten      int
}

type Indexer struct {
	Store *memory.Store
}

type SearchOptions struct {
	TopK          int
	MinConfidence float64
}

type Hit struct {
	ChunkID       int64
	DocumentID    int64
	Path          string
	OffsetStart   int
	OffsetEnd     int
	Text          string
	Preview       string
	Score         float64
	KeywordScore  float64
	SemanticScore float64
	GraphScore    float64
	RecencyScore  float64
	TrustScore    float64
	UpdatedAt     time.Time
}

type Citation struct {
	Path        string
	ChunkID     int64
	OffsetStart int
	OffsetEnd   int
	Score       float64
}

type Answer struct {
	Text       string
	Status     string
	Verified   bool
	Confidence float64
	Citations  []Citation
	Hits       []Hit
}

type Retriever struct {
	Store *memory.Store
}

func (i Indexer) IndexPath(ctx context.Context, root string, opts IndexOptions) (IndexReport, error) {
	if i.Store == nil {
		return IndexReport{}, fmt.Errorf("memory store is required")
	}
	opts = normalizeIndexOptions(opts)
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return IndexReport{}, err
	}
	report := IndexReport{Root: absRoot}
	err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(d.Name()) && path != absRoot {
				return filepath.SkipDir
			}
			return nil
		}
		report.Scanned++
		if !isSupported(path) {
			report.SkippedUnsupported++
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > opts.MaxFileBytes {
			report.SkippedTooLarge++
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !utf8.Valid(raw) || looksBinary(raw) {
			report.SkippedUnsupported++
			return nil
		}
		text := string(raw)
		hash := sha256Hex(raw)
		doc, changed, err := i.Store.UpsertDocument(ctx, path, hash, text)
		if err != nil {
			return err
		}
		if !changed {
			report.SkippedUnchanged++
			return nil
		}
		chunks := chunkText(text, opts.ChunkSize, opts.ChunkOverlap)
		for idx := range chunks {
			chunks[idx].EmbeddingID = EmbeddingID
		}
		stored, err := i.Store.ReplaceChunks(ctx, doc.ID, chunks)
		if err != nil {
			return err
		}
		if opts.graphEnabled() {
			if err := i.writeGraph(ctx, doc, stored); err != nil {
				return err
			}
		}
		report.Indexed++
		report.ChunksWritten += len(stored)
		return nil
	})
	return report, err
}

func (i Indexer) writeGraph(ctx context.Context, doc memory.Document, chunks []memory.Chunk) error {
	docNode, err := i.Store.EnsureNode(ctx, "document", doc.Path, fmt.Sprintf(`{"document_id":%d}`, doc.ID))
	if err != nil {
		return err
	}
	var prev int64
	for _, chunk := range chunks {
		label := fmt.Sprintf("chunk:%d", chunk.ID)
		chunkNode, err := i.Store.EnsureNode(ctx, "chunk", label, fmt.Sprintf(`{"chunk_id":%d,"document_id":%d}`, chunk.ID, doc.ID))
		if err != nil {
			return err
		}
		if _, err := i.Store.EnsureEdge(ctx, docNode, chunkNode, "contains", 1); err != nil {
			return err
		}
		if _, err := i.Store.EnsureEdge(ctx, chunkNode, docNode, "derived_from", 1); err != nil {
			return err
		}
		if prev != 0 {
			if _, err := i.Store.EnsureEdge(ctx, prev, chunkNode, "next_chunk", 1); err != nil {
				return err
			}
		}
		prev = chunkNode
	}
	return nil
}

func (r Retriever) Search(ctx context.Context, query string, opts SearchOptions) ([]Hit, error) {
	if r.Store == nil {
		return nil, fmt.Errorf("memory store is required")
	}
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query is required")
	}
	if opts.TopK <= 0 {
		opts.TopK = 5
	}
	chunks, err := r.Store.Chunks(ctx)
	if err != nil {
		return nil, err
	}
	queryTokens := tokenSet(query)
	queryVec := hashVector(query)
	now := time.Now()
	hits := make([]Hit, 0, len(chunks))
	for _, chunk := range chunks {
		keyword := keywordScore(queryTokens, tokenSet(chunk.Text))
		semantic := cosine(queryVec, hashVector(chunk.Text))
		graph := 0.05
		recency := recencyScore(now, chunk.UpdatedAt)
		trust := trustScore(chunk.Path)
		score := keyword*2.0 + semantic*1.2 + graph + recency + trust
		hits = append(hits, Hit{
			ChunkID:       chunk.ID,
			DocumentID:    chunk.DocumentID,
			Path:          chunk.Path,
			OffsetStart:   chunk.OffsetStart,
			OffsetEnd:     chunk.OffsetEnd,
			Text:          chunk.Text,
			Preview:       preview(chunk.Text, 220),
			Score:         score,
			KeywordScore:  keyword,
			SemanticScore: semantic,
			GraphScore:    graph,
			RecencyScore:  recency,
			TrustScore:    trust,
			UpdatedAt:     chunk.UpdatedAt,
		})
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].Score == hits[b].Score {
			return hits[a].ChunkID < hits[b].ChunkID
		}
		return hits[a].Score > hits[b].Score
	})
	if len(hits) > opts.TopK {
		hits = hits[:opts.TopK]
	}
	return hits, nil
}

func (r Retriever) Answer(ctx context.Context, query string, opts SearchOptions) (Answer, error) {
	if opts.MinConfidence == 0 {
		opts.MinConfidence = DefaultMinConfidence
	}
	hits, err := r.Search(ctx, query, opts)
	if err != nil {
		return Answer{}, err
	}
	if len(hits) == 0 || hits[0].Score < opts.MinConfidence || !hasMeaningfulEvidenceOverlap(query, hits[0].Text) {
		return Answer{
			Text:   "No hay evidencia local suficiente para responder.",
			Status: "abstained",
			Hits:   hits,
		}, nil
	}
	sentence := bestSentenceFromHits(query, hits)
	if sentence == "" {
		sentence = bestSentence(query, hits[0].Text)
	}
	if sentence == "" {
		sentence = hits[0].Preview
	}
	citations := make([]Citation, 0, len(hits))
	textCitations := make([]verifier.TextEvidenceCitation, 0, len(hits))
	for _, hit := range hits {
		path := citationPath(ctx, r.Store, hit)
		citations = append(citations, Citation{
			Path:        path,
			ChunkID:     hit.ChunkID,
			OffsetStart: hit.OffsetStart,
			OffsetEnd:   hit.OffsetEnd,
			Score:       hit.Score,
		})
		textCitations = append(textCitations, verifier.TextEvidenceCitation{
			ChunkID: hit.ChunkID,
			Text:    hit.Text,
		})
	}
	textEvidence := verifier.VerifyTextEvidence(sentence, textCitations)
	if textEvidence.Status != verifier.StatusPass {
		return Answer{
			Text:       "No hay evidencia local suficiente para responder.",
			Status:     "abstained",
			Confidence: hits[0].Score,
			Hits:       hits,
		}, nil
	}
	return Answer{
		Text:       sentence,
		Status:     "answered",
		Verified:   true,
		Confidence: hits[0].Score,
		Citations:  citations,
		Hits:       hits,
	}, nil
}

func normalizeIndexOptions(opts IndexOptions) IndexOptions {
	if opts.ChunkSize <= 0 {
		opts.ChunkSize = DefaultChunkSize
	}
	if opts.ChunkOverlap < 0 {
		opts.ChunkOverlap = 0
	}
	if opts.ChunkOverlap >= opts.ChunkSize {
		opts.ChunkOverlap = opts.ChunkSize / 4
	}
	if opts.MaxFileBytes <= 0 {
		opts.MaxFileBytes = DefaultMaxFileBytes
	}
	return opts
}

func (opts IndexOptions) graphEnabled() bool {
	return opts.GraphEnabled == nil || *opts.GraphEnabled
}

func chunkText(text string, size int, overlap int) []memory.Chunk {
	runes := []rune(text)
	if len(runes) == 0 {
		return []memory.Chunk{{OffsetStart: 0, OffsetEnd: 0, Text: ""}}
	}
	var out []memory.Chunk
	step := size - overlap
	if step <= 0 {
		step = size
	}
	for start := 0; start < len(runes); {
		end := start + size
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, memory.Chunk{
			OffsetStart: start,
			OffsetEnd:   end,
			Text:        string(runes[start:end]),
			EmbeddingID: EmbeddingID,
		})
		if end == len(runes) {
			break
		}
		start += step
	}
	return out
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", "checkpoints", "vendor":
		return true
	default:
		return false
	}
}

func isSupported(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".txt", ".go", ".json", ".yaml", ".yml":
		base := filepath.Base(path)
		if strings.HasSuffix(base, ".sqlite") {
			return false
		}
		return true
	default:
		return false
	}
}

func looksBinary(raw []byte) bool {
	for _, b := range raw {
		if b == 0 {
			return true
		}
	}
	return false
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

var tokenPattern = regexp.MustCompile(`[\pL\pN_]+`)

func tokenSet(text string) map[string]int {
	matches := tokenPattern.FindAllString(normalizeTokens(text), -1)
	out := make(map[string]int, len(matches))
	for _, match := range matches {
		out[match]++
	}
	return out
}

func keywordScore(query map[string]int, doc map[string]int) float64 {
	if len(query) == 0 || len(doc) == 0 {
		return 0
	}
	var overlap int
	for token := range query {
		if doc[token] > 0 {
			overlap++
		}
	}
	return float64(overlap) / math.Sqrt(float64(len(query)*len(doc)))
}

func hasMeaningfulEvidenceOverlap(query string, text string) bool {
	queryTokens := meaningfulTokenSet(query)
	if len(queryTokens) == 0 {
		return false
	}
	textTokens := meaningfulTokenSet(text)
	var overlap int
	for token := range queryTokens {
		if textTokens[token] > 0 {
			overlap++
		}
	}
	if len(queryTokens) <= 2 {
		return overlap >= 1
	}
	return overlap >= 2
}

func meaningfulTokenSet(text string) map[string]int {
	tokens := tokenSet(text)
	for token := range tokens {
		if len([]rune(token)) <= 2 || stopWords[token] {
			delete(tokens, token)
		}
	}
	return tokens
}

var stopWords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "by": true, "for": true, "from": true,
	"how": true, "is": true, "it": true, "of": true, "on": true, "or": true, "the": true, "to": true,
	"what": true, "when": true, "where": true, "who": true, "why": true, "with": true,
	"como": true, "con": true, "cual": true, "cuando": true, "de": true, "del": true, "donde": true,
	"el": true, "en": true, "es": true, "esa": true, "ese": true, "eso": true, "esta": true, "este": true,
	"fue": true, "la": true, "las": true, "lo": true, "los": true, "para": true, "por": true, "que": true,
	"quien": true, "son": true, "un": true, "una": true, "y": true,
}

func normalizeTokens(text string) string {
	replacer := strings.NewReplacer(
		"á", "a",
		"é", "e",
		"í", "i",
		"ó", "o",
		"ú", "u",
		"ü", "u",
		"ñ", "n",
	)
	return replacer.Replace(strings.ToLower(text))
}

func hashVector(text string) []float64 {
	vec := make([]float64, DefaultVectorDims)
	normalized := strings.ToLower(text)
	runes := []rune(normalized)
	if len(runes) < 3 {
		for _, r := range runes {
			addHash(vec, string(r))
		}
		return vec
	}
	for i := 0; i <= len(runes)-3; i++ {
		gram := string(runes[i : i+3])
		addHash(vec, gram)
	}
	return vec
}

func addHash(vec []float64, value string) {
	h := fnv.New64a()
	_, _ = h.Write([]byte(value))
	sum := h.Sum64()
	idx := int(sum % uint64(len(vec)))
	sign := 1.0
	if sum&1 == 1 {
		sign = -1
	}
	vec[idx] += sign
}

func cosine(a, b []float64) float64 {
	var dot, an, bn float64
	for i := range a {
		dot += a[i] * b[i]
		an += a[i] * a[i]
		bn += b[i] * b[i]
	}
	if an == 0 || bn == 0 {
		return 0
	}
	return dot / (math.Sqrt(an) * math.Sqrt(bn))
}

func recencyScore(now time.Time, updatedAt time.Time) float64 {
	if updatedAt.IsZero() {
		return 0
	}
	days := now.Sub(updatedAt).Hours() / 24
	if days < 0 {
		days = 0
	}
	return 0.05 / (1 + days/30)
}

func trustScore(path string) float64 {
	if strings.Contains(path, string(filepath.Separator)+"docs"+string(filepath.Separator)) {
		return 0.05
	}
	return 0.02
}

func preview(text string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes]) + "..."
}

func bestSentence(query string, text string) string {
	queryTokens := tokenSet(query)
	best, _ := bestSentenceInText(query, queryTokens, text)
	return best
}

func bestSentenceFromHits(query string, hits []Hit) string {
	queryTokens := tokenSet(query)
	best := ""
	bestScore := -1.0
	for _, hit := range hits {
		candidate, score := bestSentenceInText(query, queryTokens, hit.Text)
		score += hit.KeywordScore * 0.05
		if candidate != "" && score > bestScore {
			best = candidate
			bestScore = score
		}
	}
	return best
}

func bestSentenceInText(query string, queryTokens map[string]int, text string) (string, float64) {
	parts := splitSentences(text)
	best := ""
	bestScore := -1.0
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if !validAnswerSentence(part) {
			continue
		}
		score := answerSentenceScore(query, queryTokens, part)
		if score > bestScore {
			best = part
			bestScore = score
		}
	}
	return best, bestScore
}

func answerSentenceScore(query string, queryTokens map[string]int, sentence string) float64 {
	score := keywordScore(queryTokens, tokenSet(sentence))
	normalizedQuery := normalizeTokens(query)
	normalizedSentence := normalizeTokens(sentence)
	if isDefinitionQuery(normalizedQuery) && hasDefinitionShape(normalizedSentence) {
		score += 0.35
	}
	return score
}

func isDefinitionQuery(query string) bool {
	return strings.Contains(query, "que es ") ||
		strings.Contains(query, "what is ") ||
		strings.Contains(query, "what are ")
}

func hasDefinitionShape(sentence string) bool {
	return strings.Contains(sentence, " is ") ||
		strings.Contains(sentence, " are ") ||
		strings.Contains(sentence, " es ") ||
		strings.Contains(sentence, " son ")
}

func validAnswerSentence(text string) bool {
	tokens := meaningfulTokenSet(text)
	if len(tokens) < 3 {
		return false
	}
	if strings.Contains(text, "=") || strings.Contains(text, "include_") || strings.Contains(text, "await ") {
		return false
	}
	return len([]rune(text)) >= 25
}

func citationPath(ctx context.Context, store *memory.Store, hit Hit) string {
	if url := sourceURLFromText(hit.Text); url != "" {
		return url
	}
	if store == nil {
		return hit.Path
	}
	base := filepath.Base(hit.Path)
	if !strings.HasPrefix(base, "source_") || !strings.HasSuffix(base, ".txt") {
		return hit.Path
	}
	sourceID := strings.TrimSuffix(base, ".txt")
	source, ok, err := store.WebSourceByID(ctx, sourceID)
	if err != nil || !ok {
		return hit.Path
	}
	if source.FinalURL != "" {
		return source.FinalURL
	}
	if source.URL != "" {
		return source.URL
	}
	return hit.Path
}

func sourceURLFromText(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Source URL:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Source URL:"))
		}
	}
	return ""
}

func splitSentences(text string) []string {
	var out []string
	var b strings.Builder
	for _, r := range text {
		b.WriteRune(r)
		if r == '.' || r == '\n' || r == '?' || r == '!' {
			part := strings.TrimSpace(b.String())
			if part != "" {
				out = append(out, part)
			}
			b.Reset()
		}
	}
	if strings.TrimSpace(b.String()) != "" {
		out = append(out, strings.TrimSpace(b.String()))
	}
	return out
}

func NormalizeTextForTest(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, text)
}
