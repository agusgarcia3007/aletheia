package eval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aletheia/internal/apiserver"
	"aletheia/internal/memory"
	"aletheia/internal/model"
	"aletheia/internal/research"
	"aletheia/internal/tokenizer"
)

func RunMikrosArtifact(ctx context.Context, path string) (BootstrapReport, error) {
	start := time.Now()
	info, err := ValidateSuite(path)
	if err != nil {
		return BootstrapReport{}, err
	}
	server, err := mikrosArtifactEvalServer()
	if err != nil {
		return BootstrapReport{}, err
	}
	_ = ctx
	templates := []struct {
		name     string
		category string
		prompt   string
		want     []string
		forbid   []string
	}{
		{name: "smalltalk_capability", category: "chat", prompt: "que puedes hacer?", want: []string{"Puedo"}, forbid: []string{"job_id=", "Fuentes:", "<ACT_"}},
		{name: "ambiguous_followup", category: "abstention", prompt: "y entonces?", want: []string{"contexto"}, forbid: []string{"Hola. Soy", "Fuentes:"}},
		{name: "javascript_function", category: "coding", prompt: "como hago una funcion en javascript?", want: []string{"JavaScript", "function add"}, forbid: []string{"```go", "Fuentes:", "job_id="}},
		{name: "react_component", category: "coding", prompt: "haz un componente de react", want: []string{"GreetingCard", "tsx"}, forbid: []string{"Fuentes:", "job_id="}},
		{name: "python_js_compare", category: "coding", prompt: "que diferencia hay entre python y js", want: []string{"Python", "JavaScript"}, forbid: []string{"computerhoy", "Fuentes:"}},
		{name: "copa_america_answer", category: "research", prompt: "quiero saber quien gano la ultima copa america", want: []string{"Argentina ganó", "copaamerica.com"}, forbid: []string{"Evidence status", "Todos los campeones"}},
		{name: "worldcup_answer", category: "research", prompt: "quien gano el mundial brasil 2014?", want: []string{"Alemania ganó", "sports.example.com"}, forbid: []string{"¿Quién ganó", "Evidence status"}},
		{name: "future_abstain", category: "abstention", prompt: "quien gano la copa mundial de futbol 2038?", want: []string{"No tengo evidencia suficiente"}, forbid: []string{"web_verified", "job_id="}},
	}
	cases := make([]CaseResult, 0, 160)
	for i := 0; i < 160; i++ {
		tpl := templates[i%len(templates)]
		content := mikrosEvalChat(server, tpl.prompt)
		pass := containsAll(content, tpl.want) && containsNone(content, tpl.forbid) && !linksOnly(content)
		cases = append(cases, CaseResult{
			Name:          fmt.Sprintf("mikros_artifact_%s_%03d", tpl.name, i),
			Category:      tpl.category,
			Status:        passStatus(pass),
			Hallucinated:  !pass && tpl.category != "abstention",
			FalseVerified: strings.Contains(content, "web_verified") && tpl.category == "abstention",
			CitationValid: tpl.category == "research" && pass,
			Improved:      pass,
		})
	}
	return BootstrapReport{
		Suite:   info,
		Cases:   cases,
		Metrics: computeMetrics(cases, time.Since(start)),
	}, nil
}

func mikrosArtifactEvalServer() (*apiserver.Server, error) {
	root, err := os.MkdirTemp("", "aletheia-mikros-artifact-*")
	if err != nil {
		return nil, err
	}
	tok := tokenizer.New()
	m, err := model.New(model.Config{Name: "aletheia-mikros", VocabSize: tok.VocabSize(), ContextLength: 128, NLayers: 1, NHeads: 2, DModel: 16, DFF: 32, Seed: 9})
	if err != nil {
		return nil, err
	}
	checkpoint := filepath.Join(root, "aletheia-mikros")
	if err := m.Save(checkpoint, tok.VocabSize(), 0, 0.1); err != nil {
		return nil, err
	}
	store, err := memory.Open(filepath.Join(root, "memory.sqlite"))
	if err != nil {
		return nil, err
	}
	if err := store.Migrate(context.Background()); err != nil {
		return nil, err
	}
	if err := seedArtifactResearchFixtures(context.Background(), store); err != nil {
		return nil, err
	}
	return apiserver.New(apiserver.Options{
		Checkpoint: checkpoint,
		APIKey:     "eval",
		Store:      store,
		Research:   research.Options{Enabled: true, AutoOnKnowledgeGap: true, MinTrustScore: 0.35},
	})
}

func seedArtifactResearchFixtures(ctx context.Context, store *memory.Store) error {
	copa, err := store.CreateResearchJob(ctx, memory.ResearchJob{
		ID:         "artifact-copa-america",
		Query:      "quien gano la ultima copa america?",
		Status:     "completed",
		Mode:       "background",
		MaxSources: 2,
		Answer:     "Todos los campeones de la CONMEBOL Copa America en la historia\n\nEvidence status: web_verified",
		Confidence: 0.8,
	})
	if err != nil {
		return err
	}
	if _, err := store.UpsertWebSource(ctx, memory.WebSource{
		ID:          "artifact-source-copa",
		JobID:       copa.ID,
		URL:         "https://copaamerica.com/es/novedades/todos-los-campeones-de-la-conmebol-copa-america",
		FinalURL:    "https://copaamerica.com/es/novedades/todos-los-campeones-de-la-conmebol-copa-america",
		Title:       "Todos los campeones de la CONMEBOL Copa America",
		Snippet:     "Argentina ganó la ultima Copa America tras vencer a Colombia en la final.",
		Status:      "stored",
		ContentHash: "artifact-copa",
		TrustScore:  0.8,
		ByteSize:    10,
		ContentType: "text/html",
	}); err != nil {
		return err
	}
	worldcup, err := store.CreateResearchJob(ctx, memory.ResearchJob{
		ID:         "artifact-worldcup",
		Query:      "quien gano el mundial brasil 2014?",
		Status:     "completed",
		Mode:       "background",
		MaxSources: 2,
		Answer:     "¿Quién ganó y qué pasó en el Mundial 2014?\n\nEvidence status: web_verified",
		Confidence: 0.8,
	})
	if err != nil {
		return err
	}
	_, err = store.UpsertWebSource(ctx, memory.WebSource{
		ID:          "artifact-source-worldcup",
		JobID:       worldcup.ID,
		URL:         "https://sports.example.com/mundial-2014",
		FinalURL:    "https://sports.example.com/mundial-2014",
		Title:       "¿Quién ganó y qué pasó en el Mundial 2014?",
		Snippet:     "Alemania ganó el Mundial Brasil 2014 tras vencer 1-0 a Argentina en la final.",
		Status:      "stored",
		ContentHash: "artifact-worldcup",
		TrustScore:  0.8,
		ByteSize:    10,
		ContentType: "text/html",
	})
	return err
}

func linksOnly(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return true
	}
	lines := strings.Split(trimmed, "\n")
	meaningful := 0
	urlLines := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		meaningful++
		if strings.Contains(line, "http://") || strings.Contains(line, "https://") || strings.HasPrefix(strings.ToLower(line), "fuentes") {
			urlLines++
		}
	}
	return meaningful > 0 && urlLines == meaningful
}
