package apiserver

import (
	"encoding/json"
	"fmt"
	"strings"
)

type chatCompletionRequest struct {
	Model               string               `json:"model"`
	Messages            []chatMessage        `json:"messages"`
	Tools               []chatTool           `json:"tools,omitempty"`
	ToolChoice          json.RawMessage      `json:"tool_choice,omitempty"`
	ParallelToolCalls   *bool                `json:"parallel_tool_calls,omitempty"`
	Stop                json.RawMessage      `json:"stop,omitempty"`
	N                   *int                 `json:"n,omitempty"`
	Seed                *int                 `json:"seed,omitempty"`
	User                string               `json:"user,omitempty"`
	ResponseFormat      json.RawMessage      `json:"response_format,omitempty"`
	PresencePenalty     *float64             `json:"presence_penalty,omitempty"`
	FrequencyPenalty    *float64             `json:"frequency_penalty,omitempty"`
	MaxTokens           *int                 `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int                 `json:"max_completion_tokens,omitempty"`
	Temperature         *float64             `json:"temperature,omitempty"`
	TopP                *float64             `json:"top_p,omitempty"`
	TopK                *int                 `json:"top_k,omitempty"`
	Stream              bool                 `json:"stream,omitempty"`
	StreamOptions       *streamOptions       `json:"stream_options,omitempty"`
	Aletheia            *aletheiaChatOptions `json:"aletheia,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage,omitempty"`
}

type aletheiaChatOptions struct {
	Research string `json:"research,omitempty"`
}

type completionRequest struct {
	Model       string   `json:"model"`
	Prompt      string   `json:"prompt"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	TopK        *int     `json:"top_k,omitempty"`
}

type researchRequest struct {
	Query      string `json:"query"`
	Mode       string `json:"mode,omitempty"`
	MaxSources int    `json:"max_sources,omitempty"`
}

type chatMessage struct {
	Role       string              `json:"role"`
	Content    string              `json:"content"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
	Name       string              `json:"name,omitempty"`
	ToolCalls  []assistantToolCall `json:"tool_calls,omitempty"`
}

type chatTool struct {
	Type     string           `json:"type"`
	Function chatToolFunction `json:"function"`
}

type chatToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

func (m *chatMessage) UnmarshalJSON(raw []byte) error {
	var aux struct {
		Role       string              `json:"role"`
		Content    json.RawMessage     `json:"content"`
		ToolCallID string              `json:"tool_call_id,omitempty"`
		Name       string              `json:"name,omitempty"`
		ToolCalls  []assistantToolCall `json:"tool_calls,omitempty"`
	}
	if err := json.Unmarshal(raw, &aux); err != nil {
		return err
	}
	m.Role = strings.TrimSpace(aux.Role)
	m.ToolCallID = strings.TrimSpace(aux.ToolCallID)
	m.Name = strings.TrimSpace(aux.Name)
	m.ToolCalls = aux.ToolCalls
	if m.Role == "" {
		return fmt.Errorf("message role is required")
	}
	if len(aux.Content) == 0 || string(aux.Content) == "null" {
		m.Content = ""
		return nil
	}
	var text string
	if err := json.Unmarshal(aux.Content, &text); err == nil {
		m.Content = text
		return nil
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(aux.Content, &parts); err != nil {
		return fmt.Errorf("message content must be a string or text content parts")
	}
	var b strings.Builder
	for _, part := range parts {
		if part.Type == "" || part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	m.Content = b.String()
	return nil
}

func chatPrompt(messages []chatMessage) (string, error) {
	if len(messages) == 0 {
		return "", fmt.Errorf("messages are required")
	}
	var b strings.Builder
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		switch msg.Role {
		case "system", "developer":
			if content != "" {
				b.WriteString("<SYSTEM>")
				b.WriteString(content)
			}
		case "user":
			b.WriteString("<USER>")
			b.WriteString(content)
		case "assistant":
			b.WriteString("<ASSISTANT>")
			b.WriteString(content)
		case "tool":
			b.WriteString("<RESULT>")
			if msg.Name != "" {
				b.WriteString(msg.Name)
				b.WriteString(": ")
			}
			b.WriteString(content)
		default:
			return "", fmt.Errorf("unsupported message role %q", msg.Role)
		}
	}
	b.WriteString("<ASSISTANT>")
	return b.String(), nil
}
