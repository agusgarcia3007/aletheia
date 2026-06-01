package apiserver

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"aletheia/internal/memory"
	"aletheia/internal/research"
)

// orderedKeywords returns the meaningful query tokens in order (stopwords and
// 1-2 char tokens dropped), deduped — used to build refined search queries.
func orderedKeywords(query string) []string {
	seen := map[string]bool{}
	var out []string
	for _, tok := range strings.Fields(normalizeBasicChat(query)) {
		if len([]rune(tok)) <= 2 || chatStopWords[tok] || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}

// researchQueries plans the search rounds: the original question first, then
// progressively broader refinements (keywords only, then the most distinctive
// keywords) so a failed first round can still converge. No domain facts — just
// query reshaping.
func researchQueries(query string) []string {
	q := strings.TrimSpace(query)
	out := []string{q}
	kws := orderedKeywords(q)
	if len(kws) >= 2 {
		if joined := strings.Join(kws, " "); !strings.EqualFold(joined, q) {
			out = append(out, joined)
		}
		if len(kws) >= 3 {
			out = append(out, strings.Join(kws[:2], " "))
		}
	}
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}

// researchRoundAnswer runs one research round (cache first, then a fresh,
// time-bounded search) and returns a formatted, grounded answer if confident.
func (s *Server) researchRoundAnswer(ctx context.Context, query string) (string, bool) {
	if answer, ok := s.completedResearchAnswer(ctx, query); ok {
		return answer, true
	}
	job, err := s.store.CreateResearchJob(ctx, memory.ResearchJob{
		Query: query, Status: "running", Mode: "sync", MaxSources: s.research.MaxSources,
	})
	if err != nil {
		return "", false
	}
	wait := 12 * time.Second
	if jt := s.research.JobTimeout; jt > 0 && jt < wait {
		wait = jt
	}
	runCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	result, err := research.NewWorker(s.store, s.research).RunJob(runCtx, job)
	if err != nil {
		job.Status = "queued"
		_ = s.store.UpdateResearchJob(context.Background(), job)
		return "", false
	}
	job.Answer = result.Answer
	job.Confidence = result.Confidence
	job.Status = "completed"
	sources, _ := s.store.WebSourcesByJob(ctx, job.ID)
	claims, _ := s.store.WebClaimsByJob(ctx, job.ID)
	return formatResearchAnswer(query, job, sources, claims)
}

// streamAgenticResearch answers a knowledge-gap query by narrating an iterative
// web search over SSE: it announces each round, refines the query when a round
// comes up empty, and streams the grounded answer once it converges (or an
// honest abstention). Returns false when streaming isn't possible so the caller
// can fall back to the non-streaming path.
func (s *Server) streamAgenticResearch(w http.ResponseWriter, r *http.Request, modelID, query string) bool {
	flusher, ok := w.(http.Flusher)
	if !ok || s.store == nil || !s.research.Enabled {
		return false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	id := s.id("chatcmpl")
	created := time.Now().Unix()
	chunk := func(delta map[string]any, finish any) {
		choice := map[string]any{"index": 0, "delta": delta}
		if finish != nil {
			choice["finish_reason"] = finish
		}
		writeSSEChunk(w, flusher, map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": created,
			"model": modelID, "choices": []map[string]any{choice},
		})
	}
	emit := func(text string) { chunk(map[string]any{"content": text}, nil) }

	chunk(map[string]any{"role": "assistant"}, nil)

	answer := ""
	for i, q := range researchQueries(query) {
		if i == 0 {
			emit("_🔎 Buscando información…_\n\n")
		} else {
			emit("\n\n_Todavía no es suficiente. Afino la búsqueda…_\n\n")
		}
		if a, ok := s.researchRoundAnswer(r.Context(), q); ok && strings.TrimSpace(a) != "" {
			answer = a
			break
		}
	}
	if answer == "" {
		emit("No encontré evidencia suficiente para responder esto con confianza. Prefiero no inventar.")
		s.expert("factual_abstain")
	} else {
		emit(answer)
		s.expert("research_verified")
	}

	chunk(map[string]any{}, "stop")
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
	flusher.Flush()
	return true
}
