package verifier

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	StatusPass    = "pass"
	StatusFail    = "fail"
	StatusUnknown = "unknown"

	GoTestCommand      = "go test ./..."
	GoTestRaceCommand  = "go test -race ./..."
	GoTestFuzzCommand  = "go test -run ^$ -fuzz Fuzz -fuzztime 1s ./..."
	GoTestBenchCommand = "go test -bench . ./..."
	GoVetCommand       = "go vet ./..."

	GoTestName        = "go_test"
	GoTestRaceName    = "go_test_race"
	GoTestFuzzName    = "go_test_fuzz"
	GoTestBenchName   = "go_test_bench"
	GoVetName         = "go_vet"
	StaticGoParseName = "static_go_parse"

	DefaultOutputLimit = 64 * 1024
)

type Request struct {
	RepoPath       string
	SuccessCommand string
	Timeout        time.Duration
	TaskGoal       string
	PatchDiffHash  string
	OutputLimit    int
}

type CostEstimate struct {
	ToolCalls int
	Seconds   int
}

type Verifier interface {
	Name() string
	CanCheck(Request) bool
	Check(context.Context, Request) Evidence
	Cost() CostEstimate
}

type Evidence struct {
	Verifier      string
	Status        string
	Score         float64
	Command       string
	CWD           string
	Duration      time.Duration
	Stdout        string
	Stderr        string
	Artifacts     []string
	Timestamp     time.Time
	ErrorSummary  string
	BlockedReason string
}

type Result struct {
	Status    string
	Score     float64
	Evidence  []Evidence
	Aggregate Evidence
}

type Bus struct {
	Verifiers []Verifier
}

func NewBus(names []string) (Bus, error) {
	if len(names) == 0 {
		names = []string{GoTestName}
	}
	verifiers := make([]Verifier, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		switch name {
		case GoTestName:
			verifiers = append(verifiers, CommandVerifier{NameValue: GoTestName, Command: GoTestCommand, Seconds: 60})
		case GoTestRaceName:
			verifiers = append(verifiers, CommandVerifier{NameValue: GoTestRaceName, Command: GoTestRaceCommand, Seconds: 120})
		case GoTestFuzzName:
			verifiers = append(verifiers, CommandVerifier{NameValue: GoTestFuzzName, Command: GoTestFuzzCommand, Seconds: 20})
		case GoTestBenchName:
			verifiers = append(verifiers, CommandVerifier{NameValue: GoTestBenchName, Command: GoTestBenchCommand, Seconds: 20})
		case GoVetName:
			verifiers = append(verifiers, CommandVerifier{NameValue: GoVetName, Command: GoVetCommand, Seconds: 60})
		case StaticGoParseName:
			verifiers = append(verifiers, StaticGoParseVerifier{})
		default:
			return Bus{}, fmt.Errorf("unknown verifier %q", name)
		}
	}
	if len(verifiers) == 0 {
		return Bus{}, fmt.Errorf("no verifiers selected")
	}
	return Bus{Verifiers: verifiers}, nil
}

func ParseNames(csv string, includeVet bool, includeRace bool, includeFuzz bool, includeBench bool) ([]string, error) {
	var names []string
	if strings.TrimSpace(csv) == "" {
		names = append(names, GoTestName)
	} else {
		for _, part := range strings.Split(csv, ",") {
			name := strings.TrimSpace(part)
			if name != "" {
				names = append(names, name)
			}
		}
	}
	if includeVet && !contains(names, GoVetName) {
		names = append(names, GoVetName)
	}
	if includeRace && !contains(names, GoTestRaceName) {
		names = append(names, GoTestRaceName)
	}
	if includeFuzz && !contains(names, GoTestFuzzName) {
		names = append(names, GoTestFuzzName)
	}
	if includeBench && !contains(names, GoTestBenchName) {
		names = append(names, GoTestBenchName)
	}
	_, err := NewBus(names)
	return names, err
}

func (b Bus) Check(ctx context.Context, req Request) Result {
	evidence := make([]Evidence, 0, len(b.Verifiers))
	for _, v := range b.Verifiers {
		if !v.CanCheck(req) {
			evidence = append(evidence, Evidence{
				Verifier:      v.Name(),
				Status:        StatusUnknown,
				Score:         0,
				CWD:           req.RepoPath,
				Timestamp:     time.Now().UTC(),
				ErrorSummary:  "verifier cannot check request",
				BlockedReason: "can_check_false",
			})
			continue
		}
		ev := v.Check(ctx, req)
		evidence = append(evidence, ev)
	}
	return Aggregate(evidence)
}

func Aggregate(evidence []Evidence) Result {
	status := StatusPass
	score := 1.0
	if len(evidence) == 0 {
		status = StatusUnknown
		score = 0
	}
	for _, ev := range evidence {
		switch ev.Status {
		case StatusFail:
			status = StatusFail
			score = 0
		case StatusUnknown:
			if status != StatusFail {
				status = StatusUnknown
				score = 0
			}
		}
	}
	names := make([]string, 0, len(evidence))
	for _, ev := range evidence {
		names = append(names, ev.Verifier)
	}
	aggregateVerifier := "bus"
	if len(evidence) == 1 {
		aggregateVerifier = evidence[0].Verifier
	}
	return Result{
		Status:   status,
		Score:    score,
		Evidence: evidence,
		Aggregate: Evidence{
			Verifier:  aggregateVerifier,
			Status:    status,
			Score:     score,
			Artifacts: names,
			Timestamp: time.Now().UTC(),
		},
	}
}

type CommandVerifier struct {
	NameValue string
	Command   string
	Seconds   int
}

func (v CommandVerifier) Name() string {
	return v.NameValue
}

func (v CommandVerifier) CanCheck(req Request) bool {
	return IsAllowed(v.Command) && req.RepoPath != ""
}

func (v CommandVerifier) Check(ctx context.Context, req Request) Evidence {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = time.Duration(v.Seconds) * time.Second
	}
	return RunSandboxed(ctx, req.RepoPath, v.Command, timeout, req.OutputLimit, v.Name())
}

func (v CommandVerifier) Cost() CostEstimate {
	seconds := v.Seconds
	if seconds == 0 {
		seconds = 60
	}
	return CostEstimate{ToolCalls: 1, Seconds: seconds}
}

type StaticGoParseVerifier struct{}

func (StaticGoParseVerifier) Name() string {
	return StaticGoParseName
}

func (StaticGoParseVerifier) CanCheck(req Request) bool {
	return req.RepoPath != ""
}

func (StaticGoParseVerifier) Cost() CostEstimate {
	return CostEstimate{ToolCalls: 0, Seconds: 5}
}

func (StaticGoParseVerifier) Check(_ context.Context, req Request) Evidence {
	start := time.Now()
	ev := Evidence{
		Verifier:  StaticGoParseName,
		Status:    StatusPass,
		Score:     1,
		CWD:       req.RepoPath,
		Timestamp: start.UTC(),
	}
	repo, err := cleanRepoPath(req.RepoPath)
	if err != nil {
		ev.Status = StatusUnknown
		ev.Score = 0
		ev.ErrorSummary = err.Error()
		ev.Duration = time.Since(start)
		return ev
	}
	var parsed int
	fset := token.NewFileSet()
	err = filepath.WalkDir(repo, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		parsed++
		if _, err := parser.ParseFile(fset, path, nil, parser.AllErrors); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		return nil
	})
	ev.Duration = time.Since(start)
	if err != nil {
		ev.Status = StatusFail
		ev.Score = 0
		ev.ErrorSummary = err.Error()
		ev.Stderr = err.Error()
		return ev
	}
	ev.Stdout = fmt.Sprintf("parsed %d Go files", parsed)
	return ev
}

func IsAllowed(command string) bool {
	_, ok := allowedCommand(normalize(command))
	return ok
}

func IsReadOnlyCommand(command string) bool {
	spec, ok := allowedCommand(normalize(command))
	if !ok {
		return false
	}
	return spec.ReadOnly
}

func RunSuccess(ctx context.Context, repoPath string, command string, timeout time.Duration) (Evidence, error) {
	if !IsAllowed(command) {
		ev := blockedEvidence("command_allowlist", repoPath, command, fmt.Sprintf("unsupported success command %q", command))
		return ev, fmt.Errorf("unsupported success command %q", command)
	}
	return RunSandboxed(ctx, repoPath, command, timeout, DefaultOutputLimit, commandName(command)), nil
}

func RunGoTest(ctx context.Context, repoPath string, timeout time.Duration) (Evidence, error) {
	return RunSandboxed(ctx, repoPath, GoTestCommand, timeout, DefaultOutputLimit, GoTestName), nil
}

func RunSandboxed(ctx context.Context, repoPath string, command string, timeout time.Duration, outputLimit int, verifierName string) Evidence {
	start := time.Now()
	normalized := normalize(command)
	spec, ok := allowedCommand(normalized)
	if !ok {
		return blockedEvidence(verifierName, repoPath, command, fmt.Sprintf("command %q is not allowlisted", command))
	}
	repo, err := cleanRepoPath(repoPath)
	if err != nil {
		ev := blockedEvidence(verifierName, repoPath, command, err.Error())
		ev.Duration = time.Since(start)
		return ev
	}
	if outputLimit <= 0 {
		outputLimit = DefaultOutputLimit
	}
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, spec.Argv[0], spec.Argv[1:]...)
	cmd.Dir = repo
	stdout := &limitedBuffer{limit: outputLimit}
	stderr := &limitedBuffer{limit: outputLimit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err = cmd.Run()
	ev := Evidence{
		Verifier:  verifierName,
		Status:    StatusPass,
		Score:     1,
		Command:   normalized,
		CWD:       repo,
		Duration:  time.Since(start),
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		Timestamp: start.UTC(),
	}
	if stdout.truncated {
		ev.Artifacts = append(ev.Artifacts, "stdout_truncated")
	}
	if stderr.truncated {
		ev.Artifacts = append(ev.Artifacts, "stderr_truncated")
	}
	if runCtx.Err() == context.DeadlineExceeded {
		ev.Status = StatusUnknown
		ev.Score = 0
		ev.ErrorSummary = "verifier timed out"
		ev.Stderr = appendLine(ev.Stderr, "verifier timed out")
		return ev
	}
	if err != nil {
		ev.Status = StatusFail
		ev.Score = 0
		ev.ErrorSummary = err.Error()
	}
	return ev
}

func Payload(ev Evidence, action string, verifierNames []string, patchDiffHash string) string {
	payload := map[string]any{
		"action":          action,
		"verifier":        ev.Verifier,
		"status":          ev.Status,
		"command":         ev.Command,
		"cwd":             ev.CWD,
		"duration_ms":     ev.Duration.Milliseconds(),
		"blocked_reason":  ev.BlockedReason,
		"error_summary":   ev.ErrorSummary,
		"patch_diff_hash": patchDiffHash,
		"verifiers":       verifierNames,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func Names(verifiers []Verifier) []string {
	out := make([]string, 0, len(verifiers))
	for _, verifier := range verifiers {
		out = append(out, verifier.Name())
	}
	return out
}

func cleanRepoPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("repo path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repo path %s is not a directory", abs)
	}
	return abs, nil
}

type CommandSpec struct {
	Argv     []string
	ReadOnly bool
}

func allowedCommand(command string) (CommandSpec, bool) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return CommandSpec{}, false
	}
	switch fields[0] {
	case "go":
		return allowedGoCommand(fields)
	case "rg":
		return allowedRGCommand(fields)
	case "git":
		return allowedGitCommand(fields)
	case "cat":
		return allowedCatCommand(fields)
	case "ls":
		return allowedLSCommand(fields)
	default:
		return CommandSpec{}, false
	}
}

func allowedGoCommand(fields []string) (CommandSpec, bool) {
	if len(fields) == 2 && fields[1] == "test" {
		return CommandSpec{Argv: fields}, true
	}
	if len(fields) == 3 {
		switch {
		case fields[1] == "test" && fields[2] == "./...":
			return CommandSpec{Argv: fields}, true
		case fields[1] == "vet" && fields[2] == "./...":
			return CommandSpec{Argv: fields}, true
		}
	}
	if len(fields) == 4 && fields[1] == "test" && fields[2] == "-race" && fields[3] == "./..." {
		return CommandSpec{Argv: fields}, true
	}
	if (len(fields) == 4 || len(fields) == 5) && fields[1] == "test" {
		switch fields[2] {
		case "-run", "-bench":
			if !safePattern(fields[3]) {
				return CommandSpec{}, false
			}
			if len(fields) == 5 && fields[4] != "./..." {
				return CommandSpec{}, false
			}
			return CommandSpec{Argv: fields}, true
		}
	}
	if len(fields) == 9 && fields[1] == "test" && fields[2] == "-run" && fields[4] == "-fuzz" && fields[6] == "-fuzztime" && fields[8] == "./..." {
		if !safePattern(fields[3]) || !safePattern(fields[5]) || !safeDuration(fields[7]) {
			return CommandSpec{}, false
		}
		return CommandSpec{Argv: fields}, true
	}
	return CommandSpec{}, false
}

func allowedRGCommand(fields []string) (CommandSpec, bool) {
	if len(fields) < 2 || len(fields) > 3 {
		return CommandSpec{}, false
	}
	if strings.HasPrefix(fields[1], "-") {
		return CommandSpec{}, false
	}
	if len(fields) == 3 && !safeRelativePath(fields[2]) {
		return CommandSpec{}, false
	}
	return CommandSpec{Argv: fields, ReadOnly: true}, true
}

func allowedGitCommand(fields []string) (CommandSpec, bool) {
	if len(fields) != 2 {
		return CommandSpec{}, false
	}
	switch fields[1] {
	case "diff", "status":
		return CommandSpec{Argv: fields, ReadOnly: true}, true
	default:
		return CommandSpec{}, false
	}
}

func allowedCatCommand(fields []string) (CommandSpec, bool) {
	if len(fields) != 2 || !safeRelativePath(fields[1]) {
		return CommandSpec{}, false
	}
	return CommandSpec{Argv: fields, ReadOnly: true}, true
}

func allowedLSCommand(fields []string) (CommandSpec, bool) {
	if len(fields) > 2 {
		return CommandSpec{}, false
	}
	if len(fields) == 2 && !safeRelativePath(fields[1]) {
		return CommandSpec{}, false
	}
	return CommandSpec{Argv: fields, ReadOnly: true}, true
}

func safePattern(pattern string) bool {
	return pattern != "" && !strings.HasPrefix(pattern, "-") && !strings.ContainsAny(pattern, "\x00\n\r")
}

func safeDuration(value string) bool {
	if value == "" || strings.ContainsAny(value, "\x00\n\r") {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && r != 'm' && r != 's' && r != 'h' && r != '.' {
			return false
		}
	}
	return true
}

func safeRelativePath(path string) bool {
	if path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\n\r") {
		return false
	}
	clean := filepath.Clean(path)
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func commandName(command string) string {
	normalized := normalize(command)
	fields := strings.Fields(normalized)
	if len(fields) >= 2 && fields[0] == "go" && fields[1] == "test" {
		if len(fields) >= 3 && fields[2] == "-race" {
			return GoTestRaceName
		}
		for _, field := range fields {
			if field == "-fuzz" {
				return GoTestFuzzName
			}
			if field == "-bench" {
				return GoTestBenchName
			}
		}
		return GoTestName
	}
	if normalized == GoVetCommand {
		return GoVetName
	}
	return "run_cmd"
}

func blockedEvidence(verifierName string, repoPath string, command string, reason string) Evidence {
	blockedReason := "command_allowlist"
	if !strings.Contains(reason, "allowlist") && !strings.Contains(reason, "unsupported success command") {
		blockedReason = reason
	}
	return Evidence{
		Verifier:      verifierName,
		Status:        StatusUnknown,
		Score:         0,
		Command:       normalize(command),
		CWD:           repoPath,
		Timestamp:     time.Now().UTC(),
		ErrorSummary:  reason,
		BlockedReason: blockedReason,
		Stderr:        reason,
	}
}

func normalize(command string) string {
	return strings.Join(strings.Fields(command), " ")
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func appendLine(text string, line string) string {
	if text == "" {
		return line
	}
	return text + "\n" + line
}

type limitedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return len(p), nil
	}
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if len(p) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	_, err := b.buf.Write(p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	if b.truncated {
		return b.buf.String() + "\n[truncated]"
	}
	return b.buf.String()
}

var _ io.Writer = (*limitedBuffer)(nil)
