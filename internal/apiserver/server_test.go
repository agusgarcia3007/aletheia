package apiserver

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aletheia/internal/memory"
	"aletheia/internal/model"
	"aletheia/internal/research"
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

func TestReadyAndMetricsDoNotRequireAuth(t *testing.T) {
	store := newTestStore(t)
	server := newTestServer(t, Options{APIKey: "secret", Store: store})
	ready := httptest.NewRecorder()
	server.Handler().ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusOK || !strings.Contains(ready.Body.String(), `"status":"ready"`) {
		t.Fatalf("ready status = %d body=%s", ready.Code, ready.Body.String())
	}
	metrics := httptest.NewRecorder()
	server.Handler().ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metrics.Code != http.StatusOK || !strings.Contains(metrics.Body.String(), "aletheia_requests_total") {
		t.Fatalf("metrics status = %d body=%s", metrics.Code, metrics.Body.String())
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
	if !strings.Contains(withAuth.Body.String(), `"id":"aletheia-mikros"`) {
		t.Fatalf("body = %s", withAuth.Body.String())
	}
}

func TestModelsListsPublicMikrosAndAutoRoutesCodingInternally(t *testing.T) {
	server := newMultiModelTestServer(t, Options{APIKey: "secret"})
	models := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer secret")
	server.Handler().ServeHTTP(models, req)
	if models.Code != http.StatusOK || !strings.Contains(models.Body.String(), `"id":"aletheia-mikros"`) || strings.Contains(models.Body.String(), `"id":"aletheia-hephaestus"`) {
		t.Fatalf("models status = %d body=%s", models.Code, models.Body.String())
	}

	rec := serveJSON(t, server, "/v1/chat/completions", `{"model":"aletheia-mikros","messages":[{"role":"user","content":"como es el codigo en rust?"}]}`, "secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"model":"aletheia-mikros"`) || !strings.Contains(rec.Body.String(), "Rust") || strings.Contains(rec.Body.String(), "Fuentes") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestHephaestusReturnsToolCallsWhenToolsProvided(t *testing.T) {
	server := newMultiModelTestServer(t, Options{APIKey: "secret"})
	body := `{
		"model":"aletheia-mikros",
		"messages":[{"role":"user","content":"run the tests"}],
		"tools":[{"type":"function","function":{"name":"run_command","parameters":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}}}]
	}`
	rec := serveJSON(t, server, "/v1/chat/completions", body, "secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"finish_reason":"tool_calls"`) || !strings.Contains(rec.Body.String(), `"tool_calls"`) || !strings.Contains(rec.Body.String(), "go test ./...") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestChatCompletionsReturnsOpenAIShape(t *testing.T) {
	server := newTestServer(t, Options{APIKey: "secret"})
	body := `{"model":"aletheia-mikros","messages":[{"role":"user","content":"hola como estas?"}],"max_tokens":8}`
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
	if response.Object != "chat.completion" || response.Model != "aletheia-mikros" {
		t.Fatalf("response = %+v", response)
	}
	if len(response.Choices) != 1 || response.Choices[0].Message.Role != "assistant" || response.Choices[0].Message.Content == "" {
		t.Fatalf("choices = %+v", response.Choices)
	}
	if !strings.Contains(response.Choices[0].Message.Content, "Aletheia Mikros") {
		t.Fatalf("content = %q", response.Choices[0].Message.Content)
	}
	if response.Usage["prompt_tokens"] == 0 || response.Usage["completion_tokens"] == 0 {
		t.Fatalf("usage = %+v", response.Usage)
	}
}

func TestChatCompletionsStreamsAndIgnoresClientOptions(t *testing.T) {
	server := newTestServer(t, Options{APIKey: "secret"})
	stream := serveJSON(t, server, "/v1/chat/completions", `{"model":"aletheia-mikros","messages":[{"role":"user","content":"hola"}],"stream":true,"stream_options":{"include_usage":true},"service_tier":"auto"}`, "secret")
	if stream.Code != http.StatusOK || !strings.Contains(stream.Header().Get("Content-Type"), "text/event-stream") || !strings.Contains(stream.Body.String(), "chat.completion.chunk") || !strings.Contains(stream.Body.String(), "[DONE]") {
		t.Fatalf("stream status = %d body=%s", stream.Code, stream.Body.String())
	}
}

func TestChatCompletionsRejectsWrongModel(t *testing.T) {
	server := newTestServer(t, Options{APIKey: "secret"})
	wrongModel := serveJSON(t, server, "/v1/chat/completions", `{"model":"other","messages":[{"role":"user","content":"x"}]}`, "secret")
	if wrongModel.Code != http.StatusBadRequest || !strings.Contains(wrongModel.Body.String(), "model_not_found") {
		t.Fatalf("wrong model status = %d body=%s", wrongModel.Code, wrongModel.Body.String())
	}
}

func TestChatCompletionsStreamsToolCallsForAgentClients(t *testing.T) {
	server := newMultiModelTestServer(t, Options{APIKey: "secret"})
	body := `{
		"model":"aletheia-mikros",
		"messages":[{"role":"user","content":"analiza este repositorio"}],
		"stream":true,
		"stream_options":{"include_usage":true},
		"tools":[{"type":"function","function":{"name":"list_files","parameters":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}}]
	}`
	rec := serveJSON(t, server, "/v1/chat/completions", body, "secret")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"tool_calls"`) || !strings.Contains(rec.Body.String(), `"finish_reason":"tool_calls"`) || !strings.Contains(rec.Body.String(), "[DONE]") {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCompletionsAndBodyLimit(t *testing.T) {
	server := newTestServer(t, Options{APIKey: "secret"})
	completion := serveJSON(t, server, "/v1/completions", `{"model":"aletheia-mikros","prompt":"<USER>x<ASSISTANT>","max_tokens":4}`, "secret")
	if completion.Code != http.StatusOK || !strings.Contains(completion.Body.String(), `"object":"text_completion"`) {
		t.Fatalf("completion status = %d body=%s", completion.Code, completion.Body.String())
	}

	limited := newTestServer(t, Options{APIKey: "secret", MaxBodyBytes: 12})
	tooLarge := serveJSON(t, limited, "/v1/chat/completions", `{"model":"aletheia-mikros","messages":[{"role":"user","content":"x"}]}`, "secret")
	if tooLarge.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("too large status = %d body=%s", tooLarge.Code, tooLarge.Body.String())
	}
}

func TestMikrosCheckpointDoesNotEmitActionTokensForGreeting(t *testing.T) {
	server := newNamedTestServer(t, "aletheia-mikros", Options{APIKey: "secret"})
	body := `{"model":"aletheia-mikros","messages":[{"role":"user","content":"hola como estas?"}],"max_tokens":16}`
	rec := serveJSON(t, server, "/v1/chat/completions", body, "secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Choices) != 1 {
		t.Fatalf("choices = %+v", response.Choices)
	}
	content := response.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" || strings.Contains(content, "<ACT_") {
		t.Fatalf("content = %q", content)
	}
	if !strings.Contains(content, "Aletheia Mikros") {
		t.Fatalf("content = %q", content)
	}
}

func TestChatSmalltalkDoesNotCreateResearchJob(t *testing.T) {
	store := newTestStore(t)
	server := newTestServer(t, Options{
		APIKey: "secret",
		Store:  store,
		Research: research.Options{
			Enabled:            true,
			AutoOnKnowledgeGap: true,
			MaxSources:         3,
		},
	})
	for _, message := range []string{"hola", "quien eres?", "quien sos vos?", "que sabes hacer?"} {
		body := `{"model":"aletheia-mikros","messages":[{"role":"user","content":"` + message + `"}]}`
		rec := serveJSON(t, server, "/v1/chat/completions", body, "secret")
		if rec.Code != http.StatusOK {
			t.Fatalf("message %q status = %d body=%s", message, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "job_id=") {
			t.Fatalf("message %q triggered research: %s", message, rec.Body.String())
		}
	}
	jobs, err := store.ListResearchJobs(contextBackground(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestChatActionRequestsDoNotTriggerResearch(t *testing.T) {
	store := newTestStore(t)
	server := newTestServer(t, Options{
		APIKey: "secret",
		Store:  store,
		Research: research.Options{
			Enabled:            true,
			AutoOnKnowledgeGap: true,
			MaxSources:         3,
		},
	})
	cases := []struct {
		message string
		want    string
	}{
		{message: "haz un componente de react", want: ""},
		{message: "arregla este repo de Go que falla go test ./...", want: "aletheia solve"},
	}
	for _, tt := range cases {
		body := `{"model":"aletheia-mikros","messages":[{"role":"user","content":"` + tt.message + `"}]}`
		rec := serveJSON(t, server, "/v1/chat/completions", body, "secret")
		if rec.Code != http.StatusOK {
			t.Fatalf("message %q status = %d body=%s", tt.message, rec.Code, rec.Body.String())
		}
		if tt.want != "" && !strings.Contains(rec.Body.String(), tt.want) {
			t.Fatalf("message %q body=%s", tt.message, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), "job_id=") || strings.Contains(rec.Body.String(), "Fuentes:") {
			t.Fatalf("message %q body=%s", tt.message, rec.Body.String())
		}
	}
	jobs, err := store.ListResearchJobs(contextBackground(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestCodingKnowledgePromptsStayNaturalAndOutOfResearch(t *testing.T) {
	store := newTestStore(t)
	server := newMultiModelTestServer(t, Options{
		APIKey: "secret",
		Store:  store,
		Research: research.Options{
			Enabled:            true,
			AutoOnKnowledgeGap: true,
			MaxSources:         3,
		},
	})
	cases := []struct {
		message string
		want    []string
		forbid  []string
	}{
		{message: "hablame de rust", want: []string{"Rust", "seguridad"}, forbid: []string{"Fuentes:", "job_id="}},
		{message: "como hago una funcion en javacript?", want: []string{"JavaScript", "function add"}, forbid: []string{"```go", "Fuentes:"}},
		{message: "como hago una funcion en javascript?", want: []string{"JavaScript", "function add"}, forbid: []string{"```go", "Fuentes:"}},
		{message: "que diferencia hay enter python y js", want: []string{"Python", "JavaScript"}, forbid: []string{"computerhoy", "Fuentes:"}},
	}
	for _, tt := range cases {
		rec := serveJSON(t, server, "/v1/chat/completions", `{"model":"aletheia-mikros","messages":[{"role":"user","content":"`+tt.message+`"}]}`, "secret")
		if rec.Code != http.StatusOK {
			t.Fatalf("message %q status = %d body=%s", tt.message, rec.Code, rec.Body.String())
		}
		for _, want := range tt.want {
			if !strings.Contains(rec.Body.String(), want) {
				t.Fatalf("message %q missing %q body=%s", tt.message, want, rec.Body.String())
			}
		}
		for _, forbid := range tt.forbid {
			if strings.Contains(rec.Body.String(), forbid) {
				t.Fatalf("message %q contains %q body=%s", tt.message, forbid, rec.Body.String())
			}
		}
	}
	jobs, err := store.ListResearchJobs(contextBackground(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestChatUsesTrainedExamplesForActionRequests(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.CreateResearchJob(contextBackground(), memory.ResearchJob{
		ID:         "research-mcp-unrelated",
		Query:      "what is MCP in agents?",
		Status:     "completed",
		Mode:       "background",
		MaxSources: 1,
		Answer:     "MCP connects agents with tools.\n\nEvidence status: web_verified",
		Confidence: 0.8,
	}); err != nil {
		t.Fatal(err)
	}
	server := newTestServerWithExamples(t, []string{
		`{"prompt":"<USER>haz un componente de react<ASSISTANT>","completion":"export function GreetingCard() { return <section>Hola</section> }<EOS>"}`,
		`{"prompt":"<USER>como es el codigo en rust?<ASSISTANT>","completion":"fn main() { println!(\"Hola desde Rust\"); }<EOS>"}`,
	}, Options{
		APIKey: "secret",
		Store:  store,
		Research: research.Options{
			Enabled:            true,
			AutoOnKnowledgeGap: true,
			MaxSources:         3,
		},
	})
	cases := []struct {
		message string
		want    string
	}{
		{message: "haz un componente de react", want: "GreetingCard"},
		{message: "como es el codigo en rust?", want: "seguridad"},
	}
	for _, tt := range cases {
		rec := serveJSON(t, server, "/v1/chat/completions", `{"model":"aletheia-mikros","messages":[{"role":"user","content":"`+tt.message+`"}]}`, "secret")
		if rec.Code != http.StatusOK {
			t.Fatalf("message %q status = %d body=%s", tt.message, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), tt.want) || strings.Contains(rec.Body.String(), "job_id=") || strings.Contains(rec.Body.String(), "Fuentes:") || strings.Contains(rec.Body.String(), "MCP") {
			t.Fatalf("message %q body=%s", tt.message, rec.Body.String())
		}
	}
	jobs, err := store.ListResearchJobs(contextBackground(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestChatFutureOutcomeAbstainsWithoutResearch(t *testing.T) {
	store := newTestStore(t)
	server := newTestServer(t, Options{
		APIKey: "secret",
		Store:  store,
		Research: research.Options{
			Enabled:            true,
			AutoOnKnowledgeGap: true,
			MaxSources:         3,
		},
	})
	rec := serveJSON(t, server, "/v1/chat/completions", `{"model":"aletheia-mikros","messages":[{"role":"user","content":"quien gano la copa mundial de futbol 2038?"}]}`, "secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "No tengo evidencia suficiente") || strings.Contains(rec.Body.String(), "web_verified") || strings.Contains(rec.Body.String(), "job_id=") {
		t.Fatalf("body = %s", rec.Body.String())
	}
	jobs, err := store.ListResearchJobs(contextBackground(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestChatKnowledgeGapQueuesResearchJob(t *testing.T) {
	store := newTestStore(t)
	server := newTestServer(t, Options{
		APIKey: "secret",
		Store:  store,
		Research: research.Options{
			Enabled:            true,
			AutoOnKnowledgeGap: true,
			MaxSources:         3,
		},
	})
	rec := serveJSON(t, server, "/v1/chat/completions", `{"model":"aletheia-mikros","messages":[{"role":"user","content":"what is MCP in agents?"}]}`, "secret")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "job_id=") {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	jobs, err := store.ListResearchJobs(contextBackground(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Status != "queued" {
		t.Fatalf("jobs = %+v", jobs)
	}
}

func TestResearchableQuestionBypassesTrainedFactualExample(t *testing.T) {
	store := newTestStore(t)
	server := newTestServerWithExamples(t, []string{
		`{"prompt":"<USER>que fue la guerra de vietnam?<ASSISTANT>","completion":"No debo inventar hechos historicos sin evidencia local.<EOS>"}`,
	}, Options{
		APIKey: "secret",
		Store:  store,
		Research: research.Options{
			Enabled:            true,
			AutoOnKnowledgeGap: true,
			MaxSources:         3,
		},
	})
	rec := serveJSON(t, server, "/v1/chat/completions", `{"model":"aletheia-mikros","messages":[{"role":"user","content":"que fue la guerra de vietnam?"}]}`, "secret")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "job_id=") || strings.Contains(rec.Body.String(), "No debo inventar") {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestChatUsesCompletedResearchAnswerBeforeRetriever(t *testing.T) {
	store := newTestStore(t)
	job, err := store.CreateResearchJob(contextBackground(), memory.ResearchJob{
		ID:         "research-mcp",
		Query:      "what is MCP in agents?",
		Status:     "completed",
		Mode:       "background",
		MaxSources: 2,
		Answer:     "Model Context Protocol\n\nEvidence status: web_verified\nSource: https://modelcontextprotocol.io/docs/getting-started/intro",
		Confidence: 0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertWebSource(contextBackground(), memory.WebSource{
		ID:          "source-mcp",
		JobID:       job.ID,
		URL:         "https://modelcontextprotocol.io/docs/getting-started/intro",
		FinalURL:    "https://modelcontextprotocol.io/docs/getting-started/intro",
		Title:       "Model Context Protocol",
		Status:      "stored",
		ContentHash: "hash",
		TrustScore:  0.8,
		ByteSize:    256,
		ContentType: "text/html",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordWebClaim(contextBackground(), memory.WebClaim{
		ID:         "claim-mcp-title",
		SourceID:   "source-mcp",
		Claim:      "Model Context Protocol",
		Confidence: 0.90,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordWebClaim(contextBackground(), memory.WebClaim{
		ID:         "claim-mcp-definition",
		SourceID:   "source-mcp",
		Claim:      "The Model Context Protocol (MCP) is an open protocol for connecting AI agents to tools and data sources.",
		Confidence: 0.80,
	}); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, Options{
		APIKey:   "secret",
		Store:    store,
		Research: research.Options{Enabled: true, AutoOnKnowledgeGap: true, MinTrustScore: 0.35, MaxSources: 3},
	})
	rec := serveJSON(t, server, "/v1/chat/completions", `{"model":"aletheia-mikros","messages":[{"role":"user","content":"que es un MCP?"}]}`, "secret")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "open protocol") || !strings.Contains(rec.Body.String(), "https://modelcontextprotocol.io") {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Evidence status") || strings.Contains(rec.Body.String(), "Source:") {
		t.Fatalf("body leaked research metadata: %s", rec.Body.String())
	}
}

func TestChatDoesNotCiteBlockedResearchSources(t *testing.T) {
	store := newTestStore(t)
	job, err := store.CreateResearchJob(contextBackground(), memory.ResearchJob{
		ID:         "research-blocked",
		Query:      "what is MCP in agents?",
		Status:     "completed",
		Mode:       "background",
		MaxSources: 2,
		Answer:     "The Model Context Protocol connects agents with external tools.\n\nEvidence status: web_verified",
		Confidence: 0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []memory.WebSource{
		{ID: "source-reddit", JobID: job.ID, URL: "https://www.reddit.com/r/agents/comments/x", FinalURL: "https://www.reddit.com/r/agents/comments/x", Title: "Blocked", Status: "stored", ContentHash: "r", TrustScore: 0.1, ByteSize: 10, ContentType: "text/html"},
		{ID: "source-official", JobID: job.ID, URL: "https://modelcontextprotocol.io/docs", FinalURL: "https://modelcontextprotocol.io/docs", Title: "MCP docs", Status: "stored", ContentHash: "o", TrustScore: 0.8, ByteSize: 10, ContentType: "text/html"},
	} {
		if _, err := store.UpsertWebSource(contextBackground(), source); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.RecordWebClaim(contextBackground(), memory.WebClaim{ID: "claim-blocked", SourceID: "source-official", Claim: "The Model Context Protocol connects agents with external tools.", Confidence: 0.8}); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, Options{APIKey: "secret", Store: store, Research: research.Options{Enabled: true, AutoOnKnowledgeGap: true, MinTrustScore: 0.35}})
	rec := serveJSON(t, server, "/v1/chat/completions", `{"model":"aletheia-mikros","messages":[{"role":"user","content":"what is MCP in agents?"}]}`, "secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "reddit.com") || strings.Contains(rec.Body.String(), "Blocked") || !strings.Contains(rec.Body.String(), "modelcontextprotocol.io") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestCompletedResearchAnswerStripsPageChrome(t *testing.T) {
	store := newTestStore(t)
	job, err := store.CreateResearchJob(contextBackground(), memory.ResearchJob{
		ID:         "research-vietnam",
		Query:      "que fue la guerra de vietnam?",
		Status:     "completed",
		Mode:       "background",
		MaxSources: 2,
		Answer:     "Fotografia de Stephen Wilkes Por Redaccion National Geographic Publicado 5 sep 2023 WhatsApp Twitter Facebook Copiar URL La Guerra de Vietnam fue un conflicto armado que se desarrollo durante la Guerra Fria.\n\nEvidence status: web_verified\nSource: https://example.com/vietnam",
		Confidence: 0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertWebSource(contextBackground(), memory.WebSource{
		ID:          "source-vietnam",
		JobID:       job.ID,
		URL:         "https://history.example.com/vietnam",
		FinalURL:    "https://history.example.com/vietnam",
		Title:       "Vietnam War",
		Status:      "stored",
		ContentHash: "v",
		TrustScore:  0.8,
		ByteSize:    10,
		ContentType: "text/html",
	}); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, Options{APIKey: "secret", Store: store, Research: research.Options{Enabled: true, AutoOnKnowledgeGap: true, MinTrustScore: 0.35}})
	rec := serveJSON(t, server, "/v1/chat/completions", `{"model":"aletheia-mikros","messages":[{"role":"user","content":"que fue la guerra de vietnam?"}]}`, "secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "WhatsApp") || strings.Contains(rec.Body.String(), "Copiar URL") || strings.Contains(rec.Body.String(), "Evidence status") || strings.Contains(rec.Body.String(), "Source:") || !strings.Contains(rec.Body.String(), "Guerra de Vietnam") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestCompletedResearchAnswerDoesNotUseQuestionTitleAsAnswer(t *testing.T) {
	store := newTestStore(t)
	job, err := store.CreateResearchJob(contextBackground(), memory.ResearchJob{
		ID:         "research-worldcup",
		Query:      "quien gano el mundial brasil 2014?",
		Status:     "completed",
		Mode:       "background",
		MaxSources: 2,
		Answer:     "¿Quién ganó y qué pasó en el Mundial 2014?\n\nEvidence status: web_verified",
		Confidence: 0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertWebSource(contextBackground(), memory.WebSource{
		ID:          "source-worldcup",
		JobID:       job.ID,
		URL:         "https://sports.example.com/mundial-2014",
		FinalURL:    "https://sports.example.com/mundial-2014",
		Title:       "¿Quién ganó y qué pasó en el Mundial 2014?",
		Snippet:     "Alemania ganó el Mundial Brasil 2014 tras vencer 1-0 a Argentina en la final.",
		Status:      "stored",
		ContentHash: "wc",
		TrustScore:  0.8,
		ByteSize:    10,
		ContentType: "text/html",
	}); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, Options{APIKey: "secret", Store: store, Research: research.Options{Enabled: true, AutoOnKnowledgeGap: true, MinTrustScore: 0.35}})
	rec := serveJSON(t, server, "/v1/chat/completions", `{"model":"aletheia-mikros","messages":[{"role":"user","content":"quien gano el mundial brasil 2014?"}]}`, "secret")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Alemania ganó") || strings.Contains(rec.Body.String(), "¿Quién ganó y qué pasó") || strings.Contains(rec.Body.String(), "Evidence status") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestJobsHideFailedByDefault(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.CreateResearchJob(contextBackground(), memory.ResearchJob{ID: "failed-job", Query: "bad", Status: "failed", Mode: "background"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateResearchJob(contextBackground(), memory.ResearchJob{ID: "queued-job", Query: "ok", Status: "queued", Mode: "background"}); err != nil {
		t.Fatal(err)
	}
	server := newTestServer(t, Options{APIKey: "secret", Store: store})
	hidden := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/aletheia/jobs", nil)
	req.Header.Set("Authorization", "Bearer secret")
	server.Handler().ServeHTTP(hidden, req)
	if hidden.Code != http.StatusOK || strings.Contains(hidden.Body.String(), "failed-job") || !strings.Contains(hidden.Body.String(), "queued-job") {
		t.Fatalf("hidden failed jobs status = %d body=%s", hidden.Code, hidden.Body.String())
	}
	visible := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/v1/aletheia/jobs?include_failed=true", nil)
	req.Header.Set("Authorization", "Bearer secret")
	server.Handler().ServeHTTP(visible, req)
	if visible.Code != http.StatusOK || !strings.Contains(visible.Body.String(), "failed-job") {
		t.Fatalf("visible failed jobs status = %d body=%s", visible.Code, visible.Body.String())
	}
}

func TestResearchEndpointQueuesJob(t *testing.T) {
	store := newTestStore(t)
	server := newTestServer(t, Options{
		APIKey: "secret",
		Store:  store,
		Research: research.Options{
			Enabled:    true,
			MaxSources: 3,
		},
	})
	rec := serveJSON(t, server, "/v1/aletheia/research", `{"query":"what is mcp","mode":"background","max_sources":2}`, "secret")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"queued"`) {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	status := httptest.NewRecorder()
	jobs, err := store.ListResearchJobs(contextBackground(), 10)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/aletheia/research/"+jobs[0].ID, nil)
	req.Header.Set("Authorization", "Bearer secret")
	server.Handler().ServeHTTP(status, req)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), jobs[0].ID) {
		t.Fatalf("status = %d body=%s", status.Code, status.Body.String())
	}
}

func newTestServer(t *testing.T, opts Options) *Server {
	t.Helper()
	return newNamedTestServer(t, "aletheia-mikros", opts)
}

func newTestStore(t *testing.T) *memory.Store {
	t.Helper()
	store, err := memory.Open(filepath.Join(t.TempDir(), "memory.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(contextBackground()); err != nil {
		t.Fatal(err)
	}
	return store
}

func contextBackground() context.Context {
	return context.Background()
}

func newNamedTestServer(t *testing.T, modelName string, opts Options) *Server {
	t.Helper()
	checkpoint := filepath.Join(t.TempDir(), "checkpoint")
	tok := tokenizer.New()
	m, err := model.New(model.Config{
		Name:          modelName,
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
	m.Bias[int('H')] = 10
	if err := m.Save(checkpoint, tok.VocabSize(), 0, 0.1); err != nil {
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

func newMultiModelTestServer(t *testing.T, opts Options) *Server {
	t.Helper()
	root := t.TempDir()
	writeTestCheckpoint(t, filepath.Join(root, "aletheia-mikros"), "aletheia-mikros", 0, []string{
		`{"prompt":"<USER>hola<ASSISTANT>","completion":"Hola desde Mikros.<EOS>"}`,
	})
	writeTestCheckpoint(t, filepath.Join(root, "aletheia-hephaestus"), "aletheia-hephaestus", 1, []string{
		`{"prompt":"<USER>como es el codigo en rust?<ASSISTANT>","completion":"Un ejemplo Rust: fn main() { println!(\"Hola\"); }<EOS>"}`,
		`{"prompt":"<USER>write a small Rust function that adds two numbers<ASSISTANT>","completion":"fn add(a: i32, b: i32) -> i32 { a + b }<EOS>"}`,
	})
	opts.CheckpointsDir = root
	opts.Checkpoint = ""
	if opts.APIKey == "" && opts.Auth == "" {
		opts.APIKey = "secret"
	}
	server, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func newTestServerWithExamples(t *testing.T, examples []string, opts Options) *Server {
	t.Helper()
	checkpoint := filepath.Join(t.TempDir(), "checkpoint")
	writeTestCheckpoint(t, checkpoint, "aletheia-mikros", 1, examples)
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

func writeTestCheckpoint(t *testing.T, checkpoint string, modelName string, step int, examples []string) {
	t.Helper()
	tok := tokenizer.New()
	m, err := model.New(model.Config{
		Name:          modelName,
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
	if err := m.Save(checkpoint, tok.VocabSize(), step, 0.1); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkpoint, chatExamplesFile), []byte(strings.Join(examples, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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
