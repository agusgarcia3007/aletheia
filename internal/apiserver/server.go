package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"aletheia/internal/model"
	"aletheia/internal/runner"
	"aletheia/internal/tokenizer"
)

const (
	DefaultAddr         = ":8080"
	DefaultCheckpoint   = "checkpoints/tiny-actions"
	DefaultMaxBodyBytes = int64(1 << 20)
)

type Options struct {
	Addr         string
	Checkpoint   string
	ModelName    string
	APIKey       string
	Auth         string
	MaxBodyBytes int64
}

type Server struct {
	opts      Options
	manifest  model.Manifest
	tokenizer *tokenizer.Tokenizer
	runner    runner.Runner
	created   int64
	nextID    atomic.Uint64
}

func New(opts Options) (*Server, error) {
	opts = normalizeOptions(opts)
	if opts.Auth != "bearer" && opts.Auth != "none" {
		return nil, fmt.Errorf("auth must be bearer or none")
	}
	if opts.Auth != "none" && strings.TrimSpace(opts.APIKey) == "" {
		return nil, fmt.Errorf("ALETHEIA_API_KEY or --api-key is required unless --auth none")
	}
	tok := tokenizer.New()
	m, manifest, err := model.Load(opts.Checkpoint, tok.VocabSize())
	if err != nil {
		return nil, fmt.Errorf("load checkpoint %s: %w", opts.Checkpoint, err)
	}
	if opts.ModelName == "" {
		opts.ModelName = manifest.Config.Name
	}
	return &Server{
		opts:      opts,
		manifest:  manifest,
		tokenizer: tok,
		runner:    runner.New(m, tok),
		created:   time.Now().Unix(),
	}, nil
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	server := &http.Server{
		Addr:    s.opts.Addr,
		Handler: s.Handler(),
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		return ctx.Err()
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /v1/models", s.withAuth(s.handleModels))
	mux.HandleFunc("POST /v1/chat/completions", s.withAuth(s.handleChatCompletions))
	mux.HandleFunc("POST /v1/completions", s.withAuth(s.handleCompletions))
	return mux
}

func (s *Server) ModelName() string {
	return s.opts.ModelName
}

func (s *Server) Addr() string {
	return s.opts.Addr
}

func normalizeOptions(opts Options) Options {
	if opts.Addr == "" {
		opts.Addr = DefaultAddr
	}
	if opts.Checkpoint == "" {
		opts.Checkpoint = DefaultCheckpoint
	}
	if opts.Auth == "" {
		opts.Auth = "bearer"
	}
	if opts.MaxBodyBytes <= 0 {
		opts.MaxBodyBytes = DefaultMaxBodyBytes
	}
	return opts
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"model_loaded": true,
		"model":        s.opts.ModelName,
	})
}

func (s *Server) handleModels(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data": []map[string]any{
			{
				"id":       s.opts.ModelName,
				"object":   "model",
				"created":  s.created,
				"owned_by": "aletheia",
			},
		},
	})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req chatCompletionRequest
	if !s.decodeRequest(w, r, &req) {
		return
	}
	if err := s.validateModel(req.Model); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "model", "model_not_found", err.Error())
		return
	}
	if req.Stream {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "stream", "unsupported_parameter", "streaming is not supported by this Aletheia server")
		return
	}
	prompt, err := chatPrompt(req.Messages)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "messages", "invalid_messages", err.Error())
		return
	}
	generated, usage, err := s.generate(prompt, generationOptions{
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		TopK:        req.TopK,
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "", "generation_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      s.id("chatcmpl"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   s.opts.ModelName,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": generated,
				},
				"finish_reason": "stop",
			},
		},
		"usage": usage,
	})
}

func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	var req completionRequest
	if !s.decodeRequest(w, r, &req) {
		return
	}
	if err := s.validateModel(req.Model); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "model", "model_not_found", err.Error())
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "prompt", "missing_prompt", "prompt is required")
		return
	}
	generated, usage, err := s.generate(req.Prompt, generationOptions{
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		TopK:        req.TopK,
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "", "generation_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":      s.id("cmpl"),
		"object":  "text_completion",
		"created": time.Now().Unix(),
		"model":   s.opts.ModelName,
		"choices": []map[string]any{
			{
				"text":          generated,
				"index":         0,
				"logprobs":      nil,
				"finish_reason": "stop",
			},
		},
		"usage": usage,
	})
}

func (s *Server) decodeRequest(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, s.opts.MaxBodyBytes)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		status := http.StatusBadRequest
		code := "invalid_json"
		if strings.Contains(err.Error(), "request body too large") {
			status = http.StatusRequestEntityTooLarge
			code = "body_too_large"
		}
		writeAPIError(w, status, "invalid_request_error", "", code, err.Error())
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "", "invalid_json", "request body must contain a single JSON object")
		return false
	}
	return true
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.opts.Auth == "none" {
			next(w, r)
			return
		}
		got := strings.TrimSpace(r.Header.Get("Authorization"))
		want := "Bearer " + s.opts.APIKey
		if got != want {
			writeAPIError(w, http.StatusUnauthorized, "authentication_error", "", "invalid_api_key", "missing or invalid bearer token")
			return
		}
		next(w, r)
	}
}

func (s *Server) validateModel(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("model is required")
	}
	if name != s.opts.ModelName {
		return fmt.Errorf("model %q is not served by this Aletheia instance", name)
	}
	return nil
}

func (s *Server) generate(prompt string, opts generationOptions) (string, map[string]int, error) {
	promptTokens := s.tokenizer.Encode(prompt)
	if len(promptTokens) == 0 {
		return "", nil, fmt.Errorf("prompt produced no tokens")
	}
	eos, _ := s.tokenizer.ID("<EOS>")
	actRespond, _ := s.tokenizer.ID("<ACT_RESPOND>")
	tokens, err := s.runner.Generate(prompt, runner.Options{
		MaxTokens:   intDefault(opts.MaxTokens, 32),
		TopK:        intDefault(opts.TopK, 0),
		TopP:        floatDefault(opts.TopP, 1),
		Temperature: floatDefault(opts.Temperature, 0),
		StopTokens:  []int{eos, actRespond},
	})
	if err != nil {
		return "", nil, err
	}
	generatedTokens := tokens[len(promptTokens):]
	text, err := s.tokenizer.Decode(generatedTokens)
	if err != nil {
		return "", nil, err
	}
	return text, map[string]int{
		"prompt_tokens":     len(promptTokens),
		"completion_tokens": len(generatedTokens),
		"total_tokens":      len(tokens),
	}, nil
}

func (s *Server) id(prefix string) string {
	next := s.nextID.Add(1)
	return fmt.Sprintf("%s-%d-%d", prefix, time.Now().UnixNano(), next)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeAPIError(w http.ResponseWriter, status int, typ string, param string, code string, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    typ,
			"param":   nullableString(param),
			"code":    code,
		},
	})
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func intDefault(value *int, fallback int) int {
	if value == nil || *value <= 0 {
		return fallback
	}
	return *value
}

func floatDefault(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

type generationOptions struct {
	MaxTokens   *int
	Temperature *float64
	TopP        *float64
	TopK        *int
}
