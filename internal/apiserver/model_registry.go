package apiserver

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"aletheia/internal/model"
	"aletheia/internal/runner"
	"aletheia/internal/tokenizer"
)

const (
	hephaestusModelName   = "aletheia-hephaestus"
	DefaultCheckpointsDir = "checkpoints"
	manifestFile          = "manifest.json"
)

type servedModel struct {
	ID           string
	Checkpoint   string
	Manifest     model.Manifest
	Runner       runner.Runner
	ChatExamples []trainedChatExample
}

// loadServedModel loads a checkpoint into a servedModel (runner + manifest +
// chat examples). Shared by initial registry load and hot-swap.
func loadServedModel(checkpoint, alias string, tok *tokenizer.Tokenizer) (*servedModel, error) {
	loaded, manifest, err := model.Load(checkpoint, tok.VocabSize())
	if err != nil {
		return nil, fmt.Errorf("load checkpoint %s: %w", checkpoint, err)
	}
	id := manifest.Config.Name
	if alias != "" {
		id = alias
	}
	if id == "" {
		return nil, fmt.Errorf("checkpoint %s has empty model name", checkpoint)
	}
	examples, err := loadTrainedChatExamples(checkpoint)
	if err != nil {
		return nil, fmt.Errorf("load chat examples for %s: %w", id, err)
	}
	return &servedModel{
		ID:           id,
		Checkpoint:   checkpoint,
		Manifest:     manifest,
		Runner:       runner.New(loaded, tok),
		ChatExamples: examples,
	}, nil
}

func loadModelRegistry(opts Options, tok *tokenizer.Tokenizer) (map[string]*servedModel, []string, string, error) {
	models := map[string]*servedModel{}
	var order []string
	add := func(checkpoint string, alias string) error {
		sm, err := loadServedModel(checkpoint, alias, tok)
		if err != nil {
			return err
		}
		if _, ok := models[sm.ID]; ok {
			return fmt.Errorf("duplicate served model %q", sm.ID)
		}
		models[sm.ID] = sm
		order = append(order, sm.ID)
		return nil
	}

	if opts.CheckpointsDir != "" {
		entries, err := os.ReadDir(opts.CheckpointsDir)
		if err != nil {
			return nil, nil, "", fmt.Errorf("read checkpoints dir %s: %w", opts.CheckpointsDir, err)
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			checkpoint := filepath.Join(opts.CheckpointsDir, entry.Name())
			if _, err := os.Stat(filepath.Join(checkpoint, manifestFile)); err != nil {
				continue
			}
			if err := add(checkpoint, ""); err != nil {
				return nil, nil, "", err
			}
		}
	} else {
		alias := ""
		if opts.ModelName != "" {
			alias = opts.ModelName
		}
		if err := add(opts.Checkpoint, alias); err != nil {
			return nil, nil, "", err
		}
	}
	if len(models) == 0 {
		return nil, nil, "", fmt.Errorf("no checkpoints found")
	}
	defaultModel := opts.ModelName
	if defaultModel == "" {
		if _, ok := models[mikrosModelName]; ok {
			defaultModel = mikrosModelName
		} else {
			defaultModel = order[0]
		}
	}
	if _, ok := models[defaultModel]; !ok {
		return nil, nil, "", fmt.Errorf("default model %q is not loaded", defaultModel)
	}
	return models, order, defaultModel, nil
}

func (s *Server) model(id string) (*servedModel, bool) {
	s.modelsMu.RLock()
	defer s.modelsMu.RUnlock()
	m, ok := s.models[id]
	return m, ok
}

// swapModel hot-loads a checkpoint and replaces the served model under the given
// alias, with no restart. Used to serve a freshly trained checkpoint the instant
// the admin pipeline finishes (and at boot to prefer a persisted trained model).
func (s *Server) swapModel(alias, checkpoint string) error {
	sm, err := loadServedModel(checkpoint, alias, s.tokenizer)
	if err != nil {
		return err
	}
	s.modelsMu.Lock()
	s.models[alias] = sm
	if _, seen := indexOf(s.modelOrder, alias); !seen {
		s.modelOrder = append(s.modelOrder, alias)
	}
	s.modelsMu.Unlock()
	return nil
}

func indexOf(xs []string, target string) (int, bool) {
	for i, x := range xs {
		if x == target {
			return i, true
		}
	}
	return -1, false
}

func (s *Server) routeModel(requested string, messages []chatMessage, tools []chatTool) (*servedModel, bool, error) {
	if requested == "" {
		return nil, false, fmt.Errorf("model is required")
	}
	base, ok := s.model(requested)
	if !ok {
		return nil, false, fmt.Errorf("model %q is not served by this Aletheia instance", requested)
	}
	last := lastUserMessage(messages)
	if requested == mikrosModelName && (isChatActionRequest(last) || (len(tools) > 0 && isToolUseRequest(last))) {
		if coding, ok := s.model(hephaestusModelName); ok {
			return coding, true, nil
		}
	}
	return base, false, nil
}

func (s *Server) publicModelOrder() []string {
	s.modelsMu.RLock()
	defer s.modelsMu.RUnlock()
	if _, ok := s.models[mikrosModelName]; ok {
		return []string{mikrosModelName}
	}
	return append([]string(nil), s.modelOrder...)
}
