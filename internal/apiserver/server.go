package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"aletheia/internal/answerer"
	"aletheia/internal/memory"
	"aletheia/internal/research"
	"aletheia/internal/retriever"
	"aletheia/internal/router"
	"aletheia/internal/runner"
	"aletheia/internal/tokenizer"
)

const (
	DefaultAddr            = ":8080"
	DefaultCheckpoint      = "checkpoints/aletheia-mikros"
	DefaultMaxBodyBytes    = int64(1 << 20)
	DefaultMaxOutputTokens = 256
)

type Options struct {
	Addr             string
	Checkpoint       string
	CheckpointsDir   string
	ModelName        string
	APIKey           string
	Auth             string
	MaxBodyBytes     int64
	Store            *memory.Store
	Research         research.Options
	RouterCheckpoint string
	Router           router.Router
	KnowledgePath    string
	// AdminToken, when set, enables the token-gated /v1/aletheia/admin/* endpoints
	// that drive the self-improvement pipeline (seed -> harvest -> train) over
	// HTTP, for VPS deployments without easy shell access. Empty = disabled.
	AdminToken string
	// DataDir is the writable+persistent directory (a mounted volume) where the
	// admin pipeline writes harvested datasets and trained checkpoints, so they
	// survive redeploys. Empty falls back to a relative "data" dir.
	DataDir string
}

type Server struct {
	opts         Options
	defaultModel string
	modelOrder   []string
	models       map[string]*servedModel
	tokenizer    *tokenizer.Tokenizer
	store        *memory.Store
	research     research.Options
	chatRouter   router.Router
	answerers    answerer.Composite
	retriever    retriever.Retriever
	created      int64
	nextID       atomic.Uint64
	requests     atomic.Uint64
	chats        atomic.Uint64
	queued       atomic.Uint64
	experts      map[string]*atomic.Uint64
	adminState   *adminPipeline
}

// expertNames are the sparse capability "experts": exactly one handles each chat
// request. Tracking the distribution is the observability side of Aletheia's
// architectural mixture-of-experts.
var expertNames = []string{
	"smalltalk", "math", "coding", "translation", "abstain", "tool_boundary",
	"tool_call", "nonsense", "ambiguous", "code_generation", "future_abstain",
	"research_learn", "memory_verified", "factual_abstain", "honest_fallback", "generated",
}

func (s *Server) expert(name string) {
	if c := s.experts[name]; c != nil {
		c.Add(1)
	}
}

// recordRouterExample persists a verified routing label produced by a
// deterministic guardrail/answerer. These become self-improvement signal for
// the router via `aletheia learn`. Deduped by normalized text so repeated
// queries do not bloat memory.
func (s *Server) recordRouterExample(query string, intent router.Intent) {
	if s.store == nil || strings.TrimSpace(query) == "" {
		return
	}
	if intent == "" || intent == router.IntentUnknown {
		return
	}
	payload, err := json.Marshal(map[string]string{"text": query, "intent": string(intent)})
	if err != nil {
		return
	}
	_, _ = s.store.EnsureNode(context.Background(), "router_example", "router:"+router.Normalize(query), string(payload))
}

func expertForIntent(intent router.Intent) string {
	switch intent {
	case router.IntentSmalltalk:
		return "smalltalk"
	case router.IntentMath:
		return "math"
	case router.IntentCodingHelp, router.IntentCodeGeneration:
		return "coding"
	case router.IntentTranslation:
		return "translation"
	case router.IntentRepoAgent:
		return "tool_boundary"
	default:
		return "abstain"
	}
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
	chatRouter, err := loadChatRouter(opts)
	if err != nil {
		return nil, err
	}
	opts.ModelName = defaultModel
	s := &Server{
		opts:         opts,
		defaultModel: defaultModel,
		modelOrder:   order,
		models:       models,
		tokenizer:    tok,
		store:        opts.Store,
		research:     opts.Research,
		chatRouter:   chatRouter,
		retriever:    retriever.NewRetriever(opts.Store),
		created:      time.Now().Unix(),
		experts:      map[string]*atomic.Uint64{},
		adminState:   newAdminPipeline(),
	}
	for _, name := range expertNames {
		s.experts[name] = &atomic.Uint64{}
	}

	if s.store != nil {
		if err := s.indexKnowledge(opts.KnowledgePath); err != nil {
			return nil, fmt.Errorf("index knowledge corpus: %w", err)
		}
		s.answerers = answerer.DefaultWithKnowledge(s.codingKnowledgeOrLearn)
	} else {
		s.answerers = answerer.Default()
	}
	return s, nil
}

func loadChatRouter(opts Options) (router.Router, error) {
	if opts.Router != nil {
		return opts.Router, nil
	}
	checkpoint := strings.TrimSpace(opts.RouterCheckpoint)
	if checkpoint == "" {
		checkpoint = os.Getenv("ALETHEIA_ROUTER_CHECKPOINT")
	}
	if checkpoint == "" {
		checkpoint = filepath.Join("checkpoints", "router-mikros")
	}
	if _, err := os.Stat(filepath.Join(checkpoint, "router.json")); err != nil {
		if os.IsNotExist(err) {
			return router.NewFallback(), nil
		}
		return nil, err
	}
	loaded, err := router.LoadLinear(checkpoint)
	if err != nil {
		return nil, fmt.Errorf("load router checkpoint %s: %w", checkpoint, err)
	}
	return loaded, nil
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
	// Admin pipeline (seed -> harvest -> train). Self-gated by X-Admin-Token;
	// inert unless ALETHEIA_ADMIN_TOKEN is configured.
	mux.HandleFunc("POST /v1/aletheia/admin/pipeline", s.handleAdminPipeline)
	mux.HandleFunc("GET /v1/aletheia/admin/pipeline", s.handleAdminStatus)
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
	if opts.KnowledgePath == "" {
		opts.KnowledgePath = DefaultKnowledgePath
	}
	return opts
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.requests.Add(1)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":             "ok",
		"model_loaded":       true,
		"model":              s.defaultModel,
		"models":             s.publicModelOrder(),
		"models_loaded":      len(s.models),
		"context_length":     s.defaultContextLength(),
		"max_output_tokens":  DefaultMaxOutputTokens,
		"supports_tools":     true,
		"supports_streaming": true,
		"tokenizer":          "byte-v1",
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
		"status":             "ready",
		"model_loaded":       true,
		"models_loaded":      len(s.models),
		"memory_configured":  s.store != nil,
		"research_enabled":   s.research.Enabled,
		"context_length":     s.defaultContextLength(),
		"max_output_tokens":  DefaultMaxOutputTokens,
		"supports_tools":     true,
		"supports_streaming": true,
		"tokenizer":          "byte-v1",
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

	for _, name := range expertNames {
		_, _ = fmt.Fprintf(w, "aletheia_expert_total{expert=%q} %d\n", name, s.experts[name].Load())
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

func (s *Server) defaultContextLength() int {
	if model, ok := s.models[s.defaultModel]; ok {
		return model.Manifest.Config.ContextLength
	}
	return 0
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	s.requests.Add(1)
	s.chats.Add(1)
	var req chatCompletionRequest
	if !s.decodeRequest(w, r, &req) {
		return
	}
	respond := func(response map[string]any) {
		includeUsage := req.StreamOptions != nil && req.StreamOptions.IncludeUsage
		s.writeChatCompletion(w, req.Stream, includeUsage, response)
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
		s.expert("tool_call")
		respond(s.toolCallResponse(responseModelID(req.Model, served.ID), toolCall, s.textUsage(prompt, toolCall.Function.Name)))
		return
	}
	if reply, ok := codingToolResultReply(req.Messages); ok {
		respond(s.chatResponse(responseModelID(req.Model, served.ID), reply, s.textUsage(prompt, reply)))
		return
	}
	query := strings.TrimSpace(effectiveUserQuery(req.Messages))
	decision := s.routeChat(query, req.Messages, req.Tools)

	if query != "" && looksLikeMath(query) {
		decision.Intent = router.IntentMath
	} else if query != "" && looksLikeNonsense(query) {

		content := "No entiendo bien tu mensaje. ¿Podés reformularlo? Puedo ayudar con código, cálculos, traducciones cortas, herramientas tipo OpenCode o preguntas con evidencia."
		s.expert("nonsense")
		s.recordRouterExample(query, router.IntentAbstain)
		respond(s.chatResponse(responseModelID(req.Model, served.ID), content, s.textUsage(prompt, content)))
		return
	} else if isAmbiguousFollowup(lastUserMessage(req.Messages)) {

		content := "Necesito un poco mas de contexto para seguir. Decime el tema o la pregunta completa y respondo con evidencia si hace falta."
		s.expert("ambiguous")
		respond(s.chatResponse(responseModelID(req.Model, served.ID), content, s.textUsage(prompt, content)))
		return
	} else if query != "" && isCodingKnowledgeQuery(query) {

		if isCodeGenerationRequest(normalizeBasicChat(query)) {
			decision.Intent = router.IntentCodeGeneration
		} else {
			decision.Intent = router.IntentCodingHelp
		}
	}
	forceEvidence := query != "" && isFactualKnowledgeQuestion(query)
	if forceEvidence {

		s.recordRouterExample(query, router.IntentFactualResearch)
	}
	if !forceEvidence {
		if local, ok, err := s.answerers.Answer(r.Context(), answerer.Request{
			Query:    query,
			Messages: toRouterMessages(req.Messages),
			Intent:   decision.Intent,
			HasTools: len(req.Tools) > 0,
		}); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "server_error", "", "answerer_failed", err.Error())
			return
		} else if ok {
			s.expert(expertForIntent(local.Intent))
			s.recordRouterExample(query, local.Intent)
			respond(s.chatResponse(responseModelID(req.Model, served.ID), local.Content, s.textUsage(prompt, local.Content)))
			return
		}
	}
	if reply, ok := policyReply(served.ID, req.Messages); ok {
		respond(s.chatResponse(responseModelID(req.Model, served.ID), reply, s.textUsage(prompt, reply)))
		return
	}
	if reply, ok := codingKnowledgeReply(req.Messages); ok {
		respond(s.chatResponse(responseModelID(req.Model, served.ID), reply, s.textUsage(prompt, reply)))
		return
	}
	if query != "" && isChatActionRequest(query) {
		if generated, usage, ok := s.safeGenerate(served, prompt, generationOptions{
			MaxTokens:   maxTokens,
			Temperature: req.Temperature,
			TopP:        req.TopP,
			TopK:        req.TopK,
		}); ok {
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

		content := "Puedo ayudarte a generar ese código, pero necesito un poco más de detalle: lenguaje, objetivo concreto y entrada/salida esperada. No voy a inventar una respuesta."
		respond(s.chatResponse(responseModelID(req.Model, served.ID), content, s.textUsage(prompt, content)))
		return
	}
	if query != "" && isUnsupportedFutureOutcomeQuestion(query) {
		content := "No tengo evidencia suficiente para responder eso como hecho verificado. La pregunta pide un resultado futuro o no confirmado; necesito fuentes directas y actuales antes de afirmarlo."
		s.expert("future_abstain")
		s.recordRouterExample(query, router.IntentAbstain)
		respond(s.chatResponse(responseModelID(req.Model, served.ID), content, s.textUsage(prompt, content)))
		return
	}
	if query != "" && isAmbiguousFollowup(lastUserMessage(req.Messages)) {
		content := "Necesito un poco mas de contexto para seguir. Decime el tema o la pregunta completa y respondo con evidencia si hace falta."
		respond(s.chatResponse(responseModelID(req.Model, served.ID), content, s.textUsage(prompt, content)))
		return
	}
	if query != "" && !isResearchableQuestion(query) && !isResearchIntent(decision.Intent) {
		if reply, ok := trainedExampleReply(served, req.Messages); ok {
			respond(s.chatResponse(responseModelID(req.Model, served.ID), reply, s.textUsage(prompt, reply)))
			return
		}
		if reply, ok := basicMikrosChatReply(served.ID, req.Messages); ok {
			respond(s.chatResponse(responseModelID(req.Model, served.ID), reply, s.textUsage(prompt, reply)))
			return
		}
	}
	if query != "" && s.store != nil {
		if answer, ok := s.completedResearchAnswer(r.Context(), query); ok {
			s.expert("memory_verified")
			respond(s.chatResponse(responseModelID(req.Model, served.ID), answer, s.textUsage(prompt, answer)))
			return
		}
		answer, err := s.retriever.Answer(r.Context(), query, retriever.SearchOptions{TopK: 5, MinConfidence: retriever.DefaultMinConfidence})
		if err == nil && answer.Verified && answerRelevantToQuery(query, answer.Text) {
			s.expert("memory_verified")
			text := answer.Text
			if cites := formatCitations(answer.Citations); cites != "" {
				text += "\n\n" + cites
			}
			respond(s.chatResponse(responseModelID(req.Model, served.ID), text, s.textUsage(prompt, answer.Text)))
			return
		}
		researchMode := "background"
		if req.Aletheia != nil && strings.TrimSpace(req.Aletheia.Research) != "" {
			researchMode = strings.TrimSpace(req.Aletheia.Research)
		}
		if shouldResearch(query, researchMode, s.research) {

			researchAnswer, jobID, ok := s.researchSyncFirst(r.Context(), query)
			if ok {
				s.expert("research_verified")
				respond(s.chatResponse(responseModelID(req.Model, served.ID), researchAnswer, s.textUsage(prompt, researchAnswer)))
				return
			}
			if jobID != "" {
				content := "Estoy buscando información para responderte con fuentes. Volvé a preguntar en unos segundos y te respondo desde lo que encuentre."
				s.expert("research_learn")
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
	if query != "" && (isFactualKnowledgeQuestion(query) || isResearchIntent(decision.Intent)) {
		content := "No tengo evidencia local suficiente para responder eso de forma confiable. Si habilitas research, puedo buscar fuentes y guardar la evidencia para futuras preguntas."
		s.expert("factual_abstain")
		s.recordRouterExample(query, router.IntentFactualResearch)
		respond(s.chatResponse(responseModelID(req.Model, served.ID), content, s.textUsage(prompt, content)))
		return
	}
	if routed && served.ID == hephaestusModelName {
		content := "Puedo ayudar con codigo, pero necesito un poco mas de contexto: lenguaje, objetivo, entrada/salida esperada y restricciones. No voy a buscar fuentes web para inventar una respuesta."
		respond(s.chatResponse(responseModelID(req.Model, served.ID), content, s.textUsage(prompt, content)))
		return
	}
	if generated, usage, ok := s.safeGenerate(served, prompt, generationOptions{
		MaxTokens:   maxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		TopK:        req.TopK,
	}); ok {
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
		s.expert("generated")
		return
	}

	s.expert("honest_fallback")
	respond(s.chatResponse(responseModelID(req.Model, served.ID), honestFallback, s.textUsage(prompt, honestFallback)))
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

func (s *Server) writeChatCompletion(w http.ResponseWriter, stream bool, includeUsage bool, response map[string]any) {
	if !stream {
		writeJSON(w, http.StatusOK, response)
		return
	}
	writeChatCompletionStream(w, includeUsage, response)
}

func writeChatCompletionStream(w http.ResponseWriter, includeUsage bool, response map[string]any) {
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
	if includeUsage {
		usage, _ := response["usage"].(map[string]int)
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
	generated, usage, ok := s.safeGenerate(served, req.Prompt, generationOptions{
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		TopK:        req.TopK,
	})
	if !ok {

		generated = honestFallback
		usage = map[string]int{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0}
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
		MaxTokens:   intDefault(opts.MaxTokens, DefaultMaxOutputTokens),
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

func (s *Server) routeChat(query string, messages []chatMessage, tools []chatTool) router.RouteDecision {
	if s.chatRouter == nil {
		s.chatRouter = router.NewFallback()
	}
	return s.chatRouter.Route(router.Input{
		Text:     query,
		Messages: toRouterMessages(messages),
		HasTools: len(tools) > 0,
	})
}

func toRouterMessages(messages []chatMessage) []router.Message {
	out := make([]router.Message, 0, len(messages))
	for _, message := range messages {
		out = append(out, router.Message{Role: message.Role, Content: message.Content})
	}
	return out
}

func isResearchIntent(intent router.Intent) bool {
	return intent == router.IntentFactualResearch || intent == router.IntentDocumentQA
}

func effectiveUserQuery(messages []chatMessage) string {
	last := strings.TrimSpace(lastUserMessage(messages))
	if last == "" {
		return ""
	}

	last = unwrapPackedUserMessage(last)
	if !isContextualFollowup(last) {
		return last
	}
	previous := strings.TrimSpace(previousUserMessage(messages))
	if previous == "" {
		return last
	}
	return strings.TrimSpace(previous + " " + last)
}

// unwrapPackedUserMessage extracts the real question from a context-packed user
// message. Clients sometimes prepend instructions and history and end with a
// "Current user message:" marker; routing must key on the question that follows
// it, not the English wrapper. Returns the input unchanged when no marker is
// present.
func unwrapPackedUserMessage(text string) string {
	lower := strings.ToLower(text)
	markers := []string{"current user message:", "mensaje actual del usuario:"}
	best := -1
	bestLen := 0
	for _, m := range markers {
		if idx := strings.LastIndex(lower, m); idx >= 0 {
			if idx > best {
				best = idx
				bestLen = len(m)
			}
		}
	}
	if best < 0 {
		return text
	}
	extracted := strings.TrimSpace(text[best+bestLen:])
	if extracted == "" {
		return text
	}
	return extracted
}

func previousUserMessage(messages []chatMessage) string {
	seenLast := false
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		if !seenLast {
			seenLast = true
			continue
		}
		return messages[i].Content
	}
	return ""
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

// answerRelevantToQuery guards the retriever path against cross-topic
// contamination: stored web chunks from an earlier question (e.g. "qué es
// HTTP") must not be served verbatim for an unrelated one (e.g. "TCP vs UDP")
// just because the text-evidence verifier finds the sentence supported by its
// own citation. The answer has to actually share meaningful tokens with the
// query, or we fall through to fresh research.
func answerRelevantToQuery(query string, answer string) bool {
	queryTokens := meaningfulChatTokens(query)
	if len(queryTokens) == 0 {
		return true
	}
	return meaningfulOverlap(queryTokens, meaningfulChatTokens(answer)) >= requiredMeaningfulOverlap(len(queryTokens))
}

// researchSyncFirst runs a research job inline, bounded by a short latency cap,
// so a knowledge-gap question can be answered in the same turn. On success it
// returns the formatted answer. If it can't finish in time it re-queues the job
// for the background worker and returns the job ID for a "ask again" stub, so
// the next ask is served instantly from memory (the corpus grows by use).
func (s *Server) researchSyncFirst(ctx context.Context, query string) (string, string, bool) {
	if s.store == nil || !s.research.Enabled {
		return "", "", false
	}
	job, err := s.store.CreateResearchJob(ctx, memory.ResearchJob{
		Query:      query,
		Status:     "running",
		Mode:       "sync",
		MaxSources: s.research.MaxSources,
	})
	if err != nil {
		return "", "", false
	}
	wait := 8 * time.Second
	if jt := s.research.JobTimeout; jt > 0 && jt < wait {
		wait = jt
	}
	runCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	worker := research.NewWorker(s.store, s.research)
	result, err := worker.RunJob(runCtx, job)
	if err != nil {

		job.Status = "queued"
		_ = s.store.UpdateResearchJob(context.Background(), job)
		s.queued.Add(1)
		return "", job.ID, false
	}
	job.Answer = result.Answer
	job.Confidence = result.Confidence
	job.Status = "completed"
	sources, _ := s.store.WebSourcesByJob(ctx, job.ID)
	claims, _ := s.store.WebClaimsByJob(ctx, job.ID)
	formatted, ok := formatResearchAnswer(query, job, sources, claims)
	if !ok {
		return "", job.ID, false
	}
	return formatted, job.ID, true
}

func formatResearchAnswer(query string, job memory.ResearchJob, sources []memory.WebSource, claims []memory.WebClaim) (string, bool) {
	answer := bestPublicResearchAnswer(query, job, sources, claims)
	if answer == "" {
		return "", false
	}
	var b strings.Builder
	b.WriteString(strings.TrimSpace(answer))
	// Build the source bullets first; only emit the "Fuentes:" header if at
	// least one survives filtering, so a fully-filtered source list never leaves
	// an orphan heading with nothing under it.
	var bullets strings.Builder
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
			bullets.WriteString(fmt.Sprintf("- %s\n", url))
		} else {
			bullets.WriteString(fmt.Sprintf("- %s - %s\n", title, url))
		}
		written++
		if written >= 2 {
			break
		}
	}
	if written > 0 {
		b.WriteString("\n\nFuentes:\n")
		b.WriteString(bullets.String())
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
	if answer, ok := research.CanonicalAnswer(query, publicResearchEvidenceTexts(job, sources, claims)); ok {
		return answer
	}
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
		return best.text
	}
	answer := strings.TrimSpace(job.Answer)
	answer = cleanPublicResearchText(query, answer)
	if answer == "" || looksLikeTitleOnly(answer) || !researchAnswerMatchesQuery(query, job) {
		return ""
	}
	return answer
}

func publicResearchEvidenceTexts(job memory.ResearchJob, sources []memory.WebSource, claims []memory.WebClaim) []string {
	texts := []string{job.Query, job.Answer}
	for _, source := range sources {
		if !publicWebSourceAllowed(source) {
			continue
		}
		texts = append(texts, source.Title, source.Snippet)
	}
	for _, claim := range claims {
		texts = append(texts, claim.Claim)
	}
	return texts
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

var publicGluedSentenceRe = regexp.MustCompile(`([.!?])(\p{Lu})`)

var (
	publicLeadingSepRe     = regexp.MustCompile(`^[\s"'` + "`" + `·•‹›«»>|–—,;:.\-]+`)
	publicLeadingDateRe    = regexp.MustCompile(`(?i)^\s*\d{1,2}\s+(?:ene|feb|mar|abr|may|jun|jul|ago|sep|sept|oct|nov|dic|jan|apr|aug|dec)\w*\.?\s+\d{4}\b`)
	publicLeadingNumDateRe = regexp.MustCompile(`^\s*\d{1,4}[/.\-]\d{1,2}[/.\-]\d{1,4}\b`)
)

// stripLeadingPublicChrome mirrors research.stripLeadingChrome for the apiserver
// rendering path: it peels a leading date byline or separator junk off an
// answer ("3 jun 2023 · Una integral…" -> "Una integral…").
func stripLeadingPublicChrome(text string) string {
	for {
		before := text
		text = publicLeadingDateRe.ReplaceAllString(text, "")
		text = publicLeadingNumDateRe.ReplaceAllString(text, "")
		text = publicLeadingSepRe.ReplaceAllString(text, "")
		text = strings.TrimSpace(text)
		if text == before {
			break
		}
	}
	return text
}

func cleanPublicResearchText(query string, text string) string {
	text = stripEvidenceLines(text)
	text = stripPageChrome(text)

	text = publicGluedSentenceRe.ReplaceAllString(text, "$1 $2")
	queryTokens := meaningfulChatTokens(query)
	best := ""
	bestScore := 0
	for _, sentence := range splitPublicSentences(text) {
		sentence = strings.TrimSpace(trimBeforePublicCoreTerm(query, sentence))
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

	if containsChrome(best) {
		return ""
	}
	best = trimBeforePublicCoreTerm(query, best)
	best = stripLeadingPublicChrome(best)
	if len([]rune(best)) > 420 {
		runes := []rune(best)
		best = string(runes[:420])
		if idx := strings.LastIndex(best, " "); idx > 180 {
			best = best[:idx]
		}
		best = strings.TrimRight(best, " ,;:")
		if !strings.HasSuffix(best, ".") && !strings.HasSuffix(best, "!") && !strings.HasSuffix(best, "?") {
			best += "."
		}
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
		if strings.HasPrefix(lower, "evidence status:") || strings.HasPrefix(lower, "source:") ||
			strings.HasPrefix(lower, "source url:") || strings.HasPrefix(lower, "fetched:") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, " ")
}

// chromeMarkers are navigation/promo/interstitial phrases that web extraction
// drags in. A segment containing one is page chrome, not an answer.
var chromeMarkers = []string{
	"tambien te puede interesar", "también te puede interesar", "te puede interesar",
	"ofrecemos servicios", "servicios profesionales de desarrollo", "lee tambien", "leé también",
	"contenido relacionado", "articulos relacionados", "artículos relacionados",
	"compartir en", "copiar url", "copiar enlace", "suscribete", "suscríbete", "newsletter",
	"just a moment", "habilita javascript", "enable javascript", "aceptar cookies", "politica de cookies",
	"lo ultimo", "lo último",
}

func stripPageChrome(text string) string {
	replacements := []string{
		"WhatsApp", "Twitter", "Facebook", "Linkedin", "LinkedIn", "Telegram",
		"Copiar URL", "Beloud", "Lo Ultimo", "Lo Último",
	}
	for _, value := range replacements {
		text = strings.ReplaceAll(text, value, " ")
	}

	segments := regexp.MustCompile(`[.!?]\s+`).Split(text, -1)
	kept := make([]string, 0, len(segments))
	for _, seg := range segments {
		if containsChrome(seg) {
			continue
		}
		kept = append(kept, seg)
	}
	text = strings.Join(kept, ". ")

	text = stripSymbols(text)
	return strings.Join(strings.Fields(text), " ")
}

func containsChrome(text string) bool {
	lower := normalizeBasicChat(text)
	for _, m := range chromeMarkers {
		if strings.Contains(lower, normalizeBasicChat(m)) {
			return true
		}
	}
	return false
}

func stripSymbols(text string) string {
	var b strings.Builder
	for _, r := range text {
		if r > 0x2190 {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
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

// trimBeforePublicCoreTerm drops leading page chrome by starting the answer at
// the earliest meaningful query term. Like its research-package counterpart it
// bakes in NO marker words: it only trims when the lead-in before the first
// keyword carries a structural chrome signal (stray HTML/quote leak or
// list/byline separator). A normal sentence subject before the keyword is left
// intact rather than truncated.
func trimBeforePublicCoreTerm(query string, text string) string {
	lower := strings.ToLower(text)
	best := -1
	for token := range meaningfulChatTokens(query) {
		if len([]rune(token)) < 4 {
			continue
		}
		idx := strings.Index(lower, token)
		if idx < 0 {
			continue
		}
		if best == -1 || idx < best {
			best = idx
		}
	}
	if best <= 24 {
		return text
	}

	// Chrome signal: a stray HTML/quote leak or a date/number (byline/timestamp).
	// A clean prose subject before the keyword has neither and is kept intact.
	leadIn := text[:best]
	if strings.ContainsAny(leadIn, "\"|•›»>") || strings.ContainsAny(leadIn, "0123456789") {
		return strings.TrimSpace(text[best:])
	}
	return text
}

func looksLikeTitleOnly(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}
	if isQuestionTitle(text) || isPublicCaptionLead(text) {
		return true
	}
	if strings.Contains(text, "\n\n") || len([]rune(text)) > 220 {
		return false
	}
	terminal := strings.HasSuffix(text, ".") || strings.HasSuffix(text, "!") || strings.HasSuffix(text, "?")
	return !terminal || strings.Contains(strings.ToLower(text), " - wikipedia") || strings.Contains(strings.ToLower(text), " | ")
}

// isPublicCaptionLead skips image/figure/table captions (page furniture, not
// answers). The lead words are document structure, not domain facts.
var publicCaptionLeads = map[string]bool{
	"imagen": true, "foto": true, "fotografia": true, "figura": true, "fig": true,
	"grafico": true, "tabla": true, "video": true, "ilustracion": true,
	"mapa": true, "diagrama": true, "captura": true, "infografia": true,
}

func isPublicCaptionLead(text string) bool {
	fields := strings.Fields(normalizeBasicChat(text))
	if len(fields) == 0 {
		return false
	}
	return publicCaptionLeads[fields[0]]
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
	for _, bad := range []string{"blocked", "just a moment", "attention required", "access denied", "are you a robot", "enable javascript", "verifying you are human"} {
		if strings.Contains(title, bad) {
			return false
		}
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
	if answerIsInsufficient(job.Answer) {
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

func answerIsInsufficient(answer string) bool {
	normalized := normalizeBasicChat(answer)
	return normalized == "" ||
		strings.Contains(normalized, "no hay evidencia web suficiente") ||
		strings.Contains(normalized, "no tengo evidencia suficiente") ||
		strings.Contains(normalized, "insufficient evidence")
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
	switch {
	case tokenCount <= 1:
		return 1
	case tokenCount <= 3:

		return 2
	default:

		req := (tokenCount*3 + 4) / 5
		if req < 2 {
			req = 2
		}
		return req
	}
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
	return isFactualKnowledgeQuestion(normalized) || hasAny(normalized,
		"que fue", "que es", "quien fue", "quien es", "cuando", "donde", "por que",
		"what is", "what was", "who is", "who was", "when", "where", "why",
		"historia", "guerra", "pais", "empresa", "protocolo", "actual", "latest",
	)
}

func isFactualKnowledgeQuestion(query string) bool {
	normalized := normalizeBasicChat(query)
	if normalized == "" || isChatActionRequest(normalized) || isUnsupportedFutureOutcomeQuestion(normalized) {
		return false
	}

	if isSmalltalkOrIdentity(normalized) || isTranslationRequest(normalized) {
		return false
	}
	if len(detectCodingLanguages(normalized)) > 0 || isCodingTask(normalized) {
		return false
	}
	if looksLikeMath(normalized) {
		return false
	}

	return hasAny(normalized,
		"quien ", "quienes ", "que ", "cual ", "cuales ",
		"cuando ", "donde ", "cuanto ", "cuanta ", "cuantos ", "cuantas ", "por que ",
		"como ", "how ",
		"who ", "what ", "which ", "when ", "where ", "how many", "how much", "why ",
		// Knowledge-seeking imperatives ("explicame X", "contame de Y") are
		// questions too — without these they fall to the weak router and get a
		// generic greeting instead of evidence/abstention.
		"explicame", "explica ", "explicacion", "contame", "contanos", "hablame",
		"describe", "describi", "descripcion", "definicion", "explain", "tell me about",
		"ganador", "campeon", "campeones", "copa america", "mundial", "latest",
	)
}

// isSmalltalkOrIdentity matches greetings, thanks, farewells, and the agent's
// own identity/capability questions. These must never be routed as factual.
func isSmalltalkOrIdentity(normalized string) bool {
	return hasAny(normalized,
		"hola", "buenas", "buen dia", "buenos dias", "hello", "hi ", "hey", "que onda", "que tal",
		"gracias", "thanks", "chau", "adios", "bye", "como estas", "how are you",
		"quien sos", "quien eres", "que sos", "como te llamas", "tu nombre", "what are you", "who are you", "who built you",
		"que puedes hacer", "que podes hacer", "que sabes hacer", "what can you do", "ayuda", "help",
	)
}

func isTranslationRequest(normalized string) bool {
	return hasAny(normalized, "traduce", "traduci", "translate", "como se dice", "how do you say")
}

// looksLikeMath delegates to the answerer's real math detector so routing and
// the math evaluator stay in sync.
func looksLikeMath(query string) bool {
	return answerer.LooksLikeMath(query)
}

// isCodingKnowledgeQuery reports whether the query is a programming question
// that should be handled by the coding answerer (curated/retrieved/honest),
// rather than research or generation. Repo-repair requests are excluded (they
// go to solve), and named-but-unsupported languages still count so the corpus
// can answer them.
func isCodingKnowledgeQuery(query string) bool {
	normalized := normalizeBasicChat(query)
	if normalized == "" || isRepoRepairRequest(normalized) {
		return false
	}
	if len(detectCodingLanguages(normalized)) > 0 || isProgrammingHelpRequest(normalized) {
		return true
	}
	return hasProgrammingLanguage(normalized) && hasAny(normalized,
		"como", "ejemplo", "funcion", "function", "loop", "for", "clase", "class", "metodo", "method")
}

func isContextualFollowup(query string) bool {
	normalized := normalizeBasicChat(query)
	if normalized == "" {
		return false
	}
	return hasAny(normalized,
		"pero dime", "pero decime", "dime los", "decime los", "y los", "y las",
		"resultados de", "ultimas 5", "ultimos 5", "las ultimas", "los ultimos",
	)
}

func isAmbiguousFollowup(query string) bool {
	normalized := normalizeBasicChat(query)
	switch normalized {
	case "y entonces", "entonces", "y ahora", "ok y", "y eso", "y eso?", "continua", "continúa", "segui", "seguí", "dale", "mas", "y", "ok", "bueno y":
		return true
	}
	return false
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
	seen := map[string]bool{}
	written := 0
	for _, citation := range citations {
		path := strings.TrimSpace(citation.Path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true

		b.WriteString(fmt.Sprintf("- %s\n", path))
		written++
		if written >= 2 {
			break
		}
	}
	if written == 0 {
		return ""
	}
	return strings.TrimSpace(b.String())
}

func researchResultJSON(jobID string, result research.ResearchResult) map[string]any {
	return map[string]any{
		"status":          "completed",
		"job_id":          jobID,
		"sources_found":   len(result.Sources),
		"sources_stored":  result.SourcesStored,
		"chunks_stored":   result.ChunksStored,
		"claims_stored":   result.ClaimsStored,
		"confidence":      result.Confidence,
		"evidence_status": result.EvidenceStatus,
		"answer":          result.Answer,
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
