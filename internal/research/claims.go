package research

import (
	"regexp"
	"strings"
	"time"
)

type ClaimExtractor struct{}

var sentenceRe = regexp.MustCompile(`[^.!?\n]+[.!?]?`)
var signalRe = regexp.MustCompile(`(?i)\b(is|are|supports|requires|costs|announced|changed|released|version|date|model|protocol|server|client)\b|[0-9]{2,}`)

func (ClaimExtractor) Extract(query string, sourceID string, doc ExtractedDocument, limit int) []WebClaim {
	if limit <= 0 {
		limit = 6
	}
	keywords := keywordSet(query)
	var claims []WebClaim
	add := func(text string, confidence float64) {
		text = strings.TrimSpace(text)
		if len([]rune(text)) < 35 || len([]rune(text)) > 500 {
			return
		}
		claims = append(claims, WebClaim{
			ID:         sourceID + "_claim_" + itoa(len(claims)+1),
			SourceID:   sourceID,
			Text:       text,
			Confidence: confidence,
			CreatedAt:  time.Now().UTC(),
		})
	}
	if doc.Title != "" {
		add(doc.Title, 0.70)
	}
	for _, match := range sentenceRe.FindAllString(doc.Text, -1) {
		if len(claims) >= limit {
			break
		}
		score := overlapScore(keywords, keywordSet(match))
		if score >= 0.20 || signalRe.MatchString(match) {
			add(match, 0.45+score)
		}
	}
	return claims
}

func keywordSet(text string) map[string]bool {
	out := map[string]bool{}
	for _, field := range strings.Fields(strings.ToLower(text)) {
		field = strings.Trim(field, ".,:;!?()[]{}\"'")
		if len(field) < 3 {
			continue
		}
		out[field] = true
	}
	return out
}

func overlapScore(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var overlap int
	for token := range a {
		if b[token] {
			overlap++
		}
	}
	return float64(overlap) / float64(len(a))
}

func itoa(n int) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = digits[n%10]
		n /= 10
	}
	return string(b[i:])
}
