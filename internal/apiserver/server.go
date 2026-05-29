package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"aletheia/internal/memory"
	"aletheia/internal/model"
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
	Addr         string
	Checkpoint   string
	ModelName    string
	APIKey       string
	Auth         string
	MaxBodyBytes int64
	Store        *memory.Store
	Research     research.Options
}

type Server struct {
	opts      Options
	manifest  model.Manifest
	tokenizer *tokenizer.Tokenizer
	runner    runner.Runner
	store     *memory.Store
	research  research.Options
	created   int64
	nextID    atomic.Uint64
	requests  atomic.Uint64
	chats     atomic.Uint64
	queued    atomic.Uint64
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
		store:     opts.Store,
		research:  opts.Research,
		created:   time.Now().Unix(),
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
	s.requests.Add(1)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"model_loaded": true,
		"model":        s.opts.ModelName,
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
	s.requests.Add(1)
	s.chats.Add(1)
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
	if reply, ok := basicMikrosChatReply(s.opts.ModelName, req.Messages); ok {
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
						"content": reply,
					},
					"finish_reason": "stop",
				},
			},
			"usage": s.textUsage(prompt, reply),
		})
		return
	}
	query := strings.TrimSpace(lastUserMessage(req.Messages))
	if query != "" && s.store != nil {
		if answer, ok := s.completedResearchAnswer(r.Context(), query); ok {
			writeJSON(w, http.StatusOK, s.chatResponse(answer, s.textUsage(prompt, answer)))
			return
		}
		answer, err := (retriever.Retriever{Store: s.store}).Answer(r.Context(), query, retriever.SearchOptions{TopK: 5, MinConfidence: retriever.DefaultMinConfidence})
		if err == nil && answer.Verified {
			writeJSON(w, http.StatusOK, s.chatResponse(answer.Text+"\n\n"+formatCitations(answer.Citations), s.textUsage(prompt, answer.Text)))
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
				writeJSON(w, http.StatusOK, s.chatResponse(content, s.textUsage(prompt, content)))
				return
			}
		}
		if researchMode == "off" || !s.research.Enabled {
			content := "No tengo evidencia local suficiente para responder. La investigación web está deshabilitada."
			writeJSON(w, http.StatusOK, s.chatResponse(content, s.textUsage(prompt, content)))
			return
		}
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

func (s *Server) chatResponse(content string, usage map[string]int) map[string]any {
	return map[string]any{
		"id":      s.id("chatcmpl"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   s.opts.ModelName,
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
		sources, _ := s.store.WebSourcesByJob(ctx, job.ID)
		return formatResearchAnswer(job, sources), true
	}
	return "", false
}

func formatResearchAnswer(job memory.ResearchJob, sources []memory.WebSource) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(job.Answer))
	if len(sources) > 0 {
		b.WriteString("\n\nFuentes:\n")
		seen := map[string]bool{}
		written := 0
		for _, source := range sources {
			url := source.FinalURL
			if url == "" {
				url = source.URL
			}
			if url == "" || seen[url] {
				continue
			}
			seen[url] = true
			title := source.Title
			if title == "" {
				title = url
			}
			b.WriteString(fmt.Sprintf("- %s - %s\n", title, url))
			written++
			if written >= 5 {
				break
			}
		}
	}
	return strings.TrimSpace(b.String())
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
		strings.Contains(normalized, "codigo") ||
		strings.Contains(normalized, "code")
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
