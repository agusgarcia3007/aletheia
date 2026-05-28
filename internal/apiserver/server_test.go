package apiserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"aletheia/internal/model"
	"aletheia/internal/tokenizer"
)

func TestHealthDoesNotRequireAuth(t *testing.T) {
	server := newTestServer(t, Options{APIKey: "secret"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"model_loaded":true`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestModelsRequiresBearerAuth(t *testing.T) {
	server := newTestServer(t, Options{APIKey: "secret"})

	noAuth := httptest.NewRecorder()
	server.Handler().ServeHTTP(noAuth, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if noAuth.Code != http.StatusUnauthorized {
		t.Fatalf("no auth status = %d body=%s", noAuth.Code, noAuth.Body.String())
	}

	withAuth := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer secret")
	server.Handler().ServeHTTP(withAuth, req)
	if withAuth.Code != http.StatusOK {
		t.Fatalf("auth status = %d body=%s", withAuth.Code, withAuth.Body.String())
	}
	if !strings.Contains(withAuth.Body.String(), `"id":"tiny-actions"`) {
		t.Fatalf("body = %s", withAuth.Body.String())
	}
}

func TestChatCompletionsReturnsOpenAIShape(t *testing.T) {
	server := newTestServer(t, Options{APIKey: "secret"})
	body := `{"model":"tiny-actions","messages":[{"role":"user","content":"fix failing go test"}],"max_tokens":8}`
	rec := serveJSON(t, server, "/v1/chat/completions", body, "secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage map[string]int `json:"usage"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Object != "chat.completion" || response.Model != "tiny-actions" {
		t.Fatalf("response = %+v", response)
	}
	if len(response.Choices) != 1 || response.Choices[0].Message.Role != "assistant" || response.Choices[0].Message.Content == "" {
		t.Fatalf("choices = %+v", response.Choices)
	}
	if response.Usage["prompt_tokens"] == 0 || response.Usage["completion_tokens"] == 0 {
		t.Fatalf("usage = %+v", response.Usage)
	}
}

func TestChatCompletionsRejectsStreamAndWrongModel(t *testing.T) {
	server := newTestServer(t, Options{APIKey: "secret"})
	stream := serveJSON(t, server, "/v1/chat/completions", `{"model":"tiny-actions","messages":[{"role":"user","content":"x"}],"stream":true}`, "secret")
	if stream.Code != http.StatusBadRequest || !strings.Contains(stream.Body.String(), "unsupported_parameter") {
		t.Fatalf("stream status = %d body=%s", stream.Code, stream.Body.String())
	}
	wrongModel := serveJSON(t, server, "/v1/chat/completions", `{"model":"other","messages":[{"role":"user","content":"x"}]}`, "secret")
	if wrongModel.Code != http.StatusBadRequest || !strings.Contains(wrongModel.Body.String(), "model_not_found") {
		t.Fatalf("wrong model status = %d body=%s", wrongModel.Code, wrongModel.Body.String())
	}
}

func TestCompletionsAndBodyLimit(t *testing.T) {
	server := newTestServer(t, Options{APIKey: "secret"})
	completion := serveJSON(t, server, "/v1/completions", `{"model":"tiny-actions","prompt":"<USER>x<ASSISTANT>","max_tokens":4}`, "secret")
	if completion.Code != http.StatusOK || !strings.Contains(completion.Body.String(), `"object":"text_completion"`) {
		t.Fatalf("completion status = %d body=%s", completion.Code, completion.Body.String())
	}

	limited := newTestServer(t, Options{APIKey: "secret", MaxBodyBytes: 12})
	tooLarge := serveJSON(t, limited, "/v1/chat/completions", `{"model":"tiny-actions","messages":[{"role":"user","content":"x"}]}`, "secret")
	if tooLarge.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too large status = %d body=%s", tooLarge.Code, tooLarge.Body.String())
	}
}

func newTestServer(t *testing.T, opts Options) *Server {
	t.Helper()
	checkpoint := filepath.Join(t.TempDir(), "checkpoint")
	tok := tokenizer.New()
	m, err := model.New(model.Config{
		Name:          "tiny-actions",
		VocabSize:     tok.VocabSize(),
		ContextLength: 64,
		NLayers:       1,
		NHeads:        2,
		DModel:        16,
		DFF:           32,
		Seed:          7,
	})
	if err != nil {
		t.Fatal(err)
	}
	respondID, ok := tok.ID("<ACT_RESPOND>")
	if !ok {
		t.Fatal("missing action token")
	}
	m.Bias[respondID] = 10
	if err := m.Save(checkpoint, tok.VocabSize(), 1, 0.1); err != nil {
		t.Fatal(err)
	}
	opts.Checkpoint = checkpoint
	if opts.APIKey == "" && opts.Auth == "" {
		opts.APIKey = "secret"
	}
	server, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func serveJSON(t *testing.T, server *Server, path string, body string, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	server.Handler().ServeHTTP(rec, req)
	return rec
}
