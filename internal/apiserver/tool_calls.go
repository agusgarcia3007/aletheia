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
	last := normalizeBasicChat(lastUserMessage(req.Messages))
	if last == "" {
		return assistantToolCall{}, false
	}
	if !isCodingTask(last) && !hasAny(last, "read", "search", "grep", "inspect", "list", "run", "test", "build", "edit", "patch", "write") {
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
		args[name] = defaultArgument(name, raw, prompt)
	}
	for _, name := range schema.Required {
		if _, ok := args[name]; !ok {
			args[name] = defaultArgument(name, nil, prompt)
		}
	}
	return args
}

func defaultArgument(name string, raw json.RawMessage, prompt string) any {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "command") || strings.Contains(lower, "cmd"):
		if strings.Contains(prompt, "test") {
			return "go test ./..."
		}
		return "pwd"
	case strings.Contains(lower, "path") || strings.Contains(lower, "file"):
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
