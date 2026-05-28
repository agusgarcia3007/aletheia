package cognitivevm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aletheia/internal/memory"
	"aletheia/internal/verifier"
)

type Task struct {
	Goal    string `json:"goal"`
	Repo    string `json:"repo"`
	Success string `json:"success"`
}

type Solver struct {
	DBPath          string
	VerifierTimeout time.Duration
}

type Result struct {
	Task     Task
	RepoPath string
	Initial  verifier.Evidence
	Final    verifier.Evidence
	Diff     string
	Patched  bool
}

func (s Solver) SolveFile(ctx context.Context, taskPath string, workingDir string) (Result, error) {
	taskBytes, err := os.ReadFile(taskPath)
	if err != nil {
		return Result{}, err
	}

	var task Task
	if err := json.Unmarshal(taskBytes, &task); err != nil {
		return Result{}, fmt.Errorf("parse task: %w", err)
	}
	return s.Solve(ctx, task, workingDir)
}

func (s Solver) Solve(ctx context.Context, task Task, workingDir string) (Result, error) {
	if task.Goal == "" {
		return Result{}, fmt.Errorf("task goal is required")
	}
	if task.Repo == "" {
		return Result{}, fmt.Errorf("task repo is required")
	}
	if !verifier.IsAllowed(task.Success) {
		return Result{}, fmt.Errorf("unsupported success command %q", task.Success)
	}

	repoPath := task.Repo
	if !filepath.IsAbs(repoPath) {
		repoPath = filepath.Join(workingDir, repoPath)
	}
	repoPath = filepath.Clean(repoPath)

	store, err := memory.Open(s.DBPath)
	if err != nil {
		return Result{}, err
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		return Result{}, err
	}

	episodeID, err := store.CreateEpisode(ctx, task.Goal)
	if err != nil {
		return Result{}, err
	}

	initial, err := verifier.RunSuccess(ctx, repoPath, task.Success, s.VerifierTimeout)
	if err != nil {
		return Result{}, err
	}
	if _, err := store.RecordEvidence(ctx, toMemoryEvidence(episodeID, initial)); err != nil {
		return Result{}, err
	}

	result := Result{
		Task:     task,
		RepoPath: repoPath,
		Initial:  initial,
		Final:    initial,
	}
	if initial.Status == "pass" {
		if err := store.UpdateEpisodeResult(ctx, episodeID, "already_passed", 1); err != nil {
			return Result{}, err
		}
		return result, nil
	}

	diff, err := applyToyPatch(repoPath)
	if err != nil {
		_ = store.UpdateEpisodeResult(ctx, episodeID, "patch_failed", 0)
		return Result{}, err
	}
	result.Diff = diff
	result.Patched = true

	final, err := verifier.RunSuccess(ctx, repoPath, task.Success, s.VerifierTimeout)
	if err != nil {
		return Result{}, err
	}
	result.Final = final
	if _, err := store.RecordEvidence(ctx, toMemoryEvidence(episodeID, final)); err != nil {
		return Result{}, err
	}

	reward := 0.0
	episodeResult := "failed"
	if final.Status == "pass" {
		reward = 1
		episodeResult = "verified"
	}
	if err := store.UpdateEpisodeResult(ctx, episodeID, episodeResult, reward); err != nil {
		return Result{}, err
	}
	return result, nil
}

func toMemoryEvidence(episodeID int64, ev verifier.Evidence) memory.Evidence {
	return memory.Evidence{
		EpisodeID: episodeID,
		Verifier:  ev.Verifier,
		Status:    ev.Status,
		Score:     ev.Score,
		Stdout:    ev.Stdout,
		Stderr:    ev.Stderr,
		Timestamp: ev.Timestamp,
	}
}

func applyToyPatch(repoPath string) (string, error) {
	path := filepath.Join(repoPath, "calculator.go")
	oldBytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	oldText := string(oldBytes)
	newText := strings.Replace(oldText, "return a - b", "return a + b", 1)
	if newText == oldText {
		return "", fmt.Errorf("toy patch pattern not found in %s", path)
	}

	diff := unifiedDiff("calculator.go", oldText, newText)
	if err := os.WriteFile(path, []byte(newText), 0o644); err != nil {
		return "", err
	}
	return diff, nil
}

func unifiedDiff(path string, oldText string, newText string) string {
	oldLines := splitLines(oldText)
	newLines := splitLines(newText)
	first := 0
	for first < len(oldLines) && first < len(newLines) && oldLines[first] == newLines[first] {
		first++
	}
	if first == len(oldLines) && first == len(newLines) {
		return ""
	}

	start := first - 3
	if start < 0 {
		start = 0
	}
	oldEnd := len(oldLines)
	newEnd := len(newLines)
	for oldEnd > start && newEnd > start && oldLines[oldEnd-1] == newLines[newEnd-1] {
		oldEnd--
		newEnd--
	}
	oldDisplayEnd := oldEnd + 3
	if oldDisplayEnd > len(oldLines) {
		oldDisplayEnd = len(oldLines)
	}
	newDisplayEnd := newEnd + 3
	if newDisplayEnd > len(newLines) {
		newDisplayEnd = len(newLines)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n", path)
	fmt.Fprintf(&b, "+++ b/%s\n", path)
	fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", start+1, oldDisplayEnd-start, start+1, newDisplayEnd-start)
	i, j := start, start
	for i < oldDisplayEnd || j < newDisplayEnd {
		if i < oldDisplayEnd && j < newDisplayEnd && oldLines[i] == newLines[j] {
			b.WriteString(" " + oldLines[i])
			i++
			j++
			continue
		}
		if i < oldEnd {
			b.WriteString("-" + oldLines[i])
			i++
		}
		if j < newEnd {
			b.WriteString("+" + newLines[j])
			j++
		}
		if i >= oldEnd && j >= newEnd {
			for i < oldDisplayEnd && j < newDisplayEnd && oldLines[i] == newLines[j] {
				b.WriteString(" " + oldLines[i])
				i++
				j++
			}
		}
	}
	return b.String()
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	parts := strings.SplitAfter(text, "\n")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}
