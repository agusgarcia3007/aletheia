package verifier

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunGoTestPassAndFail(t *testing.T) {
	passDir := writeGoModule(t, true)
	failDir := writeGoModule(t, false)

	passEv, err := RunSuccess(context.Background(), passDir, GoTestCommand, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if passEv.Status != "pass" {
		t.Fatalf("pass status = %q, stderr:\n%s", passEv.Status, passEv.Stderr)
	}

	failEv, err := RunSuccess(context.Background(), failDir, GoTestCommand, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if failEv.Status != "fail" {
		t.Fatalf("fail status = %q, stdout:\n%s\nstderr:\n%s", failEv.Status, failEv.Stdout, failEv.Stderr)
	}
}

func TestRunSuccessRejectsUnsupportedCommand(t *testing.T) {
	_, err := RunSuccess(context.Background(), t.TempDir(), "rm -rf /", time.Second)
	if err == nil {
		t.Fatal("expected unsupported command error")
	}
}

func writeGoModule(t *testing.T, passing bool) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module tempmod\n\ngo 1.26\n")
	mustWrite(t, filepath.Join(dir, "calc.go"), "package tempmod\n\nfunc Add(a, b int) int { return a + b }\n")
	want := "5"
	if !passing {
		want = "6"
	}
	mustWrite(t, filepath.Join(dir, "calc_test.go"), `package tempmod

import "testing"

func TestAdd(t *testing.T) {
	if got := Add(2, 3); got != `+want+` {
		t.Fatalf("Add(2, 3) = %d", got)
	}
}
`)
	return dir
}

func mustWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
