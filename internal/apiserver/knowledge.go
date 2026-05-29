package apiserver

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"aletheia/internal/memory"
	"aletheia/internal/retriever"
)

// DefaultKnowledgePath is the local corpus indexed at startup so coding answers
// come from citable knowledge instead of a hardcoded map.
const DefaultKnowledgePath = "knowledge"

// indexKnowledge ingests the local knowledge corpus into the store so it can be
// retrieved at query time. It is best-effort: a missing corpus is not an error.
func (s *Server) indexKnowledge(path string) error {
	if s.store == nil || strings.TrimSpace(path) == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		// A missing corpus is not fatal: coding stays curated + honest-miss.
		return nil
	}
	indexer := retriever.Indexer{Store: s.store}
	_, err := indexer.IndexPath(context.Background(), path, retriever.IndexOptions{})
	return err
}

// codingKnowledgeOrLearn is the KnowledgeFunc wired into the coding answerer.
// It does NOT carry a dictionary of answers; it carries the loop:
//
//  1. already learned? -> return what was learned and stored earlier;
//  2. in the seed corpus / indexed memory? -> return it, cited;
//  3. otherwise, if learning is enabled, say "I don't know yet" and START
//     learning it (a background job fetches, extracts, stores). The next time
//     the question is asked, step 1 answers from memory — it stuck.
//
// When learning is disabled (or no store), it returns ok=false so the answerer
// asks for the missing detail. This is how knowledge grows without code edits.
func (s *Server) codingKnowledgeOrLearn(ctx context.Context, query string) (string, string, bool) {
	// 1) Already learned in a previous turn (persisted in SQLite). Any Go code in
	// the learned answer is checked with the in-process parser before we present
	// it as knowledge — verifier-first applies to learned content too.
	if answer, ok := s.completedResearchAnswer(ctx, query); ok {
		return verifyLearnedCoding(query, answer), "", true
	}
	// 2) Verified seed corpus / indexed knowledge.
	if answer, citation, ok := s.codingRetrieval(ctx, query); ok {
		return answer, citation, true
	}
	// 3) Learn on demand.
	if s.store == nil || !s.research.Enabled {
		return "", "", false
	}
	if job, ok := s.matchingLearningJob(ctx, query); ok {
		return "Sigo aprendiendo eso (job_id=" + job.ID + "). Volvé a preguntar en unos segundos y te respondo con lo aprendido y su fuente.", "", true
	}
	job, err := s.enqueueResearch(ctx, query, "background", 0)
	if err != nil {
		return "", "", false
	}
	return "No conozco eso todavía, así que lo estoy aprendiendo ahora (job_id=" + job.ID + "). Volvé a preguntar en unos segundos y te respondo con lo aprendido y su fuente.", "", true
}

// matchingLearningJob reports a non-failed job whose query overlaps this one, so
// we never enqueue duplicate learning for the same question.
func (s *Server) matchingLearningJob(ctx context.Context, query string) (memory.ResearchJob, bool) {
	if s.store == nil {
		return memory.ResearchJob{}, false
	}
	jobs, err := s.store.ListResearchJobs(ctx, 50)
	if err != nil {
		return memory.ResearchJob{}, false
	}
	qTokens := meaningfulChatTokens(query)
	if len(qTokens) == 0 {
		return memory.ResearchJob{}, false
	}
	need := requiredMeaningfulOverlap(len(qTokens))
	for _, job := range jobs {
		if job.Status == "failed" {
			continue
		}
		if meaningfulOverlap(qTokens, meaningfulChatTokens(job.Query)) >= need {
			return job, true
		}
	}
	return memory.ResearchJob{}, false
}

// codingRetrieval returns a verified worked answer from the indexed coding
// corpus, or ok=false. It only trusts chunks from the coding knowledge corpus
// and requires real keyword overlap with the question.
func (s *Server) codingRetrieval(ctx context.Context, query string) (string, string, bool) {
	if s.store == nil || strings.TrimSpace(query) == "" {
		return "", "", false
	}
	hits, err := s.retriever.Search(ctx, query, retriever.SearchOptions{TopK: 8})
	if err != nil {
		return "", "", false
	}
	qTokens := meaningfulChatTokens(query)
	need := requiredMeaningfulOverlap(len(qTokens))
	for _, hit := range hits {
		if !isCodingKnowledgePath(hit.Path) {
			continue
		}
		if meaningfulOverlap(qTokens, meaningfulChatTokens(hit.Text)) < need {
			continue
		}
		body := cleanKnowledgeChunk(hit.Text)
		if body == "" {
			continue
		}
		return body, knowledgeCitation(hit.Path), true
	}
	return "", "", false
}

// verifyLearnedCoding annotates a learned coding answer with the result of
// parsing any Go code it contains. Valid Go is marked verified; invalid Go is
// flagged so the user knows it was learned but did not pass the parser.
func verifyLearnedCoding(query, answer string) string {
	blocks := extractCodeBlocks(answer)
	if len(blocks) == 0 {
		return answer
	}
	mentionsGo := strings.Contains(strings.ToLower(query), "go") || strings.Contains(strings.ToLower(query), "golang")
	for _, b := range blocks {
		if b.Lang != "go" && !(b.Lang == "" && mentionsGo) {
			continue
		}
		if goSnippetParses(b.Code) {
			return answer + "\n\n(Código Go verificado: parsea correctamente.)"
		}
		return answer + "\n\n(Aprendido de la web; el código Go no pasó el parser, tomalo con cuidado.)"
	}
	return answer
}

func isCodingKnowledgePath(path string) bool {
	normalized := filepath.ToSlash(path)
	return strings.Contains(normalized, "knowledge/coding/")
}

// cleanKnowledgeChunk returns the markdown body without trailing index noise.
func cleanKnowledgeChunk(text string) string {
	return strings.TrimSpace(text)
}

// knowledgeCitation turns a corpus path into a short, public-friendly citation.
func knowledgeCitation(path string) string {
	normalized := filepath.ToSlash(path)
	if idx := strings.Index(normalized, "knowledge/"); idx >= 0 {
		return normalized[idx:]
	}
	return filepath.Base(path)
}
