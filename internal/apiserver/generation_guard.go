package apiserver

import (
	"strings"
	"unicode"
)

// honestFallback is returned whenever the system has no verified, computed, or
// retrieved answer AND the generative checkpoint cannot produce clean output.
// Aletheia abstains instead of emitting raw byte-model noise. The generative
// model only participates once it is trained (Step > 0) and produces clean text.
const honestFallback = "Todavía no tengo una respuesta verificada para eso. Puedo ayudarte con código, cálculos, traducciones cortas, herramientas tipo OpenCode, o buscar evidencia si habilitás research."

// cleanGeneration reports whether decoded model output is presentable to a user.
// The byte-tokenizer bootstrap checkpoint emits replacement runes, control
// characters and leftover action tokens; none of that should ever reach a
// response. This is the single guard that turns "garbage generation" into
// "honest abstention".
func cleanGeneration(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}

	if strings.Contains(trimmed, "<ACT_") || strings.Contains(trimmed, "<ACTION") ||
		strings.Contains(trimmed, "<UNK>") || strings.Contains(trimmed, "<EOS>") {
		return false
	}
	var letters, total, bad int
	for _, r := range trimmed {
		total++
		switch {
		case r == unicode.ReplacementChar:
			bad++
		case r == '\t' || r == '\n' || r == '\r':

		case unicode.IsControl(r):
			bad++
		case unicode.IsLetter(r):
			letters++
		}
	}
	if total == 0 {
		return false
	}

	if float64(bad)/float64(total) > 0.02 {
		return false
	}

	if float64(letters)/float64(total) < 0.45 {
		return false
	}

	realWords := 0
	for _, word := range strings.Fields(trimmed) {
		if countLetters(word) >= 3 {
			realWords++
		}
	}
	return realWords >= 2
}

func countLetters(word string) int {
	n := 0
	for _, r := range word {
		if unicode.IsLetter(r) {
			n++
		}
	}
	return n
}

// safeGenerate runs the checkpoint only when it is trained and validates the
// output. It returns ok=false when the caller should abstain honestly instead
// of serving noise.
func (s *Server) safeGenerate(served *servedModel, prompt string, opts generationOptions) (string, map[string]int, bool) {
	if served == nil || served.Manifest.Step <= 0 {

		return "", nil, false
	}
	text, usage, err := s.generate(served, prompt, opts)
	if err != nil || !cleanGeneration(text) {
		return "", nil, false
	}
	return text, usage, true
}
