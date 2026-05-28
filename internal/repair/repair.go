package repair

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aletheia/internal/verifier"
)

type Counterexample struct {
	Verifier string
	Status   string
	Summary  string
	Stdout   string
	Stderr   string
}

type Candidate struct {
	Path    string
	OldText string
	NewText string
}

func ExtractCounterexample(evidence []verifier.Evidence) (Counterexample, bool) {
	for i := len(evidence) - 1; i >= 0; i-- {
		ev := evidence[i]
		if ev.Status != verifier.StatusFail {
			continue
		}
		summary := strings.TrimSpace(ev.ErrorSummary)
		if summary == "" {
			summary = firstNonEmptyLine(ev.Stderr)
		}
		if summary == "" {
			summary = firstNonEmptyLine(ev.Stdout)
		}
		return Counterexample{
			Verifier: ev.Verifier,
			Status:   ev.Status,
			Summary:  summary,
			Stdout:   ev.Stdout,
			Stderr:   ev.Stderr,
		}, true
	}
	return Counterexample{}, false
}

func BuildCandidate(repoPath string, counterexample Counterexample) (Candidate, error) {
	path := filepath.Join(repoPath, "calculator.go")
	raw, err := os.ReadFile(path)
	if err != nil {
		return Candidate{}, err
	}
	oldText := string(raw)
	newText := strings.Replace(oldText, "return a - b", "return a + b", 1)
	if oldText == newText {
		if counterexample.Summary != "" {
			return Candidate{}, fmt.Errorf("no deterministic Go repair rule matched counterexample: %s", counterexample.Summary)
		}
		return Candidate{}, fmt.Errorf("no deterministic Go repair rule matched")
	}
	return Candidate{
		Path:    path,
		OldText: oldText,
		NewText: newText,
	}, nil
}

func firstNonEmptyLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
