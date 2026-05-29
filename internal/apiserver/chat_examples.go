package apiserver

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const chatExamplesFile = "chat_examples.jsonl"

type trainedChatExample struct {
	Prompt          string `json:"prompt"`
	Completion      string `json:"completion"`
	normalizedUser  string
	meaningfulTerms map[string]bool
}

func loadTrainedChatExamples(checkpoint string) ([]trainedChatExample, error) {
	path := filepath.Join(checkpoint, chatExamplesFile)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var examples []trainedChatExample
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ex trainedChatExample
		if err := json.Unmarshal([]byte(line), &ex); err != nil {
			return nil, err
		}
		user := normalizeBasicChat(lastUserFromPrompt(ex.Prompt))
		ex.normalizedUser = user
		ex.meaningfulTerms = meaningfulChatTokens(user)
		examples = append(examples, ex)
	}
	return examples, scanner.Err()
}

func trainedExampleReply(served *servedModel, messages []chatMessage) (string, bool) {
	if served == nil || len(served.ChatExamples) == 0 {
		return "", false
	}
	user := normalizeBasicChat(lastUserMessage(messages))
	if user == "" {
		return "", false
	}
	queryTerms := meaningfulChatTokens(user)
	var best trainedChatExample
	bestScore := 0.0
	for _, ex := range served.ChatExamples {
		if !frameworkCompatible(user, ex.normalizedUser) {
			continue
		}
		score := exampleScore(user, queryTerms, ex)
		if score > bestScore {
			best = ex
			bestScore = score
		}
	}
	if bestScore < 0.72 {
		return "", false
	}
	completion := strings.TrimSpace(strings.ReplaceAll(best.Completion, "<EOS>", ""))
	return completion, completion != ""
}

func exampleScore(user string, queryTerms map[string]bool, ex trainedChatExample) float64 {
	if user == ex.normalizedUser {
		return 1
	}
	if len(queryTerms) == 0 || len(ex.meaningfulTerms) == 0 {
		return 0
	}
	overlap := meaningfulOverlap(queryTerms, ex.meaningfulTerms)
	coverageQuery := float64(overlap) / float64(len(queryTerms))
	coverageExample := float64(overlap) / float64(len(ex.meaningfulTerms))
	if len(queryTerms) <= 2 && coverageQuery < 1 {
		return 0
	}
	return (coverageQuery * 0.65) + (coverageExample * 0.35)
}

func frameworkCompatible(user string, example string) bool {
	frameworks := []string{"react", "vue", "svelte", "angular"}
	for _, framework := range frameworks {
		userHas := strings.Contains(user, framework)
		exampleHas := strings.Contains(example, framework)
		if userHas != exampleHas {
			return false
		}
	}
	return true
}

func lastUserFromPrompt(prompt string) string {
	idx := strings.LastIndex(prompt, "<USER>")
	if idx < 0 {
		return prompt
	}
	rest := prompt[idx+len("<USER>"):]
	end := strings.Index(rest, "<ASSISTANT>")
	if end >= 0 {
		return rest[:end]
	}
	return rest
}
