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
	best = tidyProse(best)
	if len([]rune(best)) > 420 {
		runes := []rune(best)
		best = string(runes[:420])
		if idx := strings.LastIndex(best, " "); idx > 180 {
			best = best[:idx]
		}
	}
	best = trimDanglingTail(best)
	if best == "" {
		return "", false
	}
	if !strings.HasSuffix(best, ".") && !strings.HasSuffix(best, "!") && !strings.HasSuffix(best, "?") {
		best += "."
	}
	return strings.TrimSpace(best), true
}

// tidyProse repairs the cosmetic damage HTML extraction leaves behind: stray
// whitespace, spaces before punctuation ("texto ." -> "texto.") and curly
// quotes. It does not change meaning — only presentation.
func tidyProse(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	for _, p := range []string{",", ".", ";", ":", "!", "?", ")"} {
		text = strings.ReplaceAll(text, " "+p, p)
	}
	text = strings.ReplaceAll(text, "( ", "(")
	replacer := strings.NewReplacer("“", "\"", "”", "\"", "’", "'", "‘", "'")
	return strings.TrimSpace(replacer.Replace(text))
}

// trimDanglingTail removes the incomplete fragment a truncated snippet leaves
// at the end ("...gracias a la energía que ..." -> "...gracias a la energía").
// A sentence that trails off into an ellipsis or a bare connector reads as
// broken, so we cut back to the last complete word.
func trimDanglingTail(text string) string {
	text = strings.TrimSpace(text)
	for {
		trimmed := strings.TrimRight(text, " .…")
		trimmed = strings.TrimSuffix(trimmed, "...")
		trimmed = strings.TrimRight(trimmed, " ,;:")
		lower := strings.ToLower(trimmed)
		cut := false
		for _, tail := range []string{" que", " y", " e", " o", " de", " del", " en", " la", " el", " los", " las", " un", " una", " a", " con", " por", " para", " su", " es"} {
			if strings.HasSuffix(lower, tail) {
				trimmed = strings.TrimSpace(trimmed[:len(trimmed)-len(tail)])
				cut = true
				break
			}
		}
		if trimmed == text && !cut {
			return strings.TrimRight(text, " .…,;:")
		}
		text = trimmed
		if !cut {
			return text
		}
	}
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
