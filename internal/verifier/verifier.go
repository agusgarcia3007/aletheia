package verifier

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const GoTestCommand = "go test ./..."

type Evidence struct {
	Verifier  string
	Status    string
	Score     float64
	Stdout    string
	Stderr    string
	Timestamp time.Time
}

func IsAllowed(command string) bool {
	return normalize(command) == GoTestCommand
}

func RunSuccess(ctx context.Context, repoPath string, command string, timeout time.Duration) (Evidence, error) {
	if !IsAllowed(command) {
		return Evidence{
			Verifier:  "command_allowlist",
			Status:    "unknown",
			Score:     0,
			Stderr:    fmt.Sprintf("unsupported success command %q", command),
			Timestamp: time.Now().UTC(),
		}, fmt.Errorf("unsupported success command %q", command)
	}
	return RunGoTest(ctx, repoPath, timeout)
}

func RunGoTest(ctx context.Context, repoPath string, timeout time.Duration) (Evidence, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "go", "test", "./...")
	cmd.Dir = repoPath
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	ev := Evidence{
		Verifier:  "go_test",
		Status:    "pass",
		Score:     1,
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		Timestamp: time.Now().UTC(),
	}
	if runCtx.Err() == context.DeadlineExceeded {
		ev.Status = "unknown"
		ev.Score = 0
		ev.Stderr += "\nverifier timed out"
		return ev, runCtx.Err()
	}
	if err != nil {
		ev.Status = "fail"
		ev.Score = 0
		return ev, nil
	}
	return ev, nil
}

func normalize(command string) string {
	return strings.Join(strings.Fields(command), " ")
}
