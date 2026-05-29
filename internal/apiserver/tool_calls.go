package apiserver

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
	if modelID != hephaestusModelName || len(req.Tools) == 0 {
		return assistantToolCall{}, false
	}
	if hasToolResult(req.Messages) {
		return assistantToolCall{}, false
	}
	last := normalizeBasicChat(lastUserMessage(req.Messages))
	if last == "" {
		return assistantToolCall{}, false
	}
	if !isToolUseRequest(last) {
		return assistantToolCall{}, false
	}
	tool, ok := chooseCodingTool(last, req.Tools)
	if !ok {
		return assistantToolCall{}, false
	}
	args := synthesizeToolArguments(last, tool)
	raw, _ := json.Marshal(args)
	return assistantToolCall{
		ID:   "call_" + randomHex(8),
		Type: "function",
		Function: assistantToolCallFunction{
			Name:      tool.Function.Name,
			Arguments: string(raw),
		},
	}, true
}

func chooseCodingTool(prompt string, tools []chatTool) (chatTool, bool) {
	preferred := []string{"read", "grep", "search", "list", "edit", "write", "patch", "run", "exec", "bash", "shell", "command", "test"}
	if strings.Contains(prompt, "analiza") || strings.Contains(prompt, "analyze") || strings.Contains(prompt, "inspect") || strings.Contains(prompt, "repositorio") || strings.Contains(prompt, "repository") {
		preferred = []string{"list", "glob", "find", "search", "grep", "read", "run", "exec", "bash", "shell", "command"}
	}
	if strings.Contains(prompt, "test") || strings.Contains(prompt, "build") || strings.Contains(prompt, "run") {
		preferred = []string{"run", "exec", "bash", "shell", "command", "test", "read", "grep", "search", "list", "edit", "write", "patch"}
	}
	if strings.Contains(prompt, "modifica") || strings.Contains(prompt, "edit") || strings.Contains(prompt, "patch") || strings.Contains(prompt, "fix") || strings.Contains(prompt, "arregla") {
		preferred = []string{"edit", "write", "patch", "read", "grep", "search", "run", "exec", "bash", "shell", "command"}
	}
	for _, needle := range preferred {
		for _, tool := range tools {
			name := strings.ToLower(tool.Function.Name)
			if strings.Contains(name, needle) {
				return tool, true
			}
		}
	}
	return tools[0], true
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

func hasToolResult(messages []chatMessage) bool {
	for _, msg := range messages {
		if msg.Role == "tool" {
			return true
		}
	}
	return false
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
		return "Recibí información del repositorio. Con esa evidencia, el siguiente paso útil es revisar los archivos principales y detectar stack, comandos de test/build y puntos de entrada. Resultado observado:\n\n" + results[len(results)-1], true
	}
	return "Recibí el resultado de la herramienta y no voy a repetir la misma llamada. Resultado:\n\n" + results[len(results)-1], true
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
