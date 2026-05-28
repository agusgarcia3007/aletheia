package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aletheia/internal/cognitivevm"
	"aletheia/internal/config"
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
	case "config":
		return runConfig(args[2:])
	case "init":
		return runInit(args[2:])
	case "train":
		return runTrain(args[2:])
	case "train-selector":
		return runTrainSelector(args[2:])
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
  aletheia init [--config configs/micro.yaml] [--db %s]
  aletheia config inspect --config configs/micro.yaml
  aletheia train --config configs/tiny.yaml --dataset datasets/bootstrap_actions.jsonl --out checkpoints/tiny-actions
  aletheia train-selector --dataset datasets/selector_bootstrap.jsonl --out checkpoints/selector-bootstrap
  aletheia run --checkpoint checkpoints/tiny-actions --prompt "<USER>fix failing go test<ASSISTANT>"
  aletheia index ./docs [--config configs/micro.yaml] [--db %s]
  aletheia ask --query "qué decisión tomamos sobre el selector?" [--config configs/micro.yaml] [--db %s]
  aletheia memory inspect [--config configs/micro.yaml] [--db %s]
  aletheia memory skills [--config configs/micro.yaml] [--db %s]
  aletheia solve --task examples/buggy-go/task.json [--config configs/micro.yaml] [--db %s] [--checkpoint checkpoints/tiny-actions] [--selector-checkpoint checkpoints/selector-bootstrap] [--use-skills] [--search greedy|beam] [--beam-width 4] [--max-depth 8] [--verifier go_test,static_go_parse] [--trace]
  aletheia eval --suite evals/bootstrap [--json]
`, memory.DefaultDBPath, memory.DefaultDBPath, memory.DefaultDBPath, memory.DefaultDBPath, memory.DefaultDBPath, memory.DefaultDBPath)
	return flag.ErrHelp
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

func runModel(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	configPath := fs.String("config", "", "configuration YAML path")
	checkpoint := fs.String("checkpoint", "checkpoints/tiny-actions", "checkpoint directory")
	prompt := fs.String("prompt", "", "prompt text")
	maxTokens := fs.Int("max-tokens", 32, "maximum generated tokens")
	topK := fs.Int("top-k", 8, "top-k candidates to print from the first step")
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
	if len(stats.LatestPaths) > 0 {
		fmt.Println("latest:")
		for _, path := range stats.LatestPaths {
			fmt.Printf("  %s\n", path)
		}
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

func runSolve(args []string) error {
	fs := flag.NewFlagSet("solve", flag.ContinueOnError)
	configPath := fs.String("config", "", "configuration YAML path")
	taskPath := fs.String("task", "", "task JSON path")
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	timeout := fs.Duration("timeout", 60*time.Second, "verifier timeout")
	checkpoint := fs.String("checkpoint", "", "optional model checkpoint directory")
	selectorCheckpoint := fs.String("selector-checkpoint", "", "optional learned selector checkpoint directory")
	maxSteps := fs.Int("max-steps", 8, "maximum Cognitive VM action steps")
	searchStrategy := fs.String("search", "greedy", "search strategy: greedy or beam")
	beamWidth := fs.Int("beam-width", 4, "beam search width")
	maxDepth := fs.Int("max-depth", 0, "beam search maximum depth; defaults to --max-steps")
	useSkills := fs.Bool("use-skills", false, "reuse verified compressed skills when trigger matches")
	verifierNamesCSV := fs.String("verifier", verifier.GoTestName, "comma-separated verifier names")
	includeVet := fs.Bool("vet", false, "also run go_vet verifier")
	includeRace := fs.Bool("race", false, "also run go_test_race verifier")
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
	verifierNames, err := verifier.ParseNames(verifierCSV, *includeVet, *includeRace)
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

	report, err := eval.RunBootstrap(context.Background(), *suitePath)
	if err != nil {
		return err
	}
	if *jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	fmt.Printf("eval suite: %s\n", report.Suite.Path)
	fmt.Printf("status: bootstrap ready\n")
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
	fmt.Printf("  tool_calls_per_success: %.4f\n", report.Metrics.ToolCallsPerSuccess)
	fmt.Printf("  seconds_per_success: %.4f\n", report.Metrics.SecondsPerSuccess)
	fmt.Printf("  memory_hit_rate: %.4f\n", report.Metrics.MemoryHitRate)
	if !report.Improved() {
		return fmt.Errorf("eval bootstrap did not show beam improvement")
	}
	return nil
}
