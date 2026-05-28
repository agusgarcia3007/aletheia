package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aletheia/internal/cognitivevm"
	"aletheia/internal/eval"
	"aletheia/internal/memory"
	"aletheia/internal/model"
	"aletheia/internal/retriever"
	"aletheia/internal/runner"
	"aletheia/internal/selector"
	"aletheia/internal/tokenizer"
	"aletheia/internal/training"
	"aletheia/internal/verifier"
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
	case "index":
		return runIndex(args[2:])
	case "ask":
		return runAsk(args[2:])
	case "memory":
		return runMemory(args[2:])
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
  aletheia index ./docs [--db %s]
  aletheia ask --query "qué decisión tomamos sobre el selector?" [--db %s]
  aletheia memory inspect [--db %s]
  aletheia solve --task examples/buggy-go/task.json [--db %s] [--checkpoint checkpoints/tiny-actions] [--search greedy|beam] [--beam-width 4] [--max-depth 8] [--verifier go_test,static_go_parse] [--trace]
  aletheia eval --suite evals/bootstrap
`, memory.DefaultDBPath, memory.DefaultDBPath, memory.DefaultDBPath, memory.DefaultDBPath, memory.DefaultDBPath)
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

func runIndex(args []string) error {
	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	chunkSize := fs.Int("chunk-size", retriever.DefaultChunkSize, "chunk size in runes")
	chunkOverlap := fs.Int("chunk-overlap", retriever.DefaultChunkOverlap, "chunk overlap in runes")
	maxFileBytes := fs.Int64("max-file-bytes", retriever.DefaultMaxFileBytes, "maximum file size to index")
	path, flagArgs, err := splitIndexArgs(args)
	if err != nil {
		return err
	}
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if path == "" {
		return fmt.Errorf("index requires exactly one path")
	}
	store, err := memory.Open(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		return err
	}
	report, err := (retriever.Indexer{Store: store}).IndexPath(context.Background(), path, retriever.IndexOptions{
		ChunkSize:    *chunkSize,
		ChunkOverlap: *chunkOverlap,
		MaxFileBytes: *maxFileBytes,
	})
	if err != nil {
		return err
	}
	fmt.Printf("indexed: %s\n", report.Root)
	fmt.Printf("scanned: %d\n", report.Scanned)
	fmt.Printf("indexed_files: %d\n", report.Indexed)
	fmt.Printf("skipped_unchanged: %d\n", report.SkippedUnchanged)
	fmt.Printf("skipped_unsupported: %d\n", report.SkippedUnsupported)
	fmt.Printf("skipped_too_large: %d\n", report.SkippedTooLarge)
	fmt.Printf("chunks_written: %d\n", report.ChunksWritten)
	fmt.Printf("memory database: %s\n", *dbPath)
	return nil
}

func splitIndexArgs(args []string) (string, []string, error) {
	var path string
	var flagArgs []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flagArgs = append(flagArgs, arg)
			if strings.Contains(arg, "=") {
				continue
			}
			switch arg {
			case "--db", "-db", "--chunk-size", "-chunk-size", "--chunk-overlap", "-chunk-overlap", "--max-file-bytes", "-max-file-bytes":
				if i+1 >= len(args) {
					return "", nil, fmt.Errorf("%s requires a value", arg)
				}
				i++
				flagArgs = append(flagArgs, args[i])
			}
			continue
		}
		if path != "" {
			return "", nil, fmt.Errorf("index requires exactly one path")
		}
		path = arg
	}
	return path, flagArgs, nil
}

func runAsk(args []string) error {
	fs := flag.NewFlagSet("ask", flag.ContinueOnError)
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	query := fs.String("query", "", "question to answer from indexed local memory")
	topK := fs.Int("top-k", 5, "number of evidence chunks")
	minConfidence := fs.Float64("min-confidence", retriever.DefaultMinConfidence, "minimum confidence threshold")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*query) == "" {
		return fmt.Errorf("--query is required")
	}
	store, err := memory.Open(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		return err
	}
	answer, err := (retriever.Retriever{Store: store}).Answer(context.Background(), *query, retriever.SearchOptions{
		TopK:          *topK,
		MinConfidence: *minConfidence,
	})
	if err != nil {
		return err
	}
	fmt.Printf("status: %s\n", answer.Status)
	fmt.Printf("confidence: %.4f\n", answer.Confidence)
	fmt.Println("answer:")
	fmt.Println(answer.Text)
	if len(answer.Citations) > 0 {
		fmt.Println("evidence:")
		for _, citation := range answer.Citations {
			fmt.Printf("  %s chunk=%d offsets=%d-%d score=%.4f\n", citation.Path, citation.ChunkID, citation.OffsetStart, citation.OffsetEnd, citation.Score)
		}
	}
	return nil
}

func runMemory(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("memory requires a subcommand")
	}
	switch args[0] {
	case "inspect":
		return runMemoryInspect(args[1:])
	default:
		return fmt.Errorf("unknown memory subcommand %q", args[0])
	}
}

func runMemoryInspect(args []string) error {
	fs := flag.NewFlagSet("memory inspect", flag.ContinueOnError)
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	latest := fs.Int("latest", 5, "latest indexed paths to show")
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
	stats, err := store.Inspect(context.Background(), *latest)
	if err != nil {
		return err
	}
	fmt.Printf("documents: %d\n", stats.Documents)
	fmt.Printf("chunks: %d\n", stats.Chunks)
	fmt.Printf("nodes: %d\n", stats.Nodes)
	fmt.Printf("edges: %d\n", stats.Edges)
	if len(stats.LatestPaths) > 0 {
		fmt.Println("latest:")
		for _, path := range stats.LatestPaths {
			fmt.Printf("  %s\n", path)
		}
	}
	return nil
}

func runSolve(args []string) error {
	fs := flag.NewFlagSet("solve", flag.ContinueOnError)
	taskPath := fs.String("task", "", "task JSON path")
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	timeout := fs.Duration("timeout", 60*time.Second, "verifier timeout")
	checkpoint := fs.String("checkpoint", "", "optional model checkpoint directory")
	maxSteps := fs.Int("max-steps", 8, "maximum Cognitive VM action steps")
	searchStrategy := fs.String("search", "greedy", "search strategy: greedy or beam")
	beamWidth := fs.Int("beam-width", 4, "beam search width")
	maxDepth := fs.Int("max-depth", 0, "beam search maximum depth; defaults to --max-steps")
	verifierNamesCSV := fs.String("verifier", verifier.GoTestName, "comma-separated verifier names")
	includeVet := fs.Bool("vet", false, "also run go_vet verifier")
	includeRace := fs.Bool("race", false, "also run go_test_race verifier")
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
	verifierNames, err := verifier.ParseNames(*verifierNamesCSV, *includeVet, *includeRace)
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
		SearchStrategy:  *searchStrategy,
		BeamWidth:       *beamWidth,
		MaxDepth:        *maxDepth,
		VerifierNames:   verifierNames,
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
			fmt.Printf("  %02d %s source=%s status=%s verifier=%s verifiers=%s reason=%s\n", entry.Step, entry.Action, entry.Source, entry.Status, entry.VerifierStatus, strings.Join(entry.Verifiers, ","), entry.Reason)
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

	report, err := eval.RunBootstrap(context.Background(), *suitePath)
	if err != nil {
		return err
	}
	fmt.Printf("eval suite: %s\n", report.Suite.Path)
	fmt.Printf("status: bootstrap ready\n")
	for _, c := range report.Cases {
		fmt.Printf("%s:\n", c.Name)
		fmt.Printf("  candidate_greedy: %s\n", c.CandidateGreedyStatus)
		fmt.Printf("  beam: %s\n", c.BeamStatus)
		fmt.Printf("  improvement: %v\n", c.Improved)
	}
	if !report.Improved() {
		return fmt.Errorf("eval bootstrap did not show beam improvement")
	}
	return nil
}
