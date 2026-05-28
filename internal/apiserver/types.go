package apiserver

import (
	"encoding/json"
	"fmt"
	"strings"
)

type chatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	TopP        *float64      `json:"top_p,omitempty"`
	TopK        *int          `json:"top_k,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

type completionRequest struct {
	Model       string   `json:"model"`
	Prompt      string   `json:"prompt"`
	MaxTokens   *int     `json:"max_tokens,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	TopP        *float64 `json:"top_p,omitempty"`
	TopK        *int     `json:"top_k,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (m *chatMessage) UnmarshalJSON(raw []byte) error {
	var aux struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &aux); err != nil {
		return err
	}
	m.Role = strings.TrimSpace(aux.Role)
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
		default:
			return "", fmt.Errorf("unsupported message role %q", msg.Role)
		}
	}
	b.WriteString("<ASSISTANT>")
	return b.String(), nil
}
