package learning

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aletheia/internal/eval"
	"aletheia/internal/memory"
	"aletheia/internal/selector"
)

type Options struct {
	DBPath           string
	SuitePath        string
	OutDir           string
	TrainSelectorOut string
	Epochs           int
	LearningRate     float64
}

type Report struct {
	DBPath                string               `json:"db_path"`
	OutDir                string               `json:"out_dir"`
	SelectorExamples      int                  `json:"selector_examples"`
	VerifiedTrajectories  int                  `json:"verified_trajectories"`
	ResearchExamples      int                  `json:"research_examples"`
	Skills                int                  `json:"skills"`
	SelectorDatasetPath   string               `json:"selector_dataset_path"`
	TrajectoryDatasetPath string               `json:"trajectory_dataset_path"`
	ResearchDatasetPath   string               `json:"research_dataset_path"`
	SkillsPath            string               `json:"skills_path"`
	SelectorCheckpoint    string               `json:"selector_checkpoint,omitempty"`
	SelectorTrainReport   selector.TrainReport `json:"selector_train_report,omitempty"`
	EvalBefore            eval.Metrics         `json:"eval_before"`
	EvalAfter             eval.Metrics         `json:"eval_after"`
}

func Run(ctx context.Context, opts Options) (Report, error) {
	if opts.DBPath == "" {
		opts.DBPath = memory.DefaultDBPath
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return Report{}, fmt.Errorf("--out is required")
	}
	if opts.Epochs <= 0 {
		opts.Epochs = 300
	}
	if opts.LearningRate == 0 {
		opts.LearningRate = 0.1
	}
	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return Report{}, err
	}

	report := Report{
		DBPath:                opts.DBPath,
		OutDir:                opts.OutDir,
		SelectorDatasetPath:   filepath.Join(opts.OutDir, "selector_examples.jsonl"),
		TrajectoryDatasetPath: filepath.Join(opts.OutDir, "verified_trajectories.jsonl"),
		ResearchDatasetPath:   filepath.Join(opts.OutDir, "research_answers.jsonl"),
		SkillsPath:            filepath.Join(opts.OutDir, "skills.json"),
	}
	if opts.SuitePath != "" {
		before, err := eval.Run(ctx, opts.SuitePath)
		if err != nil {
			return Report{}, fmt.Errorf("eval before learn: %w", err)
		}
		report.EvalBefore = before.Metrics
	}

	store, err := memory.Open(opts.DBPath)
	if err != nil {
		return Report{}, err
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		return Report{}, err
	}

	selectorNodes, err := store.GraphNodes(ctx, "selector_example")
	if err != nil {
		return Report{}, err
	}
	report.SelectorExamples, err = writeNodePayloadJSONL(report.SelectorDatasetPath, selectorNodes, nil)
	if err != nil {
		return Report{}, err
	}

	trajectoryNodes, err := store.GraphNodes(ctx, "trajectory_state")
	if err != nil {
		return Report{}, err
	}
	report.VerifiedTrajectories, err = writeNodePayloadJSONL(report.TrajectoryDatasetPath, trajectoryNodes, func(payload map[string]any) bool {
		verified, _ := payload["verified"].(bool)
		completed, _ := payload["completed"].(bool)
		return verified && completed
	})
	if err != nil {
		return Report{}, err
	}

	jobs, err := store.ListResearchJobs(ctx, 10000)
	if err != nil {
		return Report{}, err
	}
	report.ResearchExamples, err = writeResearchExamplesJSONL(ctx, store, report.ResearchDatasetPath, jobs)
	if err != nil {
		return Report{}, err
	}

	skills, err := store.ListSkills(ctx)
	if err != nil {
		return Report{}, err
	}
	report.Skills = len(skills)
	if err := writeJSON(report.SkillsPath, skills); err != nil {
		return Report{}, err
	}

	if opts.TrainSelectorOut != "" && report.SelectorExamples > 0 {
		examples, err := selector.LoadTrainingExamples(report.SelectorDatasetPath)
		if err != nil {
			return Report{}, err
		}
		model, trainReport, err := selector.TrainLinear(examples, selector.TrainOptions{
			Epochs:       opts.Epochs,
			LearningRate: opts.LearningRate,
		})
		if err != nil {
			return Report{}, err
		}
		if err := model.Save(opts.TrainSelectorOut); err != nil {
			return Report{}, err
		}
		report.SelectorCheckpoint = opts.TrainSelectorOut
		report.SelectorTrainReport = trainReport
	}

	if opts.SuitePath != "" {
		after, err := eval.Run(ctx, opts.SuitePath)
		if err != nil {
			return Report{}, fmt.Errorf("eval after learn: %w", err)
		}
		report.EvalAfter = after.Metrics
	}
	return report, nil
}

func writeNodePayloadJSONL(path string, nodes []memory.Node, keep func(map[string]any) bool) (int, error) {
	file, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	count := 0
	for _, node := range nodes {
		var payload map[string]any
		if err := json.Unmarshal([]byte(node.Payload), &payload); err != nil {
			continue
		}
		if keep != nil && !keep(payload) {
			continue
		}
		if _, err := writer.WriteString(node.Payload); err != nil {
			return 0, err
		}
		if _, err := writer.WriteString("\n"); err != nil {
			return 0, err
		}
		count++
	}
	if err := writer.Flush(); err != nil {
		return 0, err
	}
	return count, nil
}

func writeResearchExamplesJSONL(ctx context.Context, store *memory.Store, path string, jobs []memory.ResearchJob) (int, error) {
	file, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	count := 0
	for _, job := range jobs {
		if job.Status != "completed" || strings.TrimSpace(job.Answer) == "" {
			continue
		}
		sources, err := store.WebSourcesByJob(ctx, job.ID)
		if err != nil {
			return 0, err
		}
		example := map[string]any{
			"query":      job.Query,
			"answer":     job.Answer,
			"confidence": job.Confidence,
			"sources":    sources,
		}
		raw, err := json.Marshal(example)
		if err != nil {
			return 0, err
		}
		if _, err := writer.Write(raw); err != nil {
			return 0, err
		}
		if _, err := writer.WriteString("\n"); err != nil {
			return 0, err
		}
		count++
	}
	if err := writer.Flush(); err != nil {
		return 0, err
	}
	return count, nil
}

func writeJSON(path string, value any) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
