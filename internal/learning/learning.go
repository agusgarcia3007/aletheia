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
	"aletheia/internal/router"
	"aletheia/internal/selector"
)

type Options struct {
	DBPath            string
	SuitePath         string
	OutDir            string
	TrainSelectorOut  string
	TrainRouterOut    string
	RouterBaseDataset string
	Epochs            int
	LearningRate      float64
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
	RouterExamples        int                  `json:"router_examples"`
	RouterDatasetPath     string               `json:"router_dataset_path,omitempty"`
	RouterCheckpoint      string               `json:"router_checkpoint,omitempty"`
	RouterPromoted        bool                 `json:"router_promoted"`
	RouterBaseAccuracy    float64              `json:"router_base_accuracy"`
	RouterCandidateAcc    float64              `json:"router_candidate_accuracy"`
	RouterPromotionReason string               `json:"router_promotion_reason,omitempty"`
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
		RouterDatasetPath:     filepath.Join(opts.OutDir, "router_examples.jsonl"),
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

	// Harvest router examples produced by the deterministic guardrails during
	// real chat. These are verified labels (when a guardrail fires, the intent is
	// known), so they are safe self-improvement signal for the router.
	routerNodes, err := store.GraphNodes(ctx, "router_example")
	if err != nil {
		return Report{}, err
	}
	report.RouterExamples, err = writeNodePayloadJSONL(report.RouterDatasetPath, routerNodes, nil)
	if err != nil {
		return Report{}, err
	}

	if opts.TrainRouterOut != "" {
		if err := trainAndPromoteRouter(opts, &report); err != nil {
			return Report{}, err
		}
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

// trainAndPromoteRouter trains a candidate router on (base corpus + harvested
// real-usage examples) and promotes it only if it does not regress on a shared
// held-out set versus a baseline trained on the base corpus alone. This is the
// verifier-first promotion gate applied to routing: never ship a worse model.
func trainAndPromoteRouter(opts Options, report *Report) error {
	base := []router.TrainingExample{}
	if strings.TrimSpace(opts.RouterBaseDataset) != "" {
		loaded, err := router.LoadTrainingExamples(opts.RouterBaseDataset)
		if err != nil {
			return fmt.Errorf("load router base dataset: %w", err)
		}
		base = loaded
	}
	harvested := []router.TrainingExample{}
	if report.RouterExamples > 0 {
		loaded, err := router.LoadTrainingExamples(report.RouterDatasetPath)
		if err != nil {
			return fmt.Errorf("load harvested router examples: %w", err)
		}
		harvested = loaded
	}
	if len(harvested) == 0 {
		report.RouterPromotionReason = "no harvested examples; nothing new to learn"
		return nil
	}
	combined := append(append([]router.TrainingExample{}, base...), harvested...)
	if len(combined) < 5 {
		report.RouterPromotionReason = "too few examples to evaluate a promotion"
		return nil
	}

	// Shared held-out set from the combined data (deterministic stride).
	var trainSet, valSet []router.TrainingExample
	for i, ex := range combined {
		if i%5 == 0 {
			valSet = append(valSet, ex)
		} else {
			trainSet = append(trainSet, ex)
		}
	}
	epochs := opts.Epochs
	if epochs <= 0 {
		epochs = 120
	}
	trainOpts := router.TrainOptions{Epochs: epochs, LearningRate: 0.2, MinConfidence: 0.35, PruneMinCount: 2}

	candidate, _, err := router.TrainLinear(trainSet, trainOpts)
	if err != nil {
		return err
	}
	baseline, _, err := router.TrainLinear(base, trainOpts)
	if err != nil {
		// No usable baseline (e.g. empty base): fall back to promoting candidate.
		baseline = candidate
	}

	report.RouterCandidateAcc = candidate.Accuracy(valSet)
	report.RouterBaseAccuracy = baseline.Accuracy(valSet)
	if report.RouterCandidateAcc+1e-9 >= report.RouterBaseAccuracy {
		// Train the promoted artifact on ALL combined data (train + val).
		promoted, _, err := router.TrainLinear(combined, trainOpts)
		if err != nil {
			return err
		}
		if err := promoted.Save(opts.TrainRouterOut); err != nil {
			return err
		}
		report.RouterCheckpoint = opts.TrainRouterOut
		report.RouterPromoted = true
		report.RouterPromotionReason = fmt.Sprintf("candidate %.3f >= base %.3f on held-out set", report.RouterCandidateAcc, report.RouterBaseAccuracy)
	} else {
		report.RouterPromoted = false
		report.RouterPromotionReason = fmt.Sprintf("candidate %.3f < base %.3f; kept current router", report.RouterCandidateAcc, report.RouterBaseAccuracy)
	}
	return nil
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
