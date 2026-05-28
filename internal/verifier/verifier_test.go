package verifier

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	if passEv.Status != StatusPass || passEv.Command != GoTestCommand || passEv.CWD == "" {
		t.Fatalf("pass evidence = %+v", passEv)
	}

	failEv, err := RunSuccess(context.Background(), failDir, GoTestCommand, 20*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if failEv.Status != StatusFail {
		t.Fatalf("fail status = %q, stdout:\n%s\nstderr:\n%s", failEv.Status, failEv.Stdout, failEv.Stderr)
	}
}

func TestRunSuccessRejectsUnsupportedCommand(t *testing.T) {
	ev, err := RunSuccess(context.Background(), t.TempDir(), "rm -rf /", time.Second)
	if err == nil {
		t.Fatal("expected unsupported command error")
	}
	if ev.Status != StatusUnknown || ev.BlockedReason == "" {
		t.Fatalf("blocked evidence = %+v", ev)
	}
}

func TestRunSandboxedTimeoutAndOutputCap(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module tempmod\n\ngo 1.26\n")
	mustWrite(t, filepath.Join(dir, "slow_test.go"), `package tempmod

import (
	"strings"
	"testing"
	"time"
)

func TestSlow(t *testing.T) {
	t.Fatal(strings.Repeat("x", 1024))
	time.Sleep(time.Second)
}
`)
	ev := RunSandboxed(context.Background(), dir, GoTestCommand, 20*time.Second, 64, GoTestName)
	if ev.Status != StatusFail {
		t.Fatalf("status = %s, want fail", ev.Status)
	}
	if !strings.Contains(ev.Stdout, "[truncated]") && !strings.Contains(ev.Stderr, "[truncated]") {
		t.Fatalf("expected truncated output: stdout=%q stderr=%q", ev.Stdout, ev.Stderr)
	}

	mustWrite(t, filepath.Join(dir, "slow_test.go"), `package tempmod

import (
	"testing"
	"time"
)

func TestSlow(t *testing.T) {
	time.Sleep(500 * time.Millisecond)
}
`)
	ev = RunSandboxed(context.Background(), dir, GoTestCommand, time.Millisecond, DefaultOutputLimit, GoTestName)
	if ev.Status != StatusUnknown || !strings.Contains(ev.ErrorSummary, "timed out") {
		t.Fatalf("timeout evidence = %+v", ev)
	}
}

func TestGoVetAndStaticParse(t *testing.T) {
	vetDir := t.TempDir()
	mustWrite(t, filepath.Join(vetDir, "go.mod"), "module vetmod\n\ngo 1.26\n")
	mustWrite(t, filepath.Join(vetDir, "main.go"), `package vetmod

import "fmt"

func Bad() {
	fmt.Printf("%d", "not-int")
}
`)
	vetEv := CommandVerifier{NameValue: GoVetName, Command: GoVetCommand, Seconds: 20}.Check(context.Background(), Request{RepoPath: vetDir, Timeout: 20 * time.Second})
	if vetEv.Status != StatusFail {
		t.Fatalf("go vet evidence = %+v", vetEv)
	}

	parseDir := t.TempDir()
	mustWrite(t, filepath.Join(parseDir, "go.mod"), "module parsemod\n\ngo 1.26\n")
	mustWrite(t, filepath.Join(parseDir, "bad.go"), "package parsemod\n\nfunc Bad( {\n")
	parseEv := StaticGoParseVerifier{}.Check(context.Background(), Request{RepoPath: parseDir})
	if parseEv.Status != StatusFail {
		t.Fatalf("parse evidence = %+v", parseEv)
	}
}

func TestBusAggregates(t *testing.T) {
	pass := Evidence{Verifier: "a", Status: StatusPass, Score: 1}
	fail := Evidence{Verifier: "b", Status: StatusFail, Score: 0}
	unknown := Evidence{Verifier: "c", Status: StatusUnknown, Score: 0}
	if got := Aggregate([]Evidence{pass, pass}).Status; got != StatusPass {
		t.Fatalf("all pass = %s", got)
	}
	if got := Aggregate([]Evidence{pass, unknown}).Status; got != StatusUnknown {
		t.Fatalf("unknown aggregate = %s", got)
	}
	if got := Aggregate([]Evidence{pass, unknown, fail}).Status; got != StatusFail {
		t.Fatalf("fail aggregate = %s", got)
	}
}

func TestParseNames(t *testing.T) {
	names, err := ParseNames("static_go_parse,go_test", true, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{StaticGoParseName, GoTestName, GoVetName}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("names = %v, want %v", names, want)
	}
	if _, err := ParseNames("rm -rf /", false, false); err == nil {
		t.Fatal("expected unknown verifier error")
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
