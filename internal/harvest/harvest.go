package harvest

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"aletheia/internal/memory"
)

// chatTrainingExample matches the JSONL schema the trainer consumes
// (internal/training.LoadDataset): a prompt/completion pair.
type chatTrainingExample struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

// maxPairBytes caps the byte length of a (prompt+completion) pair so it fits the
// default training context (1024 byte-tokens) with margin. A pathologically long
// answer is skipped rather than aborting the whole training run at load time.
const maxPairBytes = 900

// nonAnswerPrefixes are stub/abstention replies that must never become
// generative training targets — we want the model to learn to ANSWER, not to
// reproduce "no sé todavía" or a research stub.
var nonAnswerPrefixes = []string{
	"no tengo evidencia", "estoy buscando", "no conozco", "sigo aprendiendo",
	"no hay evidencia", "todavia no tengo", "todavía no tengo", "no alcanza con",
	"necesito un poco", "no debo inventar",
}

// HarvestChatDataset turns Aletheia's own verified research jobs into a
// generative training set: each completed (query -> verified answer) becomes a
// {prompt, completion} pair. There is NO external teacher — the targets are the
// verified, evidence-grounded answers the loop already produced. This is the
// self-improving loop feeding the language model: harvest -> train -> serve
// grounded -> harvest more. Returns the number of examples written.
func HarvestChatDataset(ctx context.Context, store *memory.Store, outPath string, minConfidence float64) (int, error) {
	jobs, err := store.ListResearchJobs(ctx, 100000)
	if err != nil {
		return 0, err
	}
	if dir := filepath.Dir(outPath); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return 0, err
		}
	}
	f, err := os.Create(outPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	w := bufio.NewWriter(f)

	seen := map[string]bool{}
	count := 0
	for _, job := range jobs {
		query := strings.TrimSpace(job.Query)
		answer := stripSourcesBlock(strings.TrimSpace(job.Answer))
		if job.Status != "completed" || query == "" || answer == "" {
			continue
		}
		if job.Confidence < minConfidence {
			continue
		}
		if isNonAnswer(answer) || strings.Contains(answer, "job_id=") {
			continue
		}
		if len([]rune(answer)) < 25 {
			continue
		}
		ex := chatTrainingExample{
			Prompt:     "<USER>" + query + "<ASSISTANT>",
			Completion: answer + "<EOS>",
		}
		// Skip pairs that would overflow the training context once byte-tokenized,
		// so one long answer never aborts the run.
		if len(ex.Prompt)+len(ex.Completion) > maxPairBytes {
			continue
		}
		key := strings.ToLower(query)
		if seen[key] {
			continue
		}
		seen[key] = true

		raw, err := json.Marshal(ex)
		if err != nil {
			return 0, err
		}
		if _, err := w.Write(raw); err != nil {
			return 0, err
		}
		if err := w.WriteByte('\n'); err != nil {
			return 0, err
		}
		count++
	}
	if err := w.Flush(); err != nil {
		return 0, err
	}
	return count, nil
}

func isNonAnswer(answer string) bool {
	low := strings.ToLower(answer)
	for _, p := range nonAnswerPrefixes {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	return false
}

// stripSourcesBlock removes the "Fuentes:" citation block: the model learns to
// write the answer prose; citations are attached deterministically at serve
// time, never generated (so the model can't invent a source).
func stripSourcesBlock(answer string) string {
	if idx := strings.Index(answer, "\n\nFuentes:"); idx >= 0 {
		return strings.TrimSpace(answer[:idx])
	}
	return strings.TrimSpace(answer)
}
