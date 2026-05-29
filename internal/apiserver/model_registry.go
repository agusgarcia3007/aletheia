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

func loadModelRegistry(opts Options, tok *tokenizer.Tokenizer) (map[string]*servedModel, []string, string, error) {
	models := map[string]*servedModel{}
	var order []string
	add := func(checkpoint string, alias string) error {
		loaded, manifest, err := model.Load(checkpoint, tok.VocabSize())
		if err != nil {
			return fmt.Errorf("load checkpoint %s: %w", checkpoint, err)
		}
		id := manifest.Config.Name
		if alias != "" {
			id = alias
		}
		if id == "" {
			return fmt.Errorf("checkpoint %s has empty model name", checkpoint)
		}
		if _, ok := models[id]; ok {
			return fmt.Errorf("duplicate served model %q", id)
		}
		examples, err := loadTrainedChatExamples(checkpoint)
		if err != nil {
			return fmt.Errorf("load chat examples for %s: %w", id, err)
		}
		models[id] = &servedModel{
			ID:           id,
			Checkpoint:   checkpoint,
			Manifest:     manifest,
			Runner:       runner.New(loaded, tok),
			ChatExamples: examples,
		}
		order = append(order, id)
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
	m, ok := s.models[id]
	return m, ok
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
	if _, ok := s.models[mikrosModelName]; ok {
		return []string{mikrosModelName}
	}
	return append([]string(nil), s.modelOrder...)
}
