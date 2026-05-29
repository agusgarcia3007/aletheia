package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"aletheia/internal/memory"
	"aletheia/internal/research"
	"aletheia/internal/retriever"
	"aletheia/internal/runner"
	"aletheia/internal/tokenizer"
)

const (
	DefaultAddr         = ":8080"
	DefaultCheckpoint   = "checkpoints/aletheia-mikros"
	DefaultMaxBodyBytes = int64(1 << 20)
)

type Options struct {
	Addr           string
	Checkpoint     string
	CheckpointsDir string
	ModelName      string
	APIKey         string
	Auth           string
	MaxBodyBytes   int64
	Store          *memory.Store
	Research       research.Options
}

type Server struct {
	opts         Options
	defaultModel string
	modelOrder   []string
	models       map[string]*servedModel
	tokenizer    *tokenizer.Tokenizer
	store        *memory.Store
	research     research.Options
	created      int64
	nextID       atomic.Uint64
	requests     atomic.Uint64
	chats        atomic.Uint64
	queued       atomic.Uint64
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
	models, order, defaultModel, err := loadModelRegistry(opts, tok)
	if err != nil {
		return nil, err
	}
	opts.ModelName = defaultModel
	return &Server{
		opts:         opts,
		defaultModel: defaultModel,
		modelOrder:   order,
		models:       models,
		tokenizer:    tok,
		store:        opts.Store,
		research:     opts.Research,
		created:      time.Now().Unix(),
	}, nil
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	if s.store != nil && s.research.Enabled && s.research.BackgroundJobsEnabled {
		worker := research.NewWorker(s.store, s.research)
		worker.Start(ctx, 2*time.Second)
	}
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
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /metrics", s.handleMetrics)
	mux.HandleFunc("GET /v1/models", s.withAuth(s.handleModels))
	mux.HandleFunc("POST /v1/chat/completions", s.withAuth(s.handleChatCompletions))
	mux.HandleFunc("POST /v1/completions", s.withAuth(s.handleCompletions))
	mux.HandleFunc("POST /v1/aletheia/research", s.withAuth(s.handleResearch))
	mux.HandleFunc("GET /v1/aletheia/research/", s.withAuth(s.handleResearchStatus))
	mux.HandleFunc("GET /v1/aletheia/jobs", s.withAuth(s.handleJobs))
	return mux
}

func (s *Server) ModelName() string {
	return s.defaultModel
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
	s.requests.Add(1)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"model_loaded": true,
		"model":        s.defaultModel,
		"models":       s.modelOrder,
	})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	s.requests.Add(1)
	if s.store != nil {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if err := s.store.Ping(ctx); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "not_ready",
				"error":  err.Error(),
			})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":            "ready",
		"model_loaded":      true,
		"models_loaded":     len(s.models),
		"memory_configured": s.store != nil,
		"research_enabled":  s.research.Enabled,
	})
}

func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	s.requests.Add(1)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	uptime := time.Now().Unix() - s.created
	_, _ = fmt.Fprintf(w, "aletheia_uptime_seconds %d\n", uptime)
	_, _ = fmt.Fprintf(w, "aletheia_requests_total %d\n", s.requests.Load())
	_, _ = fmt.Fprintf(w, "aletheia_chat_requests_total %d\n", s.chats.Load())
	_, _ = fmt.Fprintf(w, "aletheia_research_jobs_queued_total %d\n", s.queued.Load())
	_, _ = fmt.Fprintf(w, "aletheia_model_loaded 1\n")
	if s.store != nil {
		_, _ = fmt.Fprintf(w, "aletheia_memory_configured 1\n")
	} else {
		_, _ = fmt.Fprintf(w, "aletheia_memory_configured 0\n")
	}
}

func (s *Server) handleModels(w http.ResponseWriter, _ *http.Request) {
	s.requests.Add(1)
	publicModels := s.publicModelOrder()
	data := make([]map[string]any, 0, len(publicModels))
	for _, id := range publicModels {
		data = append(data, map[string]any{
			"id":       id,
			"object":   "model",
			"created":  s.created,
			"owned_by": "aletheia",
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   data,
	})
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	s.requests.Add(1)
	s.chats.Add(1)
	var req chatCompletionRequest
	if !s.decodeRequest(w, r, &req) {
		return
	}
	respond := func(response map[string]any) {
		s.writeChatCompletion(w, req.Stream, response)
	}
	served, routed, err := s.routeModel(req.Model, req.Messages, req.Tools)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "model", "model_not_found", err.Error())
		return
	}
	prompt, err := chatPrompt(req.Messages)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "messages", "invalid_messages", err.Error())
		return
	}
	maxTokens := req.MaxTokens
	if maxTokens == nil {
		maxTokens = req.MaxCompletionTokens
	}
	if toolCall, ok := s.codingToolCall(served.ID, req); ok {
		respond(s.toolCallResponse(responseModelID(req.Model, served.ID), toolCall, s.textUsage(prompt, toolCall.Function.Name)))
		return
	}
	if reply, ok := policyReply(served.ID, req.Messages); ok {
		respond(s.chatResponse(responseModelID(req.Model, served.ID), reply, s.textUsage(prompt, reply)))
		return
	}
	if reply, ok := codingKnowledgeReply(req.Messages); ok {
		respond(s.chatResponse(responseModelID(req.Model, served.ID), reply, s.textUsage(prompt, reply)))
		return
	}
	query := strings.TrimSpace(lastUserMessage(req.Messages))
	if query != "" && isChatActionRequest(query) {
		generated, usage, err := s.generate(served, prompt, generationOptions{
			MaxTokens:   maxTokens,
			Temperature: req.Temperature,
			TopP:        req.TopP,
			TopK:        req.TopK,
		})
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "server_error", "", "generation_failed", err.Error())
			return
		}
		respond(map[string]any{
			"id":      s.id("chatcmpl"),
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   responseModelID(req.Model, served.ID),
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
		return
	}
	if query != "" && isUnsupportedFutureOutcomeQuestion(query) {
		content := "No tengo evidencia suficiente para responder eso como hecho verificado. La pregunta pide un resultado futuro o no confirmado; necesito fuentes directas y actuales antes de afirmarlo."
		respond(s.chatResponse(responseModelID(req.Model, served.ID), content, s.textUsage(prompt, content)))
		return
	}
	if query != "" && !isResearchableQuestion(query) {
		if reply, ok := trainedExampleReply(served, req.Messages); ok {
			respond(s.chatResponse(responseModelID(req.Model, served.ID), reply, s.textUsage(prompt, reply)))
			return
		}
		if served.ID == mikrosModelName && served.Manifest.Step == 0 {
			if reply, ok := basicMikrosChatReply(served.ID, req.Messages); ok {
				respond(s.chatResponse(responseModelID(req.Model, served.ID), reply, s.textUsage(prompt, reply)))
				return
			}
		}
	}
	if query != "" && s.store != nil {
		if answer, ok := s.completedResearchAnswer(r.Context(), query); ok {
			respond(s.chatResponse(responseModelID(req.Model, served.ID), answer, s.textUsage(prompt, answer)))
			return
		}
		answer, err := (retriever.Retriever{Store: s.store}).Answer(r.Context(), query, retriever.SearchOptions{TopK: 5, MinConfidence: retriever.DefaultMinConfidence})
		if err == nil && answer.Verified {
			respond(s.chatResponse(responseModelID(req.Model, served.ID), answer.Text+"\n\n"+formatCitations(answer.Citations), s.textUsage(prompt, answer.Text)))
			return
		}
		researchMode := "background"
		if req.Aletheia != nil && strings.TrimSpace(req.Aletheia.Research) != "" {
			researchMode = strings.TrimSpace(req.Aletheia.Research)
		}
		if shouldResearch(query, researchMode, s.research) {
			job, err := s.enqueueResearch(r.Context(), query, researchMode, 0)
			if err == nil {
				content := fmt.Sprintf("No tengo evidencia local suficiente. Inicié una investigación en background con job_id=%s. Consultá el resultado en unos segundos o repetí la pregunta luego.", job.ID)
				respond(s.chatResponse(responseModelID(req.Model, served.ID), content, s.textUsage(prompt, content)))
				return
			}
		}
		if researchMode == "off" || !s.research.Enabled {
			content := "No tengo evidencia local suficiente para responder. La investigación web está deshabilitada."
			respond(s.chatResponse(responseModelID(req.Model, served.ID), content, s.textUsage(prompt, content)))
			return
		}
	}
	if routed && served.ID == hephaestusModelName {
		content := "Puedo ayudar con codigo, pero necesito un poco mas de contexto: lenguaje, objetivo, entrada/salida esperada y restricciones. No voy a buscar fuentes web para inventar una respuesta."
		respond(s.chatResponse(responseModelID(req.Model, served.ID), content, s.textUsage(prompt, content)))
		return
	}
	generated, usage, err := s.generate(served, prompt, generationOptions{
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		TopK:        req.TopK,
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "", "generation_failed", err.Error())
		return
	}
	respond(map[string]any{
		"id":      s.id("chatcmpl"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   responseModelID(req.Model, served.ID),
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

func responseModelID(requested string, served string) string {
	if requested == mikrosModelName {
		return mikrosModelName
	}
	return served
}

func (s *Server) chatResponse(modelID string, content string, usage map[string]int) map[string]any {
	return map[string]any{
		"id":      s.id("chatcmpl"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   modelID,
		"choices": []map[string]any{
			{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
		"usage": usage,
	}
}

func (s *Server) writeChatCompletion(w http.ResponseWriter, stream bool, response map[string]any) {
	if !stream {
		writeJSON(w, http.StatusOK, response)
		return
	}
	writeChatCompletionStream(w, response)
}

func writeChatCompletionStream(w http.ResponseWriter, response map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	id, _ := response["id"].(string)
	modelID, _ := response["model"].(string)
	created, _ := response["created"].(int64)
	if created == 0 {
		created = time.Now().Unix()
	}
	finishReason := "stop"
	message := map[string]any{"role": "assistant", "content": ""}
	if choices, ok := response["choices"].([]map[string]any); ok && len(choices) > 0 {
		if reason, ok := choices[0]["finish_reason"].(string); ok && reason != "" {
			finishReason = reason
		}
		if got, ok := choices[0]["message"].(map[string]any); ok {
			message = got
		}
	}
	writeSSEChunk(w, flusher, map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   modelID,
		"choices": []map[string]any{{
			"index":         0,
			"delta":         map[string]any{"role": "assistant"},
			"finish_reason": nil,
		}},
	})
	if toolCalls, ok := message["tool_calls"].([]assistantToolCall); ok && len(toolCalls) > 0 {
		chunkCalls := make([]map[string]any, 0, len(toolCalls))
		for i, call := range toolCalls {
			chunkCalls = append(chunkCalls, map[string]any{
				"index": i,
				"id":    call.ID,
				"type":  call.Type,
				"function": map[string]any{
					"name":      call.Function.Name,
					"arguments": call.Function.Arguments,
				},
			})
		}
		writeSSEChunk(w, flusher, map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   modelID,
			"choices": []map[string]any{{
				"index":         0,
				"delta":         map[string]any{"tool_calls": chunkCalls},
				"finish_reason": nil,
			}},
		})
	} else if content, ok := message["content"].(string); ok && content != "" {
		writeSSEChunk(w, flusher, map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   modelID,
			"choices": []map[string]any{{
				"index":         0,
				"delta":         map[string]any{"content": content},
				"finish_reason": nil,
			}},
		})
	}
	writeSSEChunk(w, flusher, map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   modelID,
		"choices": []map[string]any{{
			"index":         0,
			"delta":         map[string]any{},
			"finish_reason": finishReason,
		}},
	})
	if usage, ok := response["usage"].(map[string]int); ok && len(usage) > 0 {
		writeSSEChunk(w, flusher, map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   modelID,
			"choices": []map[string]any{},
			"usage":   usage,
		})
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

func writeSSEChunk(w io.Writer, flusher http.Flusher, value map[string]any) {
	raw, err := json.Marshal(value)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", raw)
	if flusher != nil {
		flusher.Flush()
	}
}

func (s *Server) handleResearch(w http.ResponseWriter, r *http.Request) {
	s.requests.Add(1)
	var req researchRequest
	if !s.decodeRequest(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Query) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "query", "missing_query", "query is required")
		return
	}
	mode := req.Mode
	if mode == "" {
		mode = "background"
	}
	job, err := s.enqueueResearch(r.Context(), req.Query, mode, req.MaxSources)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "research", "research_unavailable", err.Error())
		return
	}
	if mode == "sync" {
		worker := research.NewWorker(s.store, s.research)
		result, err := worker.RunJob(r.Context(), job)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "server_error", "research", "research_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, researchResultJSON(job.ID, result))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "queued",
		"job_id":  job.ID,
		"message": "No tengo evidencia local suficiente. Inicié una investigación en background.",
	})
}

func (s *Server) handleResearchStatus(w http.ResponseWriter, r *http.Request) {
	s.requests.Add(1)
	id := strings.TrimPrefix(r.URL.Path, "/v1/aletheia/research/")
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "job", "missing_job", "job id is required")
		return
	}
	if s.store == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "research", "research_unavailable", "research store is not configured")
		return
	}
	job, ok, err := s.store.ResearchJob(r.Context(), id)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "research", "research_failed", err.Error())
		return
	}
	if !ok {
		writeAPIError(w, http.StatusNotFound, "invalid_request_error", "job", "job_not_found", "research job not found")
		return
	}
	sources, _ := s.store.WebSourcesByJob(r.Context(), id)
	claims, _ := s.store.WebClaimsByJob(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         job.Status,
		"job_id":         job.ID,
		"query":          job.Query,
		"sources_stored": len(sources),
		"claims_stored":  len(claims),
		"confidence":     job.Confidence,
		"answer":         job.Answer,
		"error":          job.Error,
		"sources":        sources,
	})
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	s.requests.Add(1)
	if s.store == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "research", "research_unavailable", "research store is not configured")
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	includeFailed := r.URL.Query().Get("include_failed") == "true"
	jobs, err := s.store.ListResearchJobs(r.Context(), limit)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "server_error", "research", "research_failed", err.Error())
		return
	}
	if !includeFailed {
		filtered := jobs[:0]
		for _, job := range jobs {
			if job.Status != "failed" {
				filtered = append(filtered, job)
			}
		}
		jobs = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (s *Server) enqueueResearch(ctx context.Context, query string, mode string, maxSources int) (memory.ResearchJob, error) {
	if s.store == nil {
		return memory.ResearchJob{}, fmt.Errorf("research store is not configured")
	}
	if !s.research.Enabled {
		return memory.ResearchJob{}, fmt.Errorf("research is disabled")
	}
	if mode != "sync" && mode != "background" {
		mode = "background"
	}
	if maxSources <= 0 {
		maxSources = s.research.MaxSources
	}
	job, err := s.store.CreateResearchJob(ctx, memory.ResearchJob{
		Query:      query,
		Status:     "queued",
		Mode:       mode,
		MaxSources: maxSources,
	})
	if err == nil {
		s.queued.Add(1)
	}
	return job, err
}

func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	var req completionRequest
	if !s.decodeRequest(w, r, &req) {
		return
	}
	served, ok := s.model(req.Model)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "model", "model_not_found", fmt.Sprintf("model %q is not served by this Aletheia instance", req.Model))
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request_error", "prompt", "missing_prompt", "prompt is required")
		return
	}
	generated, usage, err := s.generate(served, req.Prompt, generationOptions{
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
		"model":   served.ID,
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

func (s *Server) generate(served *servedModel, prompt string, opts generationOptions) (string, map[string]int, error) {
	promptTokens := s.tokenizer.Encode(prompt)
	if len(promptTokens) == 0 {
		return "", nil, fmt.Errorf("prompt produced no tokens")
	}
	eos, _ := s.tokenizer.ID("<EOS>")
	actRespond, _ := s.tokenizer.ID("<ACT_RESPOND>")
	tokens, err := served.Runner.Generate(prompt, runner.Options{
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
	generatedTokens = trimStopTokens(generatedTokens, eos, actRespond)
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

func (s *Server) textUsage(prompt string, completion string) map[string]int {
	promptTokens := s.tokenizer.Encode(prompt)
	completionTokens := s.tokenizer.Encode(completion)
	return map[string]int{
		"prompt_tokens":     len(promptTokens),
		"completion_tokens": len(completionTokens),
		"total_tokens":      len(promptTokens) + len(completionTokens),
	}
}

func shouldResearch(query string, mode string, opts research.Options) bool {
	if mode == "off" || !opts.Enabled {
		return false
	}
	if isChatActionRequest(query) || isUnsupportedFutureOutcomeQuestion(query) {
		return false
	}
	if mode == "sync" || mode == "background" {
		return !isCodingTask(query)
	}
	return opts.AutoOnKnowledgeGap && !isCodingTask(query)
}

func (s *Server) completedResearchAnswer(ctx context.Context, query string) (string, bool) {
	if s.store == nil {
		return "", false
	}
	jobs, err := s.store.ListResearchJobs(ctx, 50)
	if err != nil {
		return "", false
	}
	queryTokens := meaningfulChatTokens(query)
	if len(queryTokens) == 0 {
		return "", false
	}
	for _, job := range jobs {
		if job.Status != "completed" || strings.TrimSpace(job.Answer) == "" || job.Confidence < s.research.MinTrustScore {
			continue
		}
		if meaningfulOverlap(queryTokens, meaningfulChatTokens(job.Query+" "+job.Answer)) < requiredMeaningfulOverlap(len(queryTokens)) {
			continue
		}
		if !researchAnswerMatchesQuery(query, job) {
			continue
		}
		sources, _ := s.store.WebSourcesByJob(ctx, job.ID)
		claims, _ := s.store.WebClaimsByJob(ctx, job.ID)
		answer, ok := formatResearchAnswer(query, job, sources, claims)
		if !ok {
			continue
		}
		return answer, true
	}
	return "", false
}

func formatResearchAnswer(query string, job memory.ResearchJob, sources []memory.WebSource, claims []memory.WebClaim) (string, bool) {
	answer := bestPublicResearchAnswer(query, job, sources, claims)
	if answer == "" {
		return "", false
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(answer))
	if len(sources) > 0 {
		b.WriteString("\n\nFuentes:\n")
		seen := map[string]bool{}
		written := 0
		for _, source := range sources {
			if !publicWebSourceAllowed(source) {
				continue
			}
			url := source.FinalURL
			if url == "" {
				url = source.URL
			}
			if url == "" || seen[url] {
				continue
			}
			seen[url] = true
			title := publicSourceTitle(source.Title)
			if title == "" {
				b.WriteString(fmt.Sprintf("- %s\n", url))
			} else {
				b.WriteString(fmt.Sprintf("- %s - %s\n", title, url))
			}
			written++
			if written >= 5 {
				break
			}
		}
	}
	return strings.TrimSpace(b.String()), true
}

func publicSourceTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" || looksLikeTitleOnly(title) {
		return ""
	}
	return title
}

type researchCandidate struct {
	text  string
	score int
}

func bestPublicResearchAnswer(query string, job memory.ResearchJob, sources []memory.WebSource, claims []memory.WebClaim) string {
	queryTokens := meaningfulChatTokens(query)
	sourceTitles := map[string]bool{}
	for _, source := range sources {
		if publicWebSourceAllowed(source) {
			sourceTitles[normalizeBasicChat(source.Title)] = true
		}
	}
	var best researchCandidate
	for _, claim := range claims {
		best = bestResearchCandidate(query, queryTokens, sourceTitles, best, claim.Claim)
	}
	for _, source := range sources {
		if !publicWebSourceAllowed(source) {
			continue
		}
		best = bestResearchCandidate(query, queryTokens, sourceTitles, best, source.Snippet)
	}
	if best.text != "" {
		return withEvidenceStatus(best.text, evidenceStatusFromAnswer(job.Answer))
	}
	answer := strings.TrimSpace(job.Answer)
	answer = cleanPublicResearchText(query, answer)
	if answer == "" || looksLikeTitleOnly(answer) || !researchAnswerMatchesQuery(query, job) {
		return ""
	}
	return answer
}

func bestResearchCandidate(query string, queryTokens map[string]bool, sourceTitles map[string]bool, best researchCandidate, candidate string) researchCandidate {
	text := strings.TrimSpace(candidate)
	normalized := normalizeBasicChat(text)
	if text == "" || sourceTitles[normalized] || looksLikeTitleOnly(text) {
		return best
	}
	score := meaningfulOverlap(queryTokens, meaningfulChatTokens(text))
	if score < requiredMeaningfulOverlap(len(queryTokens)) {
		return best
	}
	text = cleanPublicResearchText(query, text)
	if text == "" || looksLikeTitleOnly(text) {
		return best
	}
	if natural := naturalizeResearchAnswer(query, text); natural != "" {
		text = natural
	}
	if score > best.score || best.text == "" {
		best = researchCandidate{text: text, score: score}
	}
	return best
}

func cleanPublicResearchText(query string, text string) string {
	text = stripEvidenceLines(text)
	text = stripPageChrome(text)
	queryTokens := meaningfulChatTokens(query)
	best := ""
	bestScore := 0
	for _, sentence := range splitPublicSentences(text) {
		sentence = strings.TrimSpace(sentence)
		if len([]rune(sentence)) < 35 || looksLikeTitleOnly(sentence) {
			continue
		}
		score := meaningfulOverlap(queryTokens, meaningfulChatTokens(sentence))
		if score > bestScore {
			best = sentence
			bestScore = score
		}
	}
	if best == "" {
		best = text
	}
	best = trimBeforePublicCoreTerm(query, best)
	if len([]rune(best)) > 420 {
		runes := []rune(best)
		best = string(runes[:420])
		if idx := strings.LastIndex(best, " "); idx > 180 {
			best = best[:idx]
		}
		best += "."
	}
	return strings.TrimSpace(best)
}

func naturalizeResearchAnswer(query string, text string) string {
	normalized := normalizeBasicChat(query)
	if hasAny(normalized, "quien gano", "quien ganó", "who won") {
		cleaned := strings.TrimSpace(text)
		if cleaned == "" || looksLikeTitleOnly(cleaned) {
			return ""
		}
		if hasAny(normalizeBasicChat(cleaned), "gano", "ganó", "vencio", "venció", "campeon", "campeón", "won", "defeated") {
			return sentenceCase(cleaned)
		}
	}
	return ""
}

func sentenceCase(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	runes := []rune(text)
	if runes[0] >= 'a' && runes[0] <= 'z' {
		runes[0] = runes[0] - ('a' - 'A')
	}
	out := string(runes)
	if !strings.HasSuffix(out, ".") && !strings.HasSuffix(out, "!") && !strings.HasSuffix(out, "?") {
		out += "."
	}
	return out
}

func stripEvidenceLines(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		lower := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(lower, "evidence status:") || strings.HasPrefix(lower, "source:") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, " ")
}

func stripPageChrome(text string) string {
	replacements := []string{
		"WhatsApp", "Twitter", "Facebook", "Linkedin", "LinkedIn", "Telegram",
		"Copiar URL", "Beloud", "Lo Ultimo", "Lo Último",
	}
	for _, value := range replacements {
		text = strings.ReplaceAll(text, value, " ")
	}
	return strings.Join(strings.Fields(text), " ")
}

func splitPublicSentences(text string) []string {
	parts := regexp.MustCompile(`[.!?]\s+`).Split(text, -1)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func trimBeforePublicCoreTerm(query string, text string) string {
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "whatsapp") &&
		!strings.Contains(lower, "copiar") &&
		!strings.Contains(lower, "fotografia") &&
		!strings.Contains(lower, "redaccion") &&
		!strings.Contains(lower, "publicado") &&
		!strings.Contains(lower, "lo ultimo") {
		return text
	}
	for token := range meaningfulChatTokens(query) {
		if len(token) < 4 {
			continue
		}
		if idx := strings.Index(lower, token); idx > 24 && idx < len(text) {
			best := idx
			for other := range meaningfulChatTokens(query) {
				if len(other) < 4 {
					continue
				}
				if otherIdx := strings.Index(lower, other); otherIdx > 24 && otherIdx < best {
					best = otherIdx
				}
			}
			return strings.TrimSpace(text[best:])
		}
	}
	return text
}

func withEvidenceStatus(text string, status string) string {
	lower := strings.ToLower(text)
	if status == "" || strings.Contains(lower, "evidence status:") || strings.Contains(lower, "estado de evidencia:") {
		return text
	}
	return fmt.Sprintf("%s\n\nEstado de evidencia: %s", text, status)
}

func evidenceStatusFromAnswer(answer string) string {
	for _, line := range strings.Split(answer, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "evidence status:") {
			return strings.TrimSpace(line[len("Evidence status:"):])
		}
	}
	return research.StatusWebSupported
}

func looksLikeTitleOnly(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}
	if isQuestionTitle(text) {
		return true
	}
	if strings.Contains(text, "\n\n") || len([]rune(text)) > 220 {
		return false
	}
	terminal := strings.HasSuffix(text, ".") || strings.HasSuffix(text, "!") || strings.HasSuffix(text, "?")
	return !terminal || strings.Contains(strings.ToLower(text), " - wikipedia") || strings.Contains(strings.ToLower(text), " | ")
}

func isQuestionTitle(text string) bool {
	normalized := normalizeBasicChat(text)
	return strings.HasPrefix(normalized, "quien ") ||
		strings.HasPrefix(normalized, "que ") ||
		strings.HasPrefix(normalized, "como ") ||
		strings.HasPrefix(normalized, "what ") ||
		strings.HasPrefix(normalized, "who ") ||
		strings.HasPrefix(normalized, "how ")
}

func publicWebSourceAllowed(source memory.WebSource) bool {
	title := strings.ToLower(strings.TrimSpace(source.Title))
	if title == "blocked" || strings.Contains(title, "blocked") {
		return false
	}
	raw := source.FinalURL
	if raw == "" {
		raw = source.URL
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	blocked := []string{
		"facebook.com", "instagram.com", "tiktok.com", "x.com", "twitter.com",
		"reddit.com", "turiver.com",
	}
	for _, domain := range blocked {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return false
		}
	}
	return raw != ""
}

func researchAnswerMatchesQuery(query string, job memory.ResearchJob) bool {
	if isUnsupportedFutureOutcomeQuestion(query) {
		return false
	}
	years := fourDigitYears(query)
	if len(years) == 0 {
		return true
	}
	answer := job.Answer
	for _, year := range years {
		if !strings.Contains(answer, year) {
			return false
		}
	}
	return true
}

func meaningfulChatTokens(text string) map[string]bool {
	normalized := normalizeBasicChat(text)
	out := map[string]bool{}
	for _, token := range strings.Fields(normalized) {
		if len([]rune(token)) <= 2 || chatStopWords[token] {
			continue
		}
		out[token] = true
	}
	return out
}

func meaningfulOverlap(left map[string]bool, right map[string]bool) int {
	overlap := 0
	for token := range left {
		if right[token] {
			overlap++
		}
	}
	return overlap
}

func requiredMeaningfulOverlap(tokenCount int) int {
	if tokenCount <= 2 {
		return 1
	}
	return 2
}

func isChatActionRequest(query string) bool {
	normalized := normalizeBasicChat(query)
	return isRepoRepairRequest(normalized) || isCodeGenerationRequest(normalized) || isProgrammingHelpRequest(normalized)
}

var chatStopWords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "by": true, "for": true, "from": true,
	"how": true, "is": true, "it": true, "of": true, "on": true, "or": true, "the": true, "to": true,
	"what": true, "when": true, "where": true, "who": true, "why": true, "with": true,
	"como": true, "con": true, "cual": true, "cuando": true, "de": true, "del": true, "donde": true,
	"el": true, "en": true, "es": true, "esa": true, "ese": true, "eso": true, "esta": true, "este": true,
	"fue": true, "la": true, "las": true, "lo": true, "los": true, "para": true, "por": true, "que": true,
	"quien": true, "son": true, "un": true, "una": true, "y": true,
}

func isCodingTask(query string) bool {
	normalized := normalizeBasicChat(query)
	return strings.Contains(normalized, "fix ") ||
		strings.Contains(normalized, "arregla") ||
		strings.Contains(normalized, "repo") ||
		strings.Contains(normalized, "go test") ||
		strings.Contains(normalized, "patch") ||
		isProgrammingHelpRequest(normalized) ||
		strings.Contains(normalized, "codigo") ||
		strings.Contains(normalized, "code")
}

func isToolUseRequest(query string) bool {
	normalized := normalizeBasicChat(query)
	return isCodingTask(normalized) || hasAny(normalized, "read", "search", "grep", "inspect", "list", "run", "test", "build", "edit", "patch", "write")
}

func isResearchableQuestion(query string) bool {
	normalized := normalizeBasicChat(query)
	if normalized == "" || isChatActionRequest(normalized) || isUnsupportedFutureOutcomeQuestion(normalized) {
		return false
	}
	if len(detectCodingLanguages(normalized)) > 0 {
		return false
	}
	if hasAny(normalized, "hola", "gracias", "chau", "adios", "hello", "thanks", "help", "ayuda") {
		return false
	}
	return hasAny(normalized,
		"que fue", "que es", "quien fue", "quien es", "cuando", "donde", "por que",
		"what is", "what was", "who is", "who was", "when", "where", "why",
		"historia", "guerra", "pais", "empresa", "protocolo", "actual", "latest",
	)
}

var yearRe = regexp.MustCompile(`\b(19|20|21)\d{2}\b`)

func fourDigitYears(text string) []string {
	matches := yearRe.FindAllString(text, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var years []string
	for _, year := range matches {
		if !seen[year] {
			seen[year] = true
			years = append(years, year)
		}
	}
	return years
}

func isUnsupportedFutureOutcomeQuestion(query string) bool {
	normalized := normalizeBasicChat(query)
	if !hasAny(normalized, "gano", "ganador", "campeon", "resultado", "winner", "won", "champion", "result") {
		return false
	}
	for _, year := range fourDigitYears(normalized) {
		value, err := strconv.Atoi(year)
		if err == nil && value > time.Now().Year() {
			return true
		}
	}
	return false
}

func formatCitations(citations []retriever.Citation) string {
	if len(citations) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Fuentes:\n")
	for _, citation := range citations {
		b.WriteString(fmt.Sprintf("- %s chunk=%d score=%.4f\n", citation.Path, citation.ChunkID, citation.Score))
	}
	return strings.TrimSpace(b.String())
}

func researchResultJSON(jobID string, result research.ResearchResult) map[string]any {
	return map[string]any{
		"status":         "completed",
		"job_id":         jobID,
		"sources_found":  len(result.Sources),
		"sources_stored": result.SourcesStored,
		"chunks_stored":  result.ChunksStored,
		"claims_stored":  result.ClaimsStored,
		"confidence":     result.Confidence,
		"answer":         result.Answer,
	}
}

func trimStopTokens(tokens []int, stopTokens ...int) []int {
	for len(tokens) > 0 {
		last := tokens[len(tokens)-1]
		found := false
		for _, stop := range stopTokens {
			if last == stop {
				found = true
				break
			}
		}
		if !found {
			break
		}
		tokens = tokens[:len(tokens)-1]
	}
	return tokens
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
