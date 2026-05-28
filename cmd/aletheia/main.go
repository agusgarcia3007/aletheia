package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"aletheia/internal/cognitivevm"
	"aletheia/internal/eval"
	"aletheia/internal/memory"
	"aletheia/internal/model"
	"aletheia/internal/runner"
	"aletheia/internal/selector"
	"aletheia/internal/tokenizer"
	"aletheia/internal/training"
)

func main() {
	if err := run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		return usage()
	}

	switch args[1] {
	case "init":
		return runInit(args[2:])
	case "train":
		return runTrain(args[2:])
	case "run":
		return runModel(args[2:])
	case "solve":
		return runSolve(args[2:])
	case "eval":
		return runEval(args[2:])
	case "-h", "--help", "help":
		return usage()
	default:
		return fmt.Errorf("unknown command %q", args[1])
	}
}

func usage() error {
	fmt.Fprintf(os.Stderr, `aletheia is a local verifier-first cognitive architecture.

Usage:
  aletheia init [--db %s]
  aletheia train --config configs/tiny.yaml --dataset datasets/bootstrap_actions.jsonl --out checkpoints/tiny-actions
  aletheia run --checkpoint checkpoints/tiny-actions --prompt "<USER>fix failing go test<ASSISTANT>"
  aletheia solve --task examples/buggy-go/task.json [--db %s] [--checkpoint checkpoints/tiny-actions] [--trace]
  aletheia eval --suite evals/bootstrap
`, memory.DefaultDBPath, memory.DefaultDBPath)
	return flag.ErrHelp
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	if err := fs.Parse(args); err != nil {
		return err
	}

	store, err := memory.Open(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.Migrate(context.Background()); err != nil {
		return err
	}

	abs, _ := filepath.Abs(*dbPath)
	fmt.Printf("initialized memory database: %s\n", abs)
	return nil
}

func runTrain(args []string) error {
	fs := flag.NewFlagSet("train", flag.ContinueOnError)
	configPath := fs.String("config", "configs/tiny.yaml", "training config YAML")
	datasetPath := fs.String("dataset", "datasets/bootstrap_actions.jsonl", "JSONL training dataset")
	outDir := fs.String("out", "checkpoints/tiny-actions", "checkpoint output directory")
	steps := fs.Int("steps", 0, "override training steps")
	if err := fs.Parse(args); err != nil {
		return err
	}
	report, err := training.Train(context.Background(), training.Options{
		ConfigPath:  *configPath,
		DatasetPath: *datasetPath,
		OutDir:      *outDir,
		Steps:       *steps,
	})
	if err != nil {
		return err
	}
	fmt.Printf("checkpoint: %s\n", report.CheckpointPath)
	fmt.Printf("steps: %d\n", report.Steps)
	fmt.Printf("initial_loss: %.6f\n", report.InitialLoss)
	fmt.Printf("final_loss: %.6f\n", report.FinalLoss)
	fmt.Printf("initial_accuracy: %.4f\n", report.InitialAccuracy)
	fmt.Printf("final_accuracy: %.4f\n", report.FinalAccuracy)
	return nil
}

func runModel(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	checkpoint := fs.String("checkpoint", "checkpoints/tiny-actions", "checkpoint directory")
	prompt := fs.String("prompt", "", "prompt text")
	maxTokens := fs.Int("max-tokens", 32, "maximum generated tokens")
	topK := fs.Int("top-k", 8, "top-k candidates to print from the first step")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *prompt == "" {
		return fmt.Errorf("--prompt is required")
	}
	tok := tokenizer.New()
	m, manifest, err := model.Load(*checkpoint, tok.VocabSize())
	if err != nil {
		return err
	}
	r := runner.New(m, tok)
	eos, _ := tok.ID("<EOS>")
	actRespond, _ := tok.ID("<ACT_RESPOND>")
	tokens, err := r.Generate(*prompt, runner.Options{
		MaxTokens:  *maxTokens,
		StopTokens: []int{eos, actRespond},
	})
	if err != nil {
		return err
	}
	decoded, err := tok.Decode(tokens)
	if err != nil {
		return err
	}
	promptTokens := tok.Encode(*prompt)
	logits, err := m.PredictNext(promptTokens)
	if err != nil {
		return err
	}
	candidates, err := r.TopK(logits, *topK)
	if err != nil {
		return err
	}
	fmt.Printf("checkpoint: %s\n", *checkpoint)
	fmt.Printf("model: %s step=%d loss=%.6f\n", manifest.Config.Name, manifest.Step, manifest.Loss)
	fmt.Println("top_k:")
	for _, candidate := range candidates {
		fmt.Printf("  %d %q logprob=%.4f\n", candidate.TokenID, candidate.Token, candidate.LogProb)
	}
	fmt.Println("output:")
	fmt.Println(decoded)
	return nil
}

func runSolve(args []string) error {
	fs := flag.NewFlagSet("solve", flag.ContinueOnError)
	taskPath := fs.String("task", "", "task JSON path")
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	timeout := fs.Duration("timeout", 60*time.Second, "verifier timeout")
	checkpoint := fs.String("checkpoint", "", "optional model checkpoint directory")
	maxSteps := fs.Int("max-steps", 8, "maximum Cognitive VM action steps")
	trace := fs.Bool("trace", false, "print Cognitive VM action trace")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *taskPath == "" {
		return fmt.Errorf("--task is required")
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	var planner cognitivevm.Planner = cognitivevm.MockPlanner{}
	if *checkpoint != "" {
		tok := tokenizer.New()
		m, _, err := model.Load(*checkpoint, tok.VocabSize())
		if err != nil {
			return err
		}
		planner = cognitivevm.ModelPlanner{
			Runner: runner.New(m, tok),
			TopK:   8,
		}
	}

	solver := cognitivevm.Solver{
		DBPath:          *dbPath,
		VerifierTimeout: *timeout,
		Planner:         planner,
		Selector:        selector.HeuristicSelector{},
		MaxSteps:        *maxSteps,
	}
	result, err := solver.SolveFile(context.Background(), *taskPath, wd)
	if err != nil {
		return err
	}

	fmt.Printf("goal: %s\n", result.Task.Goal)
	fmt.Printf("repo: %s\n", result.RepoPath)
	fmt.Printf("initial verifier: %s %s\n", result.Initial.Verifier, result.Initial.Status)
	if *trace {
		fmt.Println("trace:")
		for _, entry := range result.Trace {
			fmt.Printf("  %02d %s source=%s status=%s verifier=%s reason=%s\n", entry.Step, entry.Action, entry.Source, entry.Status, entry.VerifierStatus, entry.Reason)
		}
	}
	if result.Patched {
		fmt.Println("patch:")
		fmt.Print(result.Diff)
	} else {
		fmt.Println("patch: none")
	}
	fmt.Printf("final verifier: %s %s\n", result.Final.Verifier, result.Final.Status)
	fmt.Printf("evidence database: %s\n", *dbPath)
	if result.Final.Stderr != "" {
		fmt.Println("final stderr:")
		fmt.Print(result.Final.Stderr)
	}
	return nil
}

func runEval(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	suitePath := fs.String("suite", "", "evaluation suite path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *suitePath == "" {
		return fmt.Errorf("--suite is required")
	}

	info, err := eval.ValidateSuite(*suitePath)
	if err != nil {
		return err
	}
	fmt.Printf("eval suite: %s\n", info.Path)
	fmt.Printf("status: bootstrap ready\n")
	return nil
}
