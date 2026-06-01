package research

import (
	"regexp"
	"strings"
	"unicode"
)

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

	isDef := looksLikeDefinitionQuery(query)
	best := ""
	bestScore := 0.0
	for _, raw := range evidence {
		cleaned := cleanExtractedSentence(raw)
		for _, sentence := range splitSentences(cleaned) {

			sentence = strings.TrimSpace(trimBeforeCoreTerm(query, sentence))
			if len([]rune(sentence)) < 35 || likelyTitle(sentence) || looksLikeStructuredDump(sentence) {
				continue
			}
			score := overlapScore(queryTokens, keywordSet(sentence))
			// For "what is X" questions, prefer a sentence that actually defines
			// the subject ("X es …") over a tangential one with similar overlap.
			if score > 0 {
				score += definitionBonus(isDef, sentence)
			}
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
	best = stripLeadingChrome(best)
	best = tidyProse(best)
	if len([]rune(best)) > 420 {
		runes := []rune(best)
		best = string(runes[:420])
		if idx := strings.LastIndex(best, " "); idx > 180 {
			best = best[:idx]
		}
	}
	best = trimDanglingTail(best)
	best = balanceTrailingParens(best)
	if best == "" {
		return "", false
	}
	if !strings.HasSuffix(best, ".") && !strings.HasSuffix(best, "!") && !strings.HasSuffix(best, "?") {
		best += "."
	}
	return strings.TrimSpace(best), true
}

// definitionConnectives are the verb phrases a real definition uses ("X es …",
// "X is …"). They are grammar, not domain facts.
var definitionConnectives = []string{
	" es ", " es un", " es una", " es el", " es la", " son ", " son un", " son las", " son los",
	" se define", " se denomina", " se conoce", " consiste en", " se refiere",
	" is ", " are ", " is a", " is an", " is the", " refers to",
}

// looksLikeStructuredDump reports whether text is a table/infobox/nav dump
// rather than prose — HTML extraction of an infobox concatenates cells, leaving
// telltale adjacent duplicate tokens ("Argentina Argentina Paraguay Paraguay").
// Such text is never a real answer, so synthesis skips it (and harvest must not
// store it as a training target). Generic structure, not domain facts.
func looksLikeStructuredDump(text string) bool {
	words := strings.Fields(text)
	if len(words) < 4 {
		return false
	}
	dups := 0
	for i := 1; i < len(words); i++ {
		if strings.EqualFold(words[i], words[i-1]) && len([]rune(words[i])) >= 3 {
			dups++
		}
	}
	if dups >= 2 {
		return true
	}
	// Navigation/menu dump: a long run dominated by Title-Case items (menu
	// labels, breadcrumbs) rather than prose. Real sentences are mostly
	// lowercase; nav menus are mostly capitalized.
	if len(words) >= 8 {
		caps := 0
		for _, w := range words {
			r := []rune(w)
			if len(r) > 0 && unicode.IsUpper(r[0]) {
				caps++
			}
		}
		if float64(caps)/float64(len(words)) >= 0.45 {
			return true
		}
	}
	return false
}

// looksLikeDefinitionQuery reports whether the user is asking what something is
// (vs. a how-to or a specific detail), so synthesis can prefer a defining
// sentence over a tangential one.
func looksLikeDefinitionQuery(query string) bool {
	n := strings.ToLower(query)
	for _, m := range []string{
		"que es", "qué es", "que son", "qué son", "what is", "what are",
		"explica", "definicion", "definición", "que significa", "qué significa",
		"hablame de", "háblame de", "contame", "contanos", "que es un", "que es una",
	} {
		if strings.Contains(n, m) {
			return true
		}
	}
	return false
}

// definitionBonus rewards a sentence that carries a definitional connective when
// the question asks for a definition. The caller only applies it to sentences
// that already mention the subject, so it tips phrasing, not topic.
func definitionBonus(isDefinitionQuery bool, sentence string) float64 {
	if !isDefinitionQuery {
		return 0
	}
	low := " " + strings.ToLower(sentence) + " "
	for _, c := range definitionConnectives {
		if strings.Contains(low, c) {
			return 0.5
		}
	}
	return 0
}

// Leading-chrome strippers: a publication date ("3 jun 2023 · "), a numeric date
// ("15/07/2024 - "), or a run of separator/quote punctuation that HTML
// extraction prepends to the lead sentence. Calendar and punctuation structure,
// not domain facts.
var (
	leadingSepRe     = regexp.MustCompile(`^[\s"'` + "`" + `·•‹›«»>|–—,;:.\-]+`)
	leadingDateRe    = regexp.MustCompile(`(?i)^\s*\d{1,2}\s+(?:ene|feb|mar|abr|may|jun|jul|ago|sep|sept|oct|nov|dic|jan|apr|aug|dec)\w*\.?\s+\d{4}\b`)
	leadingNumDateRe = regexp.MustCompile(`^\s*\d{1,4}[/.\-]\d{1,2}[/.\-]\d{1,4}\b`)
)

// stripLeadingChrome removes a date byline or separator junk from the START of
// an answer sentence, looping until nothing more peels off ("3 jun 2023 · Una
// integral…" -> "Una integral…").
func stripLeadingChrome(text string) string {
	for {
		before := text
		text = leadingDateRe.ReplaceAllString(text, "")
		text = leadingNumDateRe.ReplaceAllString(text, "")
		text = leadingSepRe.ReplaceAllString(text, "")
		text = strings.TrimSpace(text)
		if text == before {
			break
		}
	}
	return text
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

// balanceTrailingParens drops a dangling, unclosed parenthetical that a
// truncated source snippet leaves behind ("…parámetros (normalmente miles" ->
// "…parámetros"). When there are more "(" than ")", everything from the last
// unmatched "(" is cut, so the sentence reads complete instead of cut off.
func balanceTrailingParens(text string) string {
	if strings.Count(text, "(") <= strings.Count(text, ")") {
		return text
	}
	if idx := strings.LastIndex(text, "("); idx >= 0 {
		return strings.TrimRight(strings.TrimSpace(text[:idx]), " ,;:-")
	}
	return text
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
