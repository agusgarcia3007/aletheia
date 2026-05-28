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
	"aletheia/internal/selector"
	skillpkg "aletheia/internal/skills"
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
	Planner         Planner
	Selector        ActionSelector
	MaxSteps        int
	SearchStrategy  string
	BeamWidth       int
	MaxDepth        int
	UseSkills       bool
	VerifierNames   []string
}

type Result struct {
	Task           Task
	RepoPath       string
	Initial        verifier.Evidence
	Final          verifier.Evidence
	Diff           string
	Patched        bool
	Trace          []TraceEntry
	FinalStatus    string
	SkillUsed      string
	ToolCalls      int
	InitialSkipped bool
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

	planner := s.Planner
	if planner == nil {
		planner = MockPlanner{}
	}
	selectorProvided := s.Selector != nil
	actionSelector := s.Selector
	if actionSelector == nil {
		actionSelector = selector.HeuristicSelector{}
	}
	vm := VM{
		Store:           store,
		EpisodeID:       episodeID,
		VerifierTimeout: s.VerifierTimeout,
		VerifierNames:   s.VerifierNames,
	}
	var state State
	var skillUsed string
	initialSkipped := false
	skillFailed := false
	trigger, triggerOK, err := skillpkg.DetectTrigger(repoPath, task.Success)
	if err != nil {
		return Result{}, err
	}
	if s.UseSkills && triggerOK {
		if skill, ok, err := store.BestSkillByTrigger(ctx, trigger); err != nil {
			return Result{}, err
		} else if ok && skill.SuccessRate > 0 {
			snapshot, err := snapshotSkillFiles(repoPath, trigger)
			if err != nil {
				return Result{}, err
			}
			skillState, success, err := vm.RunSkill(ctx, task, repoPath, skill, s.MaxSteps)
			if err != nil {
				if restoreErr := restoreSkillFiles(snapshot); restoreErr != nil {
					return Result{}, restoreErr
				}
				return Result{}, err
			}
			if success {
				state = skillState
				skillUsed = skill.Name
				initialSkipped = true
				if err := store.UpdateSkillSuccessRate(ctx, skill.ID, 1); err != nil {
					return Result{}, err
				}
			} else {
				if err := restoreSkillFiles(snapshot); err != nil {
					return Result{}, err
				}
				skillFailed = true
				if err := store.UpdateSkillSuccessRate(ctx, skill.ID, 0); err != nil {
					return Result{}, err
				}
			}
		}
	}
	if state.Goal == "" {
		switch s.SearchStrategy {
		case "", "greedy":
			state, err = vm.Run(ctx, task, repoPath, planner, actionSelector, s.MaxSteps)
		case "beam":
			state, err = s.runBeam(ctx, task, repoPath, planner, actionSelector, selectorProvided, vm)
		default:
			return Result{}, fmt.Errorf("unsupported search strategy %q", s.SearchStrategy)
		}
	}
	if err != nil {
		return Result{}, err
	}
	if !skillFailed && skillUsed == "" && triggerOK && (state.Verified || state.lastVerifierStatus() == "pass") {
		if def, ok := skillpkg.Compress(traceActions(state.ActionTrace), true); ok {
			def.Trigger = trigger
			if _, err := store.UpsertSkill(ctx, memory.Skill{
				Name:           def.Name,
				Trigger:        def.Trigger,
				ActionSequence: def.ActionSequence,
				SuccessRate:    1,
			}); err != nil {
				return Result{}, err
			}
		}
	}

	reward := 0.0
	episodeResult := state.FinalStatus
	if state.Verified || state.lastVerifierStatus() == "pass" {
		reward = 1
		if episodeResult == "" {
			episodeResult = "verified"
		}
	}
	if episodeResult == "" {
		episodeResult = "failed"
	}
	if err := store.UpdateEpisodeResult(ctx, episodeID, episodeResult, reward); err != nil {
		return Result{}, err
	}

	result := Result{
		Task:           task,
		RepoPath:       repoPath,
		Diff:           state.Diff,
		Patched:        state.Verified,
		Trace:          state.ActionTrace,
		FinalStatus:    state.FinalStatus,
		SkillUsed:      skillUsed,
		ToolCalls:      state.ToolCalls,
		InitialSkipped: initialSkipped,
	}
	if len(state.VerifierResults) > 0 {
		result.Initial = state.VerifierResults[0]
		result.Final = state.VerifierResults[len(state.VerifierResults)-1]
	}
	return result, nil
}

type skillFileSnapshot struct {
	path string
	mode os.FileMode
	data []byte
}

func snapshotSkillFiles(repoPath string, trigger string) ([]skillFileSnapshot, error) {
	files, ok := skillpkg.FilesForTrigger(trigger)
	if !ok {
		return nil, nil
	}
	snapshot := make([]skillFileSnapshot, 0, len(files))
	for _, rel := range files {
		path := filepath.Join(repoPath, rel)
		info, err := os.Stat(path)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		snapshot = append(snapshot, skillFileSnapshot{
			path: path,
			mode: info.Mode().Perm(),
			data: data,
		})
	}
	return snapshot, nil
}

func restoreSkillFiles(snapshot []skillFileSnapshot) error {
	for _, file := range snapshot {
		if err := os.WriteFile(file.path, file.data, file.mode); err != nil {
			return err
		}
	}
	return nil
}

func traceActions(trace []TraceEntry) []string {
	actions := make([]string, 0, len(trace))
	for _, entry := range trace {
		actions = append(actions, string(entry.Action))
	}
	return actions
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
