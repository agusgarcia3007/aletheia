package verifier

import (
	"fmt"
	"strings"
	"time"
	"unicode"
)

const TextEvidenceName = "text_evidence"

type TextEvidenceCitation struct {
	ChunkID int64
	Text    string
}

type TextEvidenceVerifier struct{}

func (TextEvidenceVerifier) Name() string {
	return TextEvidenceName
}

func (TextEvidenceVerifier) Verify(answer string, citations []TextEvidenceCitation) Evidence {
	return VerifyTextEvidence(answer, citations)
}

func VerifyTextEvidence(answer string, citations []TextEvidenceCitation) Evidence {
	ev := Evidence{
		Verifier:  TextEvidenceName,
		Status:    StatusPass,
		Score:     1,
		Timestamp: time.Now().UTC(),
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		ev.Status = StatusUnknown
		ev.Score = 0
		ev.ErrorSummary = "empty answer"
		return ev
	}
	if len(citations) == 0 {
		ev.Status = StatusFail
		ev.Score = 0
		ev.ErrorSummary = "answer has no citations"
		return ev
	}

	var cited strings.Builder
	for _, citation := range citations {
		if citation.ChunkID <= 0 || strings.TrimSpace(citation.Text) == "" {
			ev.Status = StatusFail
			ev.Score = 0
			ev.ErrorSummary = "citation does not reference an existing non-empty chunk"
			return ev
		}
		fmt.Fprintf(&cited, " chunk:%d", citation.ChunkID)
		ev.Artifacts = append(ev.Artifacts, fmt.Sprintf("chunk:%d", citation.ChunkID))
		cited.WriteString(" ")
		cited.WriteString(citation.Text)
	}
	if !hasSupportedTerms(answer, cited.String()) {
		ev.Status = StatusFail
		ev.Score = 0
		ev.ErrorSummary = "answer terms are not supported by cited chunks"
	}
	return ev
}

func hasSupportedTerms(answer string, evidence string) bool {
	answerTerms := textTerms(answer)
	evidenceTerms := textTerms(evidence)
	for term := range answerTerms {
		if evidenceTerms[term] > 0 {
			return true
		}
	}
	return false
}

func textTerms(text string) map[string]int {
	out := map[string]int{}
	for _, raw := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
	}) {
		term := strings.TrimSpace(raw)
		if len(term) < 4 || textStopWords[term] {
			continue
		}
		out[term]++
	}
	return out
}

var textStopWords = map[string]bool{
	"about":  true,
	"cuando": true,
	"from":   true,
	"para":   true,
	"que":    true,
	"the":    true,
	"this":   true,
	"with":   true,
}
