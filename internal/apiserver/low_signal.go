package apiserver

import "strings"

// keyboardMash is a small, bounded set of common gibberish/placeholder tokens.
// It is NOT a knowledge dictionary — it is a structural noise filter.
var keyboardMash = map[string]bool{
	"asdf": true, "asd": true, "asdfgh": true, "qwer": true, "qwerty": true,
	"zxcv": true, "jkl": true, "hjkl": true, "sdf": true, "fgh": true,
	"lorem": true, "ipsum": true, "dolor": true, "test123": true, "aaaa": true,
}

// looksLikeNonsense detects low-signal input (repeated tokens, keyboard mashing,
// vowel-starved gibberish) without relying on a dictionary of real words. It is
// deliberately conservative to avoid rejecting short legitimate queries.
func looksLikeNonsense(query string) bool {
	n := normalizeBasicChat(query)
	fields := strings.Fields(n)
	if len(fields) == 0 {
		return false
	}

	// 1) Repeated-token spam: "asdf asdf asdf", "blah blah blah". Numbers and
	// very short tokens are ignored so math like "99 por 99" is not flagged.
	counts := map[string]int{}
	wordTokens := 0
	for _, f := range fields {
		if len([]rune(f)) < 3 || isNumericToken(f) {
			continue
		}
		wordTokens++
		counts[f]++
	}
	for _, c := range counts {
		if wordTokens >= 2 && c >= 2 && c*2 >= wordTokens {
			return true
		}
	}

	// 2) Keyboard-mash / placeholder tokens making up most of the message.
	mash := 0
	for _, f := range fields {
		if keyboardMash[f] {
			mash++
		}
	}
	if mash > 0 && mash*2 >= len(fields) {
		return true
	}

	// 3) Vowel-starved gibberish: long tokens with almost no vowels, when they
	// dominate the message.
	gibberish := 0
	for _, f := range fields {
		if len([]rune(f)) >= 4 && vowelRatio(f) < 0.15 {
			gibberish++
		}
	}
	if gibberish > 0 && gibberish*2 >= len(fields) {
		return true
	}
	return false
}

func isNumericToken(word string) bool {
	hasDigit := false
	for _, r := range word {
		if r >= '0' && r <= '9' {
			hasDigit = true
			continue
		}
		if r == '.' || r == ',' || r == '-' || r == '%' {
			continue
		}
		return false
	}
	return hasDigit
}

func vowelRatio(word string) float64 {
	vowels := 0
	total := 0
	for _, r := range word {
		if r >= 'a' && r <= 'z' {
			total++
			switch r {
			case 'a', 'e', 'i', 'o', 'u', 'y':
				vowels++
			}
		}
	}
	if total == 0 {
		return 1
	}
	return float64(vowels) / float64(total)
}
