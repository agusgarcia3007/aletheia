package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aletheia/internal/apiserver"
	"aletheia/internal/cognitivevm"
	"aletheia/internal/config"
	"aletheia/internal/datasetbuilder"
	"aletheia/internal/eval"
	"aletheia/internal/learning"
	"aletheia/internal/memory"
	"aletheia/internal/model"
	"aletheia/internal/research"
	"aletheia/internal/retriever"
	"aletheia/internal/router"
	"aletheia/internal/runner"
	"aletheia/internal/selector"
	"aletheia/internal/tokenizer"
	"aletheia/internal/training"
	"aletheia/internal/verifier"
)

func main() {
	if err := run(os.Args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		usage(os.Stdout)
		return nil
	}

	switch args[1] {
	case "config":
		return runConfig(args[2:])
	case "init":
		return runInit(args[2:])
	case "train":
		return runTrain(args[2:])
	case "dataset":
		return runDataset(args[2:])
	case "tokenizer":
		return runTokenizer(args[2:])
	case "train-selector":
		return runTrainSelector(args[2:])
	case "train-router":
		return runTrainRouter(args[2:])
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
	case "learn":
		return runLearn(args[2:])
	case "research":
		return runResearch(args[2:])
	case "research-status":
		return runResearchStatus(args[2:])
	case "jobs":
		return runJobs(args[2:])
	case "serve":
		return runServe(args[2:])
	case "-h", "--help", "help":
		usage(os.Stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[1])
	}
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `aletheia is a local verifier-first cognitive architecture.

Usage:
  aletheia init [--config configs/micro.yaml] [--db %s]
  aletheia config inspect --config configs/micro.yaml
  aletheia train --config configs/aletheia-mikros.yaml --dataset datasets/aletheia_mikros.jsonl --out checkpoints/aletheia-mikros
  aletheia dataset build --profile mikros-v1 --out datasets/generated/mikros_v1.jsonl
  aletheia dataset build --profile mikros-curriculum-v1 --out datasets/generated/mikros_curriculum_v1.jsonl
  aletheia dataset build --profile mikros-live-v1 --out datasets/generated/mikros_live_v1.jsonl
  aletheia tokenizer train --dataset datasets/generated/mikros_v1.jsonl --out checkpoints/aletheia-mikros/tokenizer.json
  aletheia train-selector --dataset datasets/selector_bootstrap.jsonl --out checkpoints/selector-bootstrap
  aletheia train-router --dataset datasets/router_mikros.jsonl --out checkpoints/router-mikros
  aletheia run --checkpoint checkpoints/aletheia-mikros --prompt "<USER>hola como estas?<ASSISTANT>"
  aletheia index ./docs [--config configs/micro.yaml] [--db %s]
  aletheia ask --query "qué decisión tomamos sobre el selector?" [--config configs/micro.yaml] [--db %s]
  aletheia memory inspect [--config configs/micro.yaml] [--db %s]
  aletheia memory skills [--config configs/micro.yaml] [--db %s]
  aletheia memory graph [--config configs/micro.yaml] [--db %s] [--type patch_candidate]
  aletheia solve --task examples/buggy-go/task.json [--config configs/micro.yaml] [--db %s] [--checkpoint checkpoint-dir] [--selector-checkpoint checkpoints/selector-bootstrap] [--use-skills] [--search greedy|beam|mcts] [--beam-width 4] [--max-depth 8] [--verifier go_test,static_go_parse] [--trace]
  aletheia eval --suite evals/bootstrap [--json]
  aletheia learn --db %s --suite evals/bootstrap --out datasets/generated [--train-selector-out checkpoints/selector-generated]
  aletheia research --query "what is MCP in agents?" [--db %s] [--background]
  aletheia research-status --job research_id [--db %s]
  aletheia jobs [--db %s]
  aletheia serve [--addr :8080] [--checkpoints-dir checkpoints] [--checkpoint checkpoints/aletheia-mikros] [--model aletheia-mikros] [--router-checkpoint checkpoints/router-mikros] [--api-key $ALETHEIA_API_KEY]
`, memory.DefaultDBPath, memory.DefaultDBPath, memory.DefaultDBPath, memory.DefaultDBPath, memory.DefaultDBPath, memory.DefaultDBPath, memory.DefaultDBPath, memory.DefaultDBPath, memory.DefaultDBPath, memory.DefaultDBPath, memory.DefaultDBPath)
}

func runConfig(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("config requires a subcommand")
	}
	switch args[0] {
	case "inspect":
		return runConfigInspect(args[1:])
	default:
		return fmt.Errorf("unknown config subcommand %q", args[0])
	}
}

func runConfigInspect(args []string) error {
	fs := flag.NewFlagSet("config inspect", flag.ContinueOnError)
	configPath := fs.String("config", "", "configuration YAML path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *configPath == "" {
		return fmt.Errorf("--config is required")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	fmt.Printf("config: %s\n", *configPath)
	fmt.Printf("project: name=%s data_dir=%s checkpoint_dir=%s memory_db=%s\n", cfg.Project.Name, cfg.Project.DataDir, cfg.Project.CheckpointDir, cfg.Project.MemoryDB)
	fmt.Printf("model: name=%s vocab_size=%d context_length=%d layers=%d heads=%d d_model=%d d_ff=%d\n", cfg.Model.Name, cfg.Model.VocabSize, cfg.Model.ContextLength, cfg.Model.NLayers, cfg.Model.NHeads, cfg.Model.DModel, cfg.Model.DFF)
	fmt.Printf("search: strategy=%s beam_width=%d max_depth=%d budget_seconds=%d budget_tool_calls=%d\n", cfg.Search.Strategy, cfg.Search.BeamWidth, cfg.Search.MaxDepth, cfg.Search.BudgetSeconds, cfg.Search.BudgetToolCalls)
	fmt.Printf("verifiers: %s timeout=%s\n", strings.Join(cfg.EnabledVerifierNames(), ","), cfg.EffectiveVerifierTimeout())
	fmt.Printf("memory: chunk_size=%d chunk_overlap=%d max_file_bytes=%d embedding=%s graph_enabled=%v\n", cfg.Memory.ChunkSize, cfg.Memory.ChunkOverlap, cfg.Memory.MaxFileBytes, cfg.Memory.Embedding, cfg.Memory.GraphEnabledBool())
	researchCfg := cfg.ResearchWithEnv()
	fmt.Printf("research: enabled=%v auto=%v background=%v provider=%s searxng_url=%s max_sources=%d\n", researchCfg.Enabled, researchCfg.AutoOnKnowledgeGap, researchCfg.BackgroundJobsEnabled, researchCfg.Provider, researchCfg.SearXNGURL, researchCfg.MaxSources)
	return nil
}

func loadConfig(path string) (*config.Config, error) {
	if path == "" {
		return nil, nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	seen := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			seen = true
		}
	})
	return seen
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	configPath := fs.String("config", "", "configuration YAML path")
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if cfg != nil && !flagWasSet(fs, "db") {
		*dbPath = cfg.Project.MemoryDB
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
	configPath := fs.String("config", "configs/aletheia-mikros.yaml", "training config YAML")
	datasetPath := fs.String("dataset", "datasets/aletheia_mikros.jsonl", "JSONL training dataset")
	outDir := fs.String("out", "checkpoints/aletheia-mikros", "checkpoint output directory")
	steps := fs.Int("steps", 0, "override training steps")
	if err := fs.Parse(args); err != nil {
		return err
	}
	report, err := training.Train(context.Background(), training.Options{
		ConfigPath:    *configPath,
		DatasetPath:   *datasetPath,
		OutDir:        *outDir,
		Steps:         *steps,
		OverrideSteps: flagWasSet(fs, "steps"),
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

func runDataset(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("dataset requires a subcommand")
	}
	switch args[0] {
	case "build":
		return runDatasetBuild(args[1:])
	default:
		return fmt.Errorf("unknown dataset subcommand %q", args[0])
	}
}

func runDatasetBuild(args []string) error {
	fs := flag.NewFlagSet("dataset build", flag.ContinueOnError)
	profile := fs.String("profile", datasetbuilder.MikrosV1Profile, "dataset profile")
	out := fs.String("out", "datasets/generated/mikros_v1.jsonl", "output JSONL path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	report, err := datasetbuilder.Build(*profile, *out)
	if err != nil {
		return err
	}
	fmt.Printf("dataset: %s\n", report.OutPath)
	fmt.Printf("profile: %s\n", report.Profile)
	fmt.Printf("examples: %d\n", report.Examples)
	return nil
}

func runTokenizer(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("tokenizer requires a subcommand")
	}
	switch args[0] {
	case "train":
		return runTokenizerTrain(args[1:])
	default:
		return fmt.Errorf("unknown tokenizer subcommand %q", args[0])
	}
}

func runTokenizerTrain(args []string) error {
	fs := flag.NewFlagSet("tokenizer train", flag.ContinueOnError)
	datasetPath := fs.String("dataset", "datasets/generated/mikros_v1.jsonl", "dataset JSONL path")
	out := fs.String("out", "checkpoints/aletheia-mikros/tokenizer.json", "tokenizer artifact path")
	vocabSize := fs.Int("vocab-size", 8192, "target tokenizer vocabulary size")
	if err := fs.Parse(args); err != nil {
		return err
	}
	artifact, err := tokenizer.TrainBPEFromJSONL(*datasetPath, *out, *vocabSize)
	if err != nil {
		return err
	}
	fmt.Printf("tokenizer: %s\n", *out)
	fmt.Printf("type: %s\n", artifact.Type)
	fmt.Printf("vocab_size: %d\n", artifact.VocabSize)
	return nil
}

func runTrainSelector(args []string) error {
	fs := flag.NewFlagSet("train-selector", flag.ContinueOnError)
	datasetPath := fs.String("dataset", "datasets/selector_bootstrap.jsonl", "selector JSONL training dataset")
	outDir := fs.String("out", "checkpoints/selector-bootstrap", "selector checkpoint output directory")
	epochs := fs.Int("epochs", 300, "training epochs")
	learningRate := fs.Float64("learning-rate", 0.1, "learning rate")
	minConfidence := fs.Float64("min-confidence", selector.DefaultMinConfidence, "minimum selector confidence before heuristic fallback")
	if err := fs.Parse(args); err != nil {
		return err
	}
	examples, err := selector.LoadTrainingExamples(*datasetPath)
	if err != nil {
		return err
	}
	model, report, err := selector.TrainLinear(examples, selector.TrainOptions{
		Epochs:        *epochs,
		LearningRate:  *learningRate,
		MinConfidence: *minConfidence,
	})
	if err != nil {
		return err
	}
	if err := model.Save(*outDir); err != nil {
		return err
	}
	fmt.Printf("selector_checkpoint: %s\n", *outDir)
	fmt.Printf("epochs: %d\n", report.Epochs)
	fmt.Printf("initial_loss: %.6f\n", report.InitialLoss)
	fmt.Printf("final_loss: %.6f\n", report.FinalLoss)
	fmt.Printf("initial_accuracy: %.4f\n", report.InitialAccuracy)
	fmt.Printf("final_accuracy: %.4f\n", report.FinalAccuracy)
	return nil
}

func runTrainRouter(args []string) error {
	fs := flag.NewFlagSet("train-router", flag.ContinueOnError)
	datasetPath := fs.String("dataset", "datasets/router_mikros.jsonl", "router JSONL training dataset")
	outDir := fs.String("out", "checkpoints/router-mikros", "router checkpoint output directory")
	epochs := fs.Int("epochs", 80, "training epochs")
	learningRate := fs.Float64("learning-rate", 0.2, "learning rate")
	minConfidence := fs.Float64("min-confidence", 0.35, "minimum confidence before fallback routing")
	valSplit := fs.Float64("val-split", 0.2, "fraction of examples held out to measure generalization (0 to disable)")
	pruneMinCount := fs.Int("prune-min-count", 2, "drop features seen fewer than this many times (shrinks model, reduces overfit; 0/1 disables)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	examples, err := router.LoadTrainingExamples(*datasetPath)
	if err != nil {
		return err
	}
	// First pass: hold out a validation set to measure generalization honestly.
	_, valReport, err := router.TrainLinear(examples, router.TrainOptions{
		Epochs:          *epochs,
		LearningRate:    *learningRate,
		MinConfidence:   *minConfidence,
		ValidationSplit: *valSplit,
	})
	if err != nil {
		return err
	}
	// Second pass: train on all data for the deployed artifact.
	model, report, err := router.TrainLinear(examples, router.TrainOptions{
		Epochs:        *epochs,
		LearningRate:  *learningRate,
		MinConfidence: *minConfidence,
		PruneMinCount: *pruneMinCount,
	})
	if err != nil {
		return err
	}
	if err := model.Save(*outDir); err != nil {
		return err
	}
	fmt.Printf("router_checkpoint: %s\n", *outDir)
	fmt.Printf("epochs: %d\n", report.Epochs)
	fmt.Printf("examples: %d\n", report.Examples)
	fmt.Printf("initial_loss: %.6f\n", report.InitialLoss)
	fmt.Printf("final_loss: %.6f\n", report.FinalLoss)
	fmt.Printf("train_accuracy: %.4f\n", report.FinalAccuracy)
	if valReport.ValidationExamples > 0 {
		fmt.Printf("validation_examples: %d\n", valReport.ValidationExamples)
		fmt.Printf("validation_accuracy: %.4f\n", valReport.ValidationAccuracy)
		if report.FinalAccuracy-valReport.ValidationAccuracy > 0.25 {
			fmt.Printf("warning: large train/validation gap (%.2f) suggests overfitting; add more diverse examples\n", report.FinalAccuracy-valReport.ValidationAccuracy)
		}
	}
	return nil
}

func runModel(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := fs.String("config", "", "configuration YAML path")
	checkpoint := fs.String("checkpoint", "checkpoints/aletheia-mikros", "checkpoint directory")
	prompt := fs.String("prompt", "", "prompt text")
	maxTokens := fs.Int("max-tokens", 32, "maximum generated tokens")
	topK := fs.Int("top-k", 8, "top-k candidates to print from the first step")
	temperature := fs.Float64("temperature", -1, "sampling temperature; <=0 keeps greedy generation")
	topP := fs.Float64("top-p", -1, "nucleus sampling threshold")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if cfg != nil {
		if !flagWasSet(fs, "checkpoint") {
			*checkpoint = filepath.Join(cfg.Project.CheckpointDir, cfg.Model.Name)
		}
		if !flagWasSet(fs, "max-tokens") {
			*maxTokens = cfg.Inference.MaxTokens
		}
		if !flagWasSet(fs, "top-k") {
			*topK = cfg.Inference.TopK
		}
		if !flagWasSet(fs, "temperature") {
			*temperature = cfg.Inference.Temperature
		}
		if !flagWasSet(fs, "top-p") {
			*topP = cfg.Inference.TopP
		}
	}
	if *temperature < 0 {
		*temperature = 0
	}
	if *topP < 0 {
		*topP = 1
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
		MaxTokens:   *maxTokens,
		TopK:        *topK,
		TopP:        *topP,
		Temperature: *temperature,
		StopTokens:  []int{eos, actRespond},
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
	configPath := fs.String("config", "", "configuration YAML path")
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
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	graphEnabled := true
	if cfg != nil {
		if !flagWasSet(fs, "db") {
			*dbPath = cfg.Project.MemoryDB
		}
		if !flagWasSet(fs, "chunk-size") {
			*chunkSize = cfg.Memory.ChunkSize
		}
		if !flagWasSet(fs, "chunk-overlap") {
			*chunkOverlap = cfg.Memory.ChunkOverlap
		}
		if !flagWasSet(fs, "max-file-bytes") {
			*maxFileBytes = cfg.Memory.MaxFileBytes
		}
		graphEnabled = cfg.Memory.GraphEnabledBool()
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
		GraphEnabled: &graphEnabled,
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
			case "--config", "-config", "--db", "-db", "--chunk-size", "-chunk-size", "--chunk-overlap", "-chunk-overlap", "--max-file-bytes", "-max-file-bytes":
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
	configPath := fs.String("config", "", "configuration YAML path")
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	query := fs.String("query", "", "question to answer from indexed local memory")
	topK := fs.Int("top-k", 5, "number of evidence chunks")
	minConfidence := fs.Float64("min-confidence", retriever.DefaultMinConfidence, "minimum confidence threshold")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if cfg != nil && !flagWasSet(fs, "db") {
		*dbPath = cfg.Project.MemoryDB
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
	fmt.Printf("verified: %v\n", answer.Verified)
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
	case "skills":
		return runMemorySkills(args[1:])
	case "graph":
		return runMemoryGraph(args[1:])
	default:
		return fmt.Errorf("unknown memory subcommand %q", args[0])
	}
}

func runMemoryInspect(args []string) error {
	fs := flag.NewFlagSet("memory inspect", flag.ContinueOnError)
	configPath := fs.String("config", "", "configuration YAML path")
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	latest := fs.Int("latest", 5, "latest indexed paths to show")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if cfg != nil && !flagWasSet(fs, "db") {
		*dbPath = cfg.Project.MemoryDB
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
	fmt.Printf("skills: %d\n", stats.Skills)
	fmt.Printf("nodes: %d\n", stats.Nodes)
	fmt.Printf("edges: %d\n", stats.Edges)
	if len(stats.NodeTypes) > 0 {
		fmt.Println("node_types:")
		for _, item := range stats.NodeTypes {
			fmt.Printf("  %s: %d\n", item.Type, item.Count)
		}
	}
	if len(stats.LatestPaths) > 0 {
		fmt.Println("latest:")
		for _, path := range stats.LatestPaths {
			fmt.Printf("  %s\n", path)
		}
	}
	return nil
}

func runMemoryGraph(args []string) error {
	fs := flag.NewFlagSet("memory graph", flag.ContinueOnError)
	configPath := fs.String("config", "", "configuration YAML path")
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	nodeType := fs.String("type", "", "optional node type filter")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if cfg != nil && !flagWasSet(fs, "db") {
		*dbPath = cfg.Project.MemoryDB
	}
	store, err := memory.Open(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		return err
	}
	nodes, err := store.GraphNodes(context.Background(), *nodeType)
	if err != nil {
		return err
	}
	edges, err := store.GraphEdges(context.Background())
	if err != nil {
		return err
	}
	if *nodeType != "" {
		nodeIDs := make(map[int64]bool, len(nodes))
		for _, node := range nodes {
			nodeIDs[node.ID] = true
		}
		filtered := edges[:0]
		for _, edge := range edges {
			if nodeIDs[edge.FromNode] || nodeIDs[edge.ToNode] {
				filtered = append(filtered, edge)
			}
		}
		edges = filtered
	}
	fmt.Printf("nodes: %d\n", len(nodes))
	for _, node := range nodes {
		fmt.Printf("  %d type=%s label=%s payload=%s\n", node.ID, node.Type, node.Label, compactPayload(node.Payload))
	}
	fmt.Printf("edges: %d\n", len(edges))
	for _, edge := range edges {
		fmt.Printf("  %d %d -[%s %.2f]-> %d\n", edge.ID, edge.FromNode, edge.Relation, edge.Weight, edge.ToNode)
	}
	return nil
}

func runMemorySkills(args []string) error {
	fs := flag.NewFlagSet("memory skills", flag.ContinueOnError)
	configPath := fs.String("config", "", "configuration YAML path")
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if cfg != nil && !flagWasSet(fs, "db") {
		*dbPath = cfg.Project.MemoryDB
	}
	store, err := memory.Open(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		return err
	}
	skills, err := store.ListSkills(context.Background())
	if err != nil {
		return err
	}
	fmt.Printf("skills: %d\n", len(skills))
	for _, skill := range skills {
		fmt.Printf("  %s trigger=%s success_rate=%.4f sequence=%s\n", skill.Name, skill.Trigger, skill.SuccessRate, strings.Join(skill.ActionSequence, ","))
	}
	return nil
}

func compactPayload(payload string) string {
	payload = strings.TrimSpace(payload)
	if len(payload) <= 240 {
		return payload
	}
	return payload[:240] + "...[truncated]"
}

func runSolve(args []string) error {
	fs := flag.NewFlagSet("solve", flag.ContinueOnError)
	configPath := fs.String("config", "", "configuration YAML path")
	taskPath := fs.String("task", "", "task JSON path")
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	timeout := fs.Duration("timeout", 60*time.Second, "verifier timeout")
	checkpoint := fs.String("checkpoint", "", "optional model checkpoint directory")
	selectorCheckpoint := fs.String("selector-checkpoint", "", "optional learned selector checkpoint directory")
	maxSteps := fs.Int("max-steps", 8, "maximum Cognitive VM action steps")
	searchStrategy := fs.String("search", "greedy", "search strategy: greedy, beam, or mcts")
	beamWidth := fs.Int("beam-width", 4, "beam search width")
	maxDepth := fs.Int("max-depth", 0, "beam search maximum depth; defaults to --max-steps")
	useSkills := fs.Bool("use-skills", false, "reuse verified compressed skills when trigger matches")
	verifierNamesCSV := fs.String("verifier", verifier.GoTestName, "comma-separated verifier names")
	includeVet := fs.Bool("vet", false, "also run go_vet verifier")
	includeRace := fs.Bool("race", false, "also run go_test_race verifier")
	includeFuzz := fs.Bool("fuzz", false, "also run go_test_fuzz verifier")
	includeBench := fs.Bool("bench", false, "also run go_test_bench verifier")
	trace := fs.Bool("trace", false, "print Cognitive VM action trace")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if cfg != nil {
		if !flagWasSet(fs, "db") {
			*dbPath = cfg.Project.MemoryDB
		}
		if !flagWasSet(fs, "timeout") {
			*timeout = cfg.EffectiveVerifierTimeout()
		}
		if !flagWasSet(fs, "search") {
			*searchStrategy = cfg.Search.Strategy
		}
		if !flagWasSet(fs, "beam-width") {
			*beamWidth = cfg.Search.BeamWidth
		}
		if !flagWasSet(fs, "max-depth") {
			*maxDepth = cfg.Search.MaxDepth
		}
	}
	if *taskPath == "" {
		return fmt.Errorf("--task is required")
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	verifierCSV := *verifierNamesCSV
	if cfg != nil && !flagWasSet(fs, "verifier") {
		verifierCSV = strings.Join(cfg.EnabledVerifierNames(), ",")
	}
	verifierNames, err := verifier.ParseNames(verifierCSV, *includeVet, *includeRace, *includeFuzz, *includeBench)
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

	var actionSelector cognitivevm.ActionSelector
	if *selectorCheckpoint != "" {
		learned, err := selector.LoadLinear(*selectorCheckpoint)
		if err != nil {
			return err
		}
		actionSelector = learned
	}

	solver := cognitivevm.Solver{
		DBPath:          *dbPath,
		VerifierTimeout: *timeout,
		Planner:         planner,
		Selector:        actionSelector,
		MaxSteps:        *maxSteps,
		SearchStrategy:  *searchStrategy,
		BeamWidth:       *beamWidth,
		MaxDepth:        *maxDepth,
		UseSkills:       *useSkills,
		VerifierNames:   verifierNames,
	}
	result, err := solver.SolveFile(context.Background(), *taskPath, wd)
	if err != nil {
		return err
	}

	fmt.Printf("goal: %s\n", result.Task.Goal)
	fmt.Printf("repo: %s\n", result.RepoPath)
	if result.InitialSkipped {
		fmt.Printf("initial verifier: skipped\n")
	} else {
		fmt.Printf("initial verifier: %s %s\n", result.Initial.Verifier, result.Initial.Status)
	}
	if result.SkillUsed != "" {
		fmt.Printf("skill: %s\n", result.SkillUsed)
	}
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
	fmt.Printf("tool_calls: %d\n", result.ToolCalls)
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
	jsonOutput := fs.Bool("json", false, "print JSON evaluation report")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *suitePath == "" {
		return fmt.Errorf("--suite is required")
	}

	report, err := eval.Run(context.Background(), *suitePath)
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	fmt.Printf("eval suite: %s\n", report.Suite.Path)
	fmt.Printf("status: %s ready\n", filepath.Base(report.Suite.Path))
	for _, c := range report.Cases {
		fmt.Printf("%s:\n", c.Name)
		if c.Status != "" {
			fmt.Printf("  status: %s\n", c.Status)
		}
		if c.CandidateGreedyStatus != "" {
			fmt.Printf("  candidate_greedy: %s\n", c.CandidateGreedyStatus)
		}
		if c.BeamStatus != "" {
			fmt.Printf("  beam: %s\n", c.BeamStatus)
		}
		if c.MCTSStatus != "" {
			fmt.Printf("  mcts: %s\n", c.MCTSStatus)
		}
		if c.LearnedSelectorStatus != "" {
			fmt.Printf("  learned_selector: %s\n", c.LearnedSelectorStatus)
		}
		if c.SkillReuseStatus != "" {
			fmt.Printf("  skill_reuse: %s\n", c.SkillReuseStatus)
			fmt.Printf("  baseline_tool_calls: %d\n", c.BaselineToolCalls)
			fmt.Printf("  skill_tool_calls: %d\n", c.SkillToolCalls)
		}
		fmt.Printf("  improvement: %v\n", c.Improved)
	}
	fmt.Printf("metrics:\n")
	fmt.Printf("  verified_success_rate: %.4f\n", report.Metrics.VerifiedSuccessRate)
	fmt.Printf("  hallucination_rate: %.4f\n", report.Metrics.HallucinationRate)
	fmt.Printf("  abstention_accuracy: %.4f\n", report.Metrics.AbstentionAccuracy)
	fmt.Printf("  false_verified_rate: %.4f\n", report.Metrics.FalseVerifiedRate)
	fmt.Printf("  citation_validity: %.4f\n", report.Metrics.CitationValidity)
	fmt.Printf("  repair_pass_rate: %.4f\n", report.Metrics.RepairPassRate)
	fmt.Printf("  tool_calls_per_success: %.4f\n", report.Metrics.ToolCallsPerSuccess)
	fmt.Printf("  seconds_per_success: %.4f\n", report.Metrics.SecondsPerSuccess)
	fmt.Printf("  memory_hit_rate: %.4f\n", report.Metrics.MemoryHitRate)
	fmt.Printf("  cost_per_success: %.4f\n", report.Metrics.CostPerSuccess)
	if !report.Improved() {
		return fmt.Errorf("eval suite did not pass release gates")
	}
	return nil
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stdout)
	configPath := fs.String("config", "", "configuration YAML path")
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	addr := fs.String("addr", envDefault("ALETHEIA_ADDR", apiserver.DefaultAddr), "HTTP listen address")
	checkpoint := fs.String("checkpoint", envDefault("ALETHEIA_CHECKPOINT", apiserver.DefaultCheckpoint), "model checkpoint directory")
	checkpointsDir := fs.String("checkpoints-dir", os.Getenv("ALETHEIA_CHECKPOINTS_DIR"), "directory containing model checkpoint subdirectories")
	modelName := fs.String("model", os.Getenv("ALETHEIA_MODEL"), "served OpenAI-compatible model name")
	routerCheckpoint := fs.String("router-checkpoint", envDefault("ALETHEIA_ROUTER_CHECKPOINT", "checkpoints/router-mikros"), "optional Mikros router checkpoint directory")
	knowledgePath := fs.String("knowledge", envDefault("ALETHEIA_KNOWLEDGE", apiserver.DefaultKnowledgePath), "local knowledge corpus directory indexed for retrieval")
	apiKey := fs.String("api-key", os.Getenv("ALETHEIA_API_KEY"), "Bearer API key for /v1/* endpoints")
	auth := fs.String("auth", envDefault("ALETHEIA_AUTH", "bearer"), "authentication mode: bearer or none")
	maxBodyBytes := fs.Int64("max-body-bytes", apiserver.DefaultMaxBodyBytes, "maximum JSON request body size")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if cfg != nil && !flagWasSet(fs, "db") {
		*dbPath = cfg.Project.MemoryDB
	}
	store, err := memory.Open(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		return err
	}
	server, err := apiserver.New(apiserver.Options{
		Addr:             *addr,
		Checkpoint:       *checkpoint,
		CheckpointsDir:   *checkpointsDir,
		ModelName:        *modelName,
		APIKey:           *apiKey,
		Auth:             *auth,
		MaxBodyBytes:     *maxBodyBytes,
		Store:            store,
		Research:         researchOptionsFromConfig(cfg),
		RouterCheckpoint: *routerCheckpoint,
		KnowledgePath:    *knowledgePath,
	})
	if err != nil {
		return err
	}
	fmt.Printf("serving model %s on %s\n", server.ModelName(), server.Addr())
	return server.ListenAndServe(context.Background())
}

func runResearch(args []string) error {
	fs := flag.NewFlagSet("research", flag.ContinueOnError)
	configPath := fs.String("config", "", "configuration YAML path")
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	query := fs.String("query", "", "research query")
	background := fs.Bool("background", false, "queue research instead of running synchronously")
	maxSources := fs.Int("max-sources", 0, "maximum sources to fetch")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*query) == "" {
		return fmt.Errorf("--query is required")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if cfg != nil && !flagWasSet(fs, "db") {
		*dbPath = cfg.Project.MemoryDB
	}
	store, err := memory.Open(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		return err
	}
	opts := researchOptionsFromConfig(cfg)
	opts.Enabled = true
	if *maxSources > 0 {
		opts.MaxSources = *maxSources
	}
	mode := "sync"
	if *background {
		mode = "background"
	}
	job, err := store.CreateResearchJob(context.Background(), memory.ResearchJob{
		Query:      *query,
		Status:     "queued",
		Mode:       mode,
		MaxSources: opts.MaxSources,
	})
	if err != nil {
		return err
	}
	if *background {
		fmt.Printf("status: queued\njob_id: %s\n", job.ID)
		return nil
	}
	worker := research.NewWorker(store, opts)
	result, err := worker.RunJob(context.Background(), job)
	if err != nil {
		return err
	}
	fmt.Printf("status: completed\n")
	fmt.Printf("job_id: %s\n", job.ID)
	fmt.Printf("sources_found: %d\n", len(result.Sources))
	fmt.Printf("sources_stored: %d\n", result.SourcesStored)
	fmt.Printf("chunks_stored: %d\n", result.ChunksStored)
	fmt.Printf("claims_stored: %d\n", result.ClaimsStored)
	fmt.Printf("confidence: %.4f\n", result.Confidence)
	fmt.Printf("answer:\n%s\n", result.Answer)
	return nil
}

func runResearchStatus(args []string) error {
	fs := flag.NewFlagSet("research-status", flag.ContinueOnError)
	configPath := fs.String("config", "", "configuration YAML path")
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	jobID := fs.String("job", "", "research job id")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *jobID == "" {
		return fmt.Errorf("--job is required")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if cfg != nil && !flagWasSet(fs, "db") {
		*dbPath = cfg.Project.MemoryDB
	}
	store, err := memory.Open(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		return err
	}
	job, ok, err := store.ResearchJob(context.Background(), *jobID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("research job %s not found", *jobID)
	}
	sources, _ := store.WebSourcesByJob(context.Background(), job.ID)
	claims, _ := store.WebClaimsByJob(context.Background(), job.ID)
	fmt.Printf("status: %s\n", job.Status)
	fmt.Printf("job_id: %s\n", job.ID)
	fmt.Printf("query: %s\n", job.Query)
	fmt.Printf("sources_stored: %d\n", len(sources))
	fmt.Printf("claims_stored: %d\n", len(claims))
	fmt.Printf("confidence: %.4f\n", job.Confidence)
	if job.Error != "" {
		fmt.Printf("error: %s\n", job.Error)
	}
	if job.Answer != "" {
		fmt.Printf("answer:\n%s\n", job.Answer)
	}
	return nil
}

func runJobs(args []string) error {
	fs := flag.NewFlagSet("jobs", flag.ContinueOnError)
	configPath := fs.String("config", "", "configuration YAML path")
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	limit := fs.Int("limit", 20, "maximum jobs to list")
	includeFailed := fs.Bool("include-failed", false, "include failed research jobs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if cfg != nil && !flagWasSet(fs, "db") {
		*dbPath = cfg.Project.MemoryDB
	}
	store, err := memory.Open(*dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(context.Background()); err != nil {
		return err
	}
	jobs, err := store.ListResearchJobs(context.Background(), *limit)
	if err != nil {
		return err
	}
	if !*includeFailed {
		filtered := jobs[:0]
		for _, job := range jobs {
			if job.Status != "failed" {
				filtered = append(filtered, job)
			}
		}
		jobs = filtered
	}
	fmt.Printf("jobs: %d\n", len(jobs))
	for _, job := range jobs {
		fmt.Printf("  %s status=%s mode=%s confidence=%.4f query=%q\n", job.ID, job.Status, job.Mode, job.Confidence, job.Query)
	}
	return nil
}

func researchOptionsFromConfig(cfg *config.Config) research.Options {
	var base config.Config
	if cfg != nil {
		base = *cfg
	} else {
		base.ApplyDefaults()
	}
	researchCfg := base.ResearchWithEnv()
	return research.Options{
		Enabled:               researchCfg.Enabled,
		AutoOnKnowledgeGap:    researchCfg.AutoOnKnowledgeGap,
		BackgroundJobsEnabled: researchCfg.BackgroundJobsEnabled,
		Provider:              researchCfg.Provider,
		SearXNGURL:            researchCfg.SearXNGURL,
		MaxSources:            researchCfg.MaxSources,
		MaxFetchBytes:         researchCfg.MaxFetchBytes,
		FetchTimeout:          time.Duration(researchCfg.FetchTimeoutSeconds) * time.Second,
		JobTimeout:            time.Duration(researchCfg.JobTimeoutSeconds) * time.Second,
		MinSourcesForVerified: researchCfg.MinSourcesForVerified,
		MinTrustScore:         researchCfg.MinTrustScore,
		UserAgent:             researchCfg.UserAgent,
		BlockedDomains:        researchCfg.BlockedDomains,
		AllowedDomains:        researchCfg.AllowedDomains,
	}
}

func runLearn(args []string) error {
	fs := flag.NewFlagSet("learn", flag.ContinueOnError)
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	suitePath := fs.String("suite", "", "optional evaluation suite path")
	outDir := fs.String("out", "", "generated dataset output directory")
	trainSelectorOut := fs.String("train-selector-out", "", "optional selector checkpoint output directory")
	trainRouterOut := fs.String("train-router-out", "", "optional router checkpoint output directory (promotes only if not worse)")
	routerBaseDataset := fs.String("router-base-dataset", "datasets/router_mikros.jsonl", "base router dataset combined with harvested real-usage examples")
	epochs := fs.Int("epochs", 300, "selector training epochs when --train-selector-out is set")
	learningRate := fs.Float64("learning-rate", 0.1, "selector learning rate when --train-selector-out is set")
	if err := fs.Parse(args); err != nil {
		return err
	}
	report, err := learning.Run(context.Background(), learning.Options{
		DBPath:            *dbPath,
		SuitePath:         *suitePath,
		OutDir:            *outDir,
		TrainSelectorOut:  *trainSelectorOut,
		TrainRouterOut:    *trainRouterOut,
		RouterBaseDataset: *routerBaseDataset,
		Epochs:            *epochs,
		LearningRate:      *learningRate,
	})
	if err != nil {
		return err
	}
	fmt.Printf("learn_out: %s\n", report.OutDir)
	fmt.Printf("selector_examples: %d\n", report.SelectorExamples)
	fmt.Printf("verified_trajectories: %d\n", report.VerifiedTrajectories)
	fmt.Printf("research_examples: %d\n", report.ResearchExamples)
	fmt.Printf("skills: %d\n", report.Skills)
	fmt.Printf("selector_dataset: %s\n", report.SelectorDatasetPath)
	fmt.Printf("trajectory_dataset: %s\n", report.TrajectoryDatasetPath)
	fmt.Printf("research_dataset: %s\n", report.ResearchDatasetPath)
	if report.SelectorCheckpoint != "" {
		fmt.Printf("selector_checkpoint: %s\n", report.SelectorCheckpoint)
		fmt.Printf("selector_final_accuracy: %.4f\n", report.SelectorTrainReport.FinalAccuracy)
	}
	fmt.Printf("router_examples: %d\n", report.RouterExamples)
	if *trainRouterOut != "" {
		fmt.Printf("router_base_accuracy: %.4f\n", report.RouterBaseAccuracy)
		fmt.Printf("router_candidate_accuracy: %.4f\n", report.RouterCandidateAcc)
		fmt.Printf("router_promoted: %v\n", report.RouterPromoted)
		fmt.Printf("router_promotion: %s\n", report.RouterPromotionReason)
		if report.RouterCheckpoint != "" {
			fmt.Printf("router_checkpoint: %s\n", report.RouterCheckpoint)
		}
	}
	if *suitePath != "" {
		fmt.Printf("eval_before_verified_success_rate: %.4f\n", report.EvalBefore.VerifiedSuccessRate)
		fmt.Printf("eval_after_verified_success_rate: %.4f\n", report.EvalAfter.VerifiedSuccessRate)
	}
	return nil
}

func envDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
