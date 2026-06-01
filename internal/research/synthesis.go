package research

import "strings"

// CanonicalAnswer turns retrieved evidence into a direct answer by selecting the
// best-supported sentence: the one whose tokens overlap most with the query.
//
// It bakes in NO domain facts — no countries, no tournaments, no dates. Every
// string it can return is extracted verbatim (and lightly cleaned of page
// chrome) from the evidence the retriever/research worker actually gathered.
// When no sentence has meaningful support it returns ok=false so the caller
// abstains. This is the synthesis layer's contribution to principle #3: facts
// live in storage, not in the weights and not in this source file.
func CanonicalAnswer(query string, evidence []string) (string, bool) {
	queryTokens := keywordSet(query)
	if len(queryTokens) == 0 {
		return "", false
	}
	threshold := requiredSynthesisOverlap(len(queryTokens))

	best := ""
	bestScore := 0.0
	for _, raw := range evidence {
		cleaned := cleanExtractedSentence(raw)
		for _, sentence := range splitSentences(cleaned) {
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
	}
	if best == "" || bestScore < threshold {
		return "", false
	}

	best = trimBeforeCoreTerm(query, best)
	best = strings.Join(strings.Fields(best), " ")
	if len([]rune(best)) > 420 {
		runes := []rune(best)
		best = string(runes[:420])
		if idx := strings.LastIndex(best, " "); idx > 180 {
			best = best[:idx]
		}
	}
	if !strings.HasSuffix(best, ".") && !strings.HasSuffix(best, "!") && !strings.HasSuffix(best, "?") {
		best += "."
	}
	return strings.TrimSpace(best), true
}

// requiredSynthesisOverlap scales the minimum query/sentence token overlap with
// the query length: short queries must match almost entirely, long queries can
// settle for a meaningful fraction. Below the threshold we abstain rather than
// surface a loosely-related sentence as if it were the answer.
func requiredSynthesisOverlap(queryTokens int) float64 {
	switch {
	case queryTokens <= 2:
		return 0.5
	case queryTokens <= 4:
		return 0.4
	default:
		return 0.3
	}
}
