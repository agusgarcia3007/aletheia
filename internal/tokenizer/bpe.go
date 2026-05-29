package tokenizer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type BPEArtifact struct {
	FormatVersion int      `json:"format_version"`
	CreatedAt     string   `json:"created_at"`
	Type          string   `json:"type"`
	VocabSize     int      `json:"vocab_size"`
	Tokens        []string `json:"tokens"`
}

func TrainBPEFromJSONL(datasetPath string, outPath string, vocabSize int) (BPEArtifact, error) {
	if vocabSize <= 0 {
		return BPEArtifact{}, fmt.Errorf("vocab size must be positive")
	}
	f, err := os.Open(datasetPath)
	if err != nil {
		return BPEArtifact{}, err
	}
	defer f.Close()
	freq := map[string]int{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		for _, token := range splitBPETerms(line) {
			freq[token]++
		}
	}
	if err := scanner.Err(); err != nil {
		return BPEArtifact{}, err
	}
	type item struct {
		token string
		count int
	}
	items := make([]item, 0, len(freq))
	for token, count := range freq {
		items = append(items, item{token: token, count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count == items[j].count {
			return items[i].token < items[j].token
		}
		return items[i].count > items[j].count
	})
	tokens := append([]string(nil), SpecialTokens...)
	seen := map[string]bool{}
	for _, token := range tokens {
		seen[token] = true
	}
	for _, item := range items {
		if len(tokens) >= vocabSize {
			break
		}
		if seen[item.token] {
			continue
		}
		seen[item.token] = true
		tokens = append(tokens, item.token)
	}
	artifact := BPEArtifact{
		FormatVersion: 1,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		Type:          "deterministic_wordpiece_v1",
		VocabSize:     len(tokens),
		Tokens:        tokens,
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return BPEArtifact{}, err
	}
	raw, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return BPEArtifact{}, err
	}
	if err := os.WriteFile(outPath, raw, 0o644); err != nil {
		return BPEArtifact{}, err
	}
	return artifact, nil
}

func splitBPETerms(text string) []string {
	replacer := strings.NewReplacer(
		"{", " ", "}", " ", "[", " ", "]", " ", "(", " ", ")", " ",
		",", " ", ".", " ", ":", " ", ";", " ", "\"", " ", "'", " ",
		"\n", " ", "\t", " ",
	)
	text = strings.ToLower(replacer.Replace(text))
	var out []string
	for _, field := range strings.Fields(text) {
		if len([]rune(field)) <= 1 {
			continue
		}
		out = append(out, field)
	}
	return out
}
