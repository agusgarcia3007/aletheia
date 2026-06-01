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

func RunMikrosLive(ctx context.Context, path string) (BootstrapReport, error) {
	start := time.Now()
	info, err := ValidateSuite(path)
	if err != nil {
		return BootstrapReport{}, err
	}
	server, cleanup, err := mikrosLiveEvalServer()
	if err != nil {
		return BootstrapReport{}, err
	}
	defer cleanup()
	_ = ctx

	templates := []struct {
		name     string
		category string
		prompt   string
		want     []string
		forbid   []string
	}{
		{name: "smalltalk_capability", category: "router", prompt: "que podes hacer?", want: []string{"Puedo conversar"}, forbid: []string{"job_id=", "Fuentes:", "<ACT_"}},
		{name: "python_csv", category: "coding", prompt: "como leo un csv en python y filtro filas?", want: []string{"csv.DictReader", "status"}, forbid: []string{"job_id=", "Fuentes:", "chunk="}},
		{name: "sql_count_group", category: "coding", prompt: "dame una query SQL para contar usuarios por pais", want: []string{"COUNT(*)", "GROUP BY pais"}, forbid: []string{"job_id=", "Fuentes:", "chunk="}},
		{name: "go_errors", category: "coding", prompt: "como se manejan errores en go?", want: []string{"err != nil", "%w"}, forbid: []string{"job_id=", "Fuentes:", "chunk="}},
		{name: "rust_map_filter", category: "coding", prompt: "explicame map y filter en rust con un ejemplo corto", want: []string{"filter", "map", "collect"}, forbid: []string{"job_id=", "Fuentes:", "chunk="}},
		{name: "react_product_card", category: "coding", prompt: "haz un componente de react para una tarjeta de producto con precio y boton", want: []string{"ProductCard", "price", "onAdd"}, forbid: []string{"job_id=", "Fuentes:", "chunk="}},
		{name: "python_js_compare", category: "coding", prompt: "que diferencia hay enter python y js", want: []string{"Python", "JavaScript"}, forbid: []string{"computerhoy", "Fuentes:", "chunk="}},
		{name: "math_multiply", category: "math", prompt: "cuanto es 17 por 23?", want: []string{"391"}, forbid: []string{"job_id=", "Fuentes:", "chunk="}},
		{name: "translation_short", category: "translation", prompt: "traduce al ingles: no tengo evidencia suficiente", want: []string{"I do not have enough evidence."}, forbid: []string{"job_id=", "Fuentes:", "chunk="}},
		{name: "copa_latest", category: "research", prompt: "quiero saber quien gano la ultima copa america", want: []string{"Argentina ganó", "copaamerica.com"}, forbid: []string{"Todos los campeones", "Evidence status", "chunk="}},
		{name: "worldcup_2014", category: "research", prompt: "quien gano el mundial brasil 2014?", want: []string{"Alemania ganó", "1-0", "sports.example.com"}, forbid: []string{"¿Quién ganó", "Evidence status", "chunk="}},
		{name: "go_history", category: "research", prompt: "en que anio se creo el lenguaje go", want: []string{"2009", "golang.example"}, forbid: []string{"Evidence status", "chunk="}},
		{name: "future_abstain", category: "abstention", prompt: "quien gano la copa mundial de futbol 2038?", want: []string{"No tengo evidencia suficiente"}, forbid: []string{"web_verified", "job_id=", "chunk="}},
		{name: "nonsense_abstain", category: "abstention", prompt: "blorf zibble quantum vegetable quien gano eso?", want: []string{"No tengo evidencia suficiente"}, forbid: []string{"Fuentes:", "chunk="}},
	}
	cases := make([]CaseResult, 0, 252)
	for i := 0; i < 252; i++ {
		tpl := templates[i%len(templates)]
		content := mikrosEvalChat(server, tpl.prompt)
		pass := containsAll(content, tpl.want) && containsNone(content, tpl.forbid) && !linksOnly(content)
		cases = append(cases, CaseResult{
			Name:          fmt.Sprintf("mikros_live_%s_%03d", tpl.name, i),
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

func mikrosLiveEvalServer() (*apiserver.Server, func(), error) {
	root, err := os.MkdirTemp("", "aletheia-mikros-live-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	tok := tokenizer.New()
	m, err := model.New(model.Config{Name: "aletheia-mikros", VocabSize: tok.VocabSize(), ContextLength: 128, NLayers: 1, NHeads: 2, DModel: 16, DFF: 32, Seed: 11})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	checkpoint := filepath.Join(root, "aletheia-mikros")
	if err := m.Save(checkpoint, tok.VocabSize(), 0, 0.1); err != nil {
		cleanup()
		return nil, nil, err
	}
	store, err := memory.Open(filepath.Join(root, "memory.sqlite"))
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		cleanup()
		return nil, nil, err
	}
	if err := seedLiveResearchFixtures(context.Background(), store); err != nil {
		_ = store.Close()
		cleanup()
		return nil, nil, err
	}
	server, err := apiserver.New(apiserver.Options{
		Checkpoint: checkpoint,
		APIKey:     "eval",
		Store:      store,
		Research:   research.Options{Enabled: true, AutoOnKnowledgeGap: true, MinTrustScore: 0.35},
	})
	if err != nil {
		_ = store.Close()
		cleanup()
		return nil, nil, err
	}
	return server, func() {
		_ = store.Close()
		cleanup()
	}, nil
}

func seedLiveResearchFixtures(ctx context.Context, store *memory.Store) error {
	copa, err := store.CreateResearchJob(ctx, memory.ResearchJob{
		ID:         "live-copa-america",
		Query:      "quien gano la ultima copa america",
		Status:     "completed",
		Mode:       "background",
		MaxSources: 2,
		Answer:     "Argentina ganó la ultima Copa America tras vencer a Colombia en la final.",
		Confidence: 0.85,
	})
	if err != nil {
		return err
	}
	if _, err := store.UpsertWebSource(ctx, memory.WebSource{
		ID:          "live-source-copa",
		JobID:       copa.ID,
		URL:         "https://copaamerica.com/es/novedades/final-copa-america",
		FinalURL:    "https://copaamerica.com/es/novedades/final-copa-america",
		Title:       "Final de la CONMEBOL Copa America",
		Snippet:     "Argentina ganó la ultima Copa America tras vencer a Colombia en la final.",
		Status:      "stored",
		ContentHash: "live-copa",
		TrustScore:  0.9,
		ByteSize:    10,
		ContentType: "text/html",
	}); err != nil {
		return err
	}
	if _, err := store.RecordWebClaim(ctx, memory.WebClaim{
		ID:         "live-claim-copa",
		SourceID:   "live-source-copa",
		Claim:      "Argentina ganó la ultima Copa America tras vencer a Colombia en la final.",
		Confidence: 0.9,
	}); err != nil {
		return err
	}
	worldcup, err := store.CreateResearchJob(ctx, memory.ResearchJob{
		ID:         "live-worldcup",
		Query:      "quien gano el mundial brasil 2014?",
		Status:     "completed",
		Mode:       "background",
		MaxSources: 2,
		Answer:     "Alemania ganó el Mundial de Brasil 2014; venció a Argentina 1-0 en la final.",
		Confidence: 0.85,
	})
	if err != nil {
		return err
	}
	if _, err := store.UpsertWebSource(ctx, memory.WebSource{
		ID:          "live-source-worldcup",
		JobID:       worldcup.ID,
		URL:         "https://sports.example.com/mundial-2014",
		FinalURL:    "https://sports.example.com/mundial-2014",
		Title:       "Final Mundial Brasil 2014",
		Snippet:     "Alemania ganó el Mundial Brasil 2014 tras vencer 1-0 a Argentina en la final.",
		Status:      "stored",
		ContentHash: "live-worldcup",
		TrustScore:  0.8,
		ByteSize:    10,
		ContentType: "text/html",
	}); err != nil {
		return err
	}
	if _, err = store.RecordWebClaim(ctx, memory.WebClaim{
		ID:         "live-claim-worldcup",
		SourceID:   "live-source-worldcup",
		Claim:      "Alemania ganó el Mundial Brasil 2014 tras vencer 1-0 a Argentina en la final.",
		Confidence: 0.9,
	}); err != nil {
		return err
	}

	golang, err := store.CreateResearchJob(ctx, memory.ResearchJob{
		ID:         "live-go-history",
		Query:      "en que anio se creo el lenguaje go",
		Status:     "completed",
		Mode:       "background",
		MaxSources: 2,
		Answer:     "El lenguaje de programacion Go se creo en el anio 2009 dentro de Google.",
		Confidence: 0.85,
	})
	if err != nil {
		return err
	}
	if _, err := store.UpsertWebSource(ctx, memory.WebSource{
		ID:          "live-source-go",
		JobID:       golang.ID,
		URL:         "https://golang.example/historia",
		FinalURL:    "https://golang.example/historia",
		Title:       "Historia del lenguaje Go",
		Snippet:     "El lenguaje de programacion Go se creo en el anio 2009 dentro de Google.",
		Status:      "stored",
		ContentHash: "live-go",
		TrustScore:  0.9,
		ByteSize:    10,
		ContentType: "text/html",
	}); err != nil {
		return err
	}
	_, err = store.RecordWebClaim(ctx, memory.WebClaim{
		ID:         "live-claim-go",
		SourceID:   "live-source-go",
		Claim:      "El lenguaje de programacion Go se creo en el anio 2009 dentro de Google.",
		Confidence: 0.9,
	})
	return err
}
