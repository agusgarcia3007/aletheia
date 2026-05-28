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
  aletheia solve --task examples/buggy-go/task.json [--db %s]
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

func runSolve(args []string) error {
	fs := flag.NewFlagSet("solve", flag.ContinueOnError)
	taskPath := fs.String("task", "", "task JSON path")
	dbPath := fs.String("db", memory.DefaultDBPath, "SQLite memory database path")
	timeout := fs.Duration("timeout", 60*time.Second, "verifier timeout")
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

	solver := cognitivevm.Solver{
		DBPath:          *dbPath,
		VerifierTimeout: *timeout,
	}
	result, err := solver.SolveFile(context.Background(), *taskPath, wd)
	if err != nil {
		return err
	}

	fmt.Printf("goal: %s\n", result.Task.Goal)
	fmt.Printf("repo: %s\n", result.RepoPath)
	fmt.Printf("initial verifier: %s %s\n", result.Initial.Verifier, result.Initial.Status)
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
