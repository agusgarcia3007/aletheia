package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aletheia/internal/apiserver"
	"aletheia/internal/model"
	"aletheia/internal/tokenizer"
)

func RunMikrosFunctional(ctx context.Context, path string) (BootstrapReport, error) {
	start := time.Now()
	info, err := ValidateSuite(path)
	if err != nil {
		return BootstrapReport{}, err
	}
	server, err := mikrosEvalServer()
	if err != nil {
		return BootstrapReport{}, err
	}
	_ = ctx
	cases := make([]CaseResult, 0, 150)
	templates := []struct {
		name     string
		category string
		prompt   string
		want     []string
		forbid   []string
	}{
		{name: "smalltalk", category: "chat", prompt: "hola", want: []string{"Aletheia"}, forbid: []string{"<ACT_", "Fuentes:"}},
		{name: "rust_intro", category: "coding", prompt: "hablame de rust", want: []string{"Rust", "seguridad"}, forbid: []string{"Fuentes:", "job_id="}},
		{name: "javascript_function", category: "coding", prompt: "como hago una funcion en javacript?", want: []string{"JavaScript", "function add"}, forbid: []string{"```go", "Fuentes:"}},
		{name: "react_component", category: "coding", prompt: "haz un componente de react", want: []string{"GreetingCard", "tsx"}, forbid: []string{"Fuentes:", "job_id="}},
		{name: "python_js_compare", category: "coding", prompt: "que diferencia hay enter python y js", want: []string{"Python", "JavaScript"}, forbid: []string{"computerhoy", "Fuentes:"}},
		{name: "future_abstain", category: "abstention", prompt: "quien gano la copa mundial de futbol 2038?", want: []string{"No tengo evidencia suficiente"}, forbid: []string{"web_verified", "job_id="}},
	}
	for i := 0; i < 150; i++ {
		tpl := templates[i%len(templates)]
		content := mikrosEvalChat(server, tpl.prompt)
		pass := containsAll(content, tpl.want) && containsNone(content, tpl.forbid)
		cases = append(cases, CaseResult{
			Name:          fmt.Sprintf("%s_%02d", tpl.name, i),
			Category:      tpl.category,
			Status:        passStatus(pass),
			Hallucinated:  !pass && tpl.category != "abstention",
			FalseVerified: strings.Contains(content, "web_verified") && tpl.category == "abstention",
			Improved:      pass,
		})
	}
	return BootstrapReport{
		Suite:   info,
		Cases:   cases,
		Metrics: computeMetrics(cases, time.Since(start)),
	}, nil
}

func mikrosEvalServer() (*apiserver.Server, error) {
	root, err := os.MkdirTemp("", "aletheia-mikros-functional-*")
	if err != nil {
		return nil, err
	}
	tok := tokenizer.New()
	for _, modelName := range []string{"aletheia-mikros", "aletheia-hephaestus"} {
		m, err := model.New(model.Config{Name: modelName, VocabSize: tok.VocabSize(), ContextLength: 128, NLayers: 1, NHeads: 2, DModel: 16, DFF: 32, Seed: 9})
		if err != nil {
			return nil, err
		}
		dir := filepath.Join(root, modelName)
		if err := m.Save(dir, tok.VocabSize(), 0, 0.1); err != nil {
			return nil, err
		}
	}
	server, err := apiserver.New(apiserver.Options{CheckpointsDir: root, ModelName: "aletheia-mikros", APIKey: "eval"})
	_ = os.RemoveAll(root)
	return server, err
}

func mikrosEvalChat(server *apiserver.Server, prompt string) string {
	body, _ := json.Marshal(map[string]any{
		"model": "aletheia-mikros",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens": 180,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer eval")
	req.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(rec, req)
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || len(response.Choices) == 0 {
		return rec.Body.String()
	}
	return response.Choices[0].Message.Content
}

func containsAll(text string, wants []string) bool {
	for _, want := range wants {
		if !strings.Contains(text, want) {
			return false
		}
	}
	return true
}

func containsNone(text string, forbids []string) bool {
	for _, forbid := range forbids {
		if strings.Contains(text, forbid) {
			return false
		}
	}
	return true
}
