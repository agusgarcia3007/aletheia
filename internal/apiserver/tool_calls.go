package apiserver

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type assistantToolCall struct {
	ID       string                    `json:"id"`
	Type     string                    `json:"type"`
	Function assistantToolCallFunction `json:"function"`
}

type assistantToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

func (s *Server) codingToolCall(modelID string, req chatCompletionRequest) (assistantToolCall, bool) {
	if modelID != hephaestusModelName && modelID != mikrosModelName {
		return assistantToolCall{}, false
	}
	if len(req.Tools) == 0 {
		return assistantToolCall{}, false
	}
	choice := parseToolChoice(req.ToolChoice)
	if choice.Mode == "none" {
		return assistantToolCall{}, false
	}
	last := normalizeBasicChat(lastUserMessage(req.Messages))
	if last == "" {
		return assistantToolCall{}, false
	}
	if choice.Mode != "required" && choice.Name == "" && !isToolUseRequest(last) {
		return assistantToolCall{}, false
	}
	state := inspectToolState(req.Messages)
	if state.CallCount >= agentMaxToolCalls() {
		return assistantToolCall{}, false
	}
	candidate, ok := chooseNextTool(last, req.Tools, choice, state)
	if !ok {
		return assistantToolCall{}, false
	}
	raw, _ := json.Marshal(candidate.Args)
	return assistantToolCall{
		ID:   "call_" + randomHex(8),
		Type: "function",
		Function: assistantToolCallFunction{
			Name:      candidate.Tool.Function.Name,
			Arguments: string(raw),
		},
	}, true
}

type toolChoice struct {
	Mode string
	Name string
}

type toolCandidate struct {
	Tool chatTool
	Args map[string]any
}

type toolState struct {
	CallCount    int
	Fingerprints map[string]bool
	Results      []string
}

func parseToolChoice(raw json.RawMessage) toolChoice {
	if len(raw) == 0 || string(raw) == "null" {
		return toolChoice{Mode: "auto"}
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		text = strings.TrimSpace(strings.ToLower(text))
		switch text {
		case "none", "auto", "required":
			return toolChoice{Mode: text}
		default:
			return toolChoice{Mode: "auto"}
		}
	}
	var obj struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && strings.TrimSpace(obj.Function.Name) != "" {
		return toolChoice{Mode: "required", Name: strings.TrimSpace(obj.Function.Name)}
	}
	return toolChoice{Mode: "auto"}
}

func inspectToolState(messages []chatMessage) toolState {
	state := toolState{Fingerprints: map[string]bool{}}
	for _, msg := range messages {
		for _, call := range msg.ToolCalls {
			state.CallCount++
			state.Fingerprints[toolFingerprint(call.Function.Name, call.Function.Arguments)] = true
		}
		if msg.Role == "tool" && strings.TrimSpace(msg.Content) != "" {
			state.Results = append(state.Results, strings.TrimSpace(msg.Content))
		}
	}
	return state
}

func chooseNextTool(prompt string, tools []chatTool, choice toolChoice, state toolState) (toolCandidate, bool) {
	var candidates []toolCandidate
	if choice.Name != "" {
		if tool, ok := toolByExactName(tools, choice.Name); ok {
			candidates = append(candidates, candidateForPrompt(prompt, tool, state))
		}
	} else if isRepoAnalysisPrompt(prompt) {
		candidates = repoAnalysisCandidates(prompt, tools, state)
	} else {
		candidates = defaultToolCandidates(prompt, tools, state)
	}
	for _, candidate := range candidates {
		if candidate.Tool.Function.Name == "" || unsafeToolCandidate(candidate) {
			continue
		}
		raw, _ := json.Marshal(candidate.Args)
		if state.Fingerprints[toolFingerprint(candidate.Tool.Function.Name, string(raw))] {
			continue
		}
		return candidate, true
	}
	if choice.Mode == "required" && choice.Name == "" {
		for _, tool := range tools {
			candidate := candidateForPrompt(prompt, tool, state)
			if unsafeToolCandidate(candidate) {
				continue
			}
			raw, _ := json.Marshal(candidate.Args)
			if !state.Fingerprints[toolFingerprint(candidate.Tool.Function.Name, string(raw))] {
				return candidate, true
			}
		}
	}
	return toolCandidate{}, false
}

func repoAnalysisCandidates(prompt string, tools []chatTool, state toolState) []toolCandidate {
	var out []toolCandidate
	if resultsIndicateNoFiles(state.Results) {
		out = append(out, repoListingCandidates(prompt, tools)...)
		out = append(out, candidatesByPreferenceWithFile(prompt, tools, []string{"read", "open", "cat"}, "README.md")...)
		return out
	}
	if len(state.Results) == 0 {
		out = append(out, repoListingCandidates(prompt, tools)...)
	}
	if manifest := manifestFromResults(state.Results); manifest != "" {
		out = append(out, candidatesByPreferenceWithFile(prompt, tools, []string{"read", "open", "cat"}, manifest)...)
	}
	if len(state.Results) >= 2 {
		out = append(out, candidatesByPreferenceWithQuery(prompt, tools, []string{"grep", "search", "rg"}, "test main package scripts cmd src")...)
	}
	if len(out) == 0 {
		out = append(out, candidatesByPreference(prompt, tools, []string{"search", "grep", "read"})...)
	}
	return out
}

func repoListingCandidates(prompt string, tools []chatTool) []toolCandidate {
	candidates := candidatesByPreferenceStrict(prompt, tools, []string{"list", "glob", "find"})
	for i := range candidates {
		name := strings.ToLower(candidates[i].Tool.Function.Name)
		if strings.Contains(name, "glob") {
			setQueryArgument(candidates[i].Args, "**/*")
		}
		if strings.Contains(name, "find") {
			setQueryArgument(candidates[i].Args, "README.md go.mod package.json Cargo.toml pyproject.toml")
		}
		setPathArgument(candidates[i].Args, ".")
	}
	return candidates
}

func candidatesByPreferenceStrict(prompt string, tools []chatTool, preferred []string) []toolCandidate {
	var out []toolCandidate
	for _, needle := range preferred {
		for _, tool := range tools {
			if strings.Contains(strings.ToLower(tool.Function.Name), needle) {
				out = append(out, candidateForPrompt(prompt, tool, toolState{}))
			}
		}
	}
	return out
}

func defaultToolCandidates(prompt string, tools []chatTool, state toolState) []toolCandidate {
	preferred := []string{"read", "grep", "search", "list", "run", "test"}
	if strings.Contains(prompt, "test") || strings.Contains(prompt, "build") || strings.Contains(prompt, "run") {
		preferred = []string{"run", "exec", "bash", "shell", "command", "test", "read", "grep", "search", "list"}
	}
	if strings.Contains(prompt, "modifica") || strings.Contains(prompt, "edit") || strings.Contains(prompt, "patch") || strings.Contains(prompt, "fix") || strings.Contains(prompt, "arregla") {
		preferred = []string{"read", "grep", "search", "patch", "edit", "write"}
	}
	return candidatesByPreference(prompt, tools, preferred)
}

func candidatesByPreference(prompt string, tools []chatTool, preferred []string) []toolCandidate {
	var out []toolCandidate
	for _, needle := range preferred {
		for _, tool := range tools {
			name := strings.ToLower(tool.Function.Name)
			if strings.Contains(name, needle) {
				out = append(out, candidateForPrompt(prompt, tool, toolState{}))
			}
		}
	}
	if len(out) == 0 {
		for _, tool := range tools {
			out = append(out, candidateForPrompt(prompt, tool, toolState{}))
		}
	}
	return out
}

func candidatesByPreferenceWithFile(prompt string, tools []chatTool, preferred []string, file string) []toolCandidate {
	candidates := candidatesByPreference(prompt, tools, preferred)
	for i := range candidates {
		setPathArgument(candidates[i].Args, file)
	}
	return candidates
}

func candidatesByPreferenceWithQuery(prompt string, tools []chatTool, preferred []string, query string) []toolCandidate {
	candidates := candidatesByPreference(prompt, tools, preferred)
	for i := range candidates {
		setQueryArgument(candidates[i].Args, query)
	}
	return candidates
}

func candidateForPrompt(prompt string, tool chatTool, state toolState) toolCandidate {
	return toolCandidate{Tool: tool, Args: synthesizeToolArguments(prompt, tool)}
}

func toolByExactName(tools []chatTool, name string) (chatTool, bool) {
	for _, tool := range tools {
		if tool.Function.Name == name {
			return tool, true
		}
	}
	return chatTool{}, false
}

func synthesizeToolArguments(prompt string, tool chatTool) map[string]any {
	args := map[string]any{}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	_ = json.Unmarshal(tool.Function.Parameters, &schema)
	for name, raw := range schema.Properties {
		args[name] = defaultArgument(tool.Function.Name, name, raw, prompt)
	}
	for _, name := range schema.Required {
		if _, ok := args[name]; !ok {
			args[name] = defaultArgument(tool.Function.Name, name, nil, prompt)
		}
	}
	return args
}

func defaultArgument(toolName string, name string, raw json.RawMessage, prompt string) any {
	toolLower := strings.ToLower(toolName)
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "limit"):
		return 200
	case strings.Contains(lower, "offset"):
		return 0
	case strings.Contains(lower, "command") || strings.Contains(lower, "cmd"):
		if strings.Contains(prompt, "test") {
			return "go test ./..."
		}
		return "pwd"
	case strings.Contains(lower, "path") || strings.Contains(lower, "file"):
		if strings.Contains(prompt, "analiza") || strings.Contains(prompt, "analyze") || strings.Contains(prompt, "repositorio") || strings.Contains(prompt, "repository") {
			if strings.Contains(toolLower, "list") || strings.Contains(toolLower, "glob") || strings.Contains(toolLower, "find") || strings.Contains(toolLower, "search") || strings.Contains(toolLower, "grep") {
				return "."
			}
			return "README.md"
		}
		return "."
	case strings.Contains(lower, "query") || strings.Contains(lower, "pattern") || strings.Contains(lower, "search"):
		if isRepoAnalysisPrompt(normalizeBasicChat(prompt)) {
			if strings.Contains(toolLower, "glob") {
				return "**/*"
			}
			if strings.Contains(toolLower, "find") {
				return "README.md go.mod package.json Cargo.toml pyproject.toml"
			}
		}
		return prompt
	case strings.Contains(lower, "content") || strings.Contains(lower, "text"):
		return ""
	}
	var prop struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &prop)
	switch prop.Type {
	case "boolean":
		return false
	case "integer", "number":
		return 0
	case "array":
		return []any{}
	case "object":
		return map[string]any{}
	default:
		return prompt
	}
}

func setPathArgument(args map[string]any, path string) {
	for key := range args {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "path") || strings.Contains(lower, "file") {
			args[key] = path
		}
	}
}

func setQueryArgument(args map[string]any, query string) {
	for key := range args {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "query") || strings.Contains(lower, "pattern") || strings.Contains(lower, "search") {
			args[key] = query
		}
	}
}

func codingToolResultReply(messages []chatMessage) (string, bool) {
	var results []string
	for _, msg := range messages {
		if msg.Role != "tool" {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if len([]rune(content)) > 1600 {
			runes := []rune(content)
			content = string(runes[:1600]) + "..."
		}
		results = append(results, content)
	}
	if len(results) == 0 {
		return "", false
	}
	last := normalizeBasicChat(lastUserMessage(messages))
	if strings.Contains(last, "analiza") || strings.Contains(last, "analyze") || strings.Contains(last, "repositorio") || strings.Contains(last, "repository") {
		return repoAnalysisReply(results), true
	}
	return "Recibí el resultado de la herramienta y no voy a repetir la misma llamada. Resultado:\n\n" + results[len(results)-1], true
}

func repoAnalysisReply(results []string) string {
	combined := strings.Join(results, "\n")
	if strings.Contains(strings.ToLower(combined), "no files found") {
		return "OpenCode no devolvió archivos del repositorio. No voy a inventar un análisis; revisá que la sesión esté abierta en el directorio correcto o que la herramienta de listado tenga permisos."
	}
	stack := "No pude inferir el stack todavía."
	switch {
	case strings.Contains(combined, "go.mod"):
		stack = "Parece un repo Go."
	case strings.Contains(combined, "package.json"):
		stack = "Parece un repo JavaScript/TypeScript."
	case strings.Contains(combined, "Cargo.toml"):
		stack = "Parece un repo Rust."
	case strings.Contains(combined, "pyproject.toml") || strings.Contains(combined, "requirements.txt"):
		stack = "Parece un repo Python."
	}
	observed := compactToolText(combined, 1200)
	return fmt.Sprintf("Recibí evidencia del repositorio. %s\n\nObservado:\n%s\n\nSiguiente paso sugerido: leer el manifest principal, ubicar tests y confirmar el comando de build/test antes de proponer cambios.", stack, observed)
}

func compactToolText(text string, limit int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	var kept []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		kept = append(kept, line)
		if len(strings.Join(kept, "\n")) > limit {
			break
		}
	}
	out := strings.Join(kept, "\n")
	runes := []rune(out)
	if len(runes) > limit {
		out = string(runes[:limit]) + "..."
	}
	return out
}

func manifestFromResults(results []string) string {
	combined := strings.Join(results, "\n")
	for _, candidate := range []string{"README.md", "go.mod", "package.json", "Cargo.toml", "pyproject.toml"} {
		if strings.Contains(combined, candidate) {
			return candidate
		}
	}
	return ""
}

func resultsIndicateNoFiles(results []string) bool {
	if len(results) == 0 {
		return false
	}
	last := strings.ToLower(strings.TrimSpace(results[len(results)-1]))
	return strings.Contains(last, "no files found") || strings.Contains(last, "no files")
}

func isRepoAnalysisPrompt(prompt string) bool {
	return strings.Contains(prompt, "analiza") ||
		strings.Contains(prompt, "analyze") ||
		strings.Contains(prompt, "inspect") ||
		strings.Contains(prompt, "repositorio") ||
		strings.Contains(prompt, "repository") ||
		strings.Contains(prompt, "repo")
}

func unsafeToolCandidate(candidate toolCandidate) bool {
	name := strings.ToLower(candidate.Tool.Function.Name)
	if hasAny(name, "delete", "remove", "rm", "deploy", "secret") {
		return true
	}
	if hasAny(name, "run", "exec", "bash", "shell", "command") {
		command := strings.ToLower(commandArgument(candidate.Args))
		if command == "" {
			return true
		}
		allowed := []string{"pwd", "ls", "go test", "npm test", "bun test", "cargo test", "pytest"}
		for _, prefix := range allowed {
			if strings.HasPrefix(command, prefix) {
				return false
			}
		}
		return true
	}
	return false
}

func commandArgument(args map[string]any) string {
	for key, value := range args {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "command") || strings.Contains(lower, "cmd") {
			if text, ok := value.(string); ok {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

func toolFingerprint(name string, args string) string {
	var normalized any
	if err := json.Unmarshal([]byte(args), &normalized); err == nil {
		if raw, marshalErr := json.Marshal(normalized); marshalErr == nil {
			args = string(raw)
		}
	}
	return strings.ToLower(strings.TrimSpace(name)) + ":" + strings.TrimSpace(args)
}

func agentMaxToolCalls() int {
	raw := strings.TrimSpace(os.Getenv("ALETHEIA_AGENT_MAX_TOOL_CALLS"))
	if raw == "" {
		return 6
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 6
	}
	return value
}

func (s *Server) toolCallResponse(modelID string, toolCall assistantToolCall, usage map[string]int) map[string]any {
	return map[string]any{
		"id":      s.id("chatcmpl"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   modelID,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":       "assistant",
					"content":    nil,
					"tool_calls": []assistantToolCall{toolCall},
				},
				"finish_reason": "tool_calls",
			},
		},
		"usage": usage,
	}
}

func randomHex(bytesLen int) string {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(buf)
}
