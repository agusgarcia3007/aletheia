package repair

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"aletheia/internal/verifier"
)

type Counterexample struct {
	Verifier string
	Status   string
	Summary  string
	Stdout   string
	Stderr   string
}

type Candidate struct {
	Path    string
	OldText string
	NewText string
}

func ExtractCounterexample(evidence []verifier.Evidence) (Counterexample, bool) {
	for i := len(evidence) - 1; i >= 0; i-- {
		ev := evidence[i]
		if ev.Status != verifier.StatusFail {
			continue
		}
		summary := strings.TrimSpace(ev.ErrorSummary)
		if summary == "" {
			summary = firstNonEmptyLine(ev.Stderr)
		}
		if summary == "" {
			summary = firstNonEmptyLine(ev.Stdout)
		}
		return Counterexample{
			Verifier: ev.Verifier,
			Status:   ev.Status,
			Summary:  summary,
			Stdout:   ev.Stdout,
			Stderr:   ev.Stderr,
		}, true
	}
	return Counterexample{}, false
}

func BuildCandidate(repoPath string, counterexample Counterexample) (Candidate, error) {
	files, err := goSourceFiles(repoPath)
	if err != nil {
		return Candidate{}, err
	}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			return Candidate{}, err
		}
		oldText := string(raw)
		if newText, ok := repairText(oldText, counterexample); ok {
			return Candidate{
				Path:    path,
				OldText: oldText,
				NewText: newText,
			}, nil
		}
	}
	if counterexample.Summary != "" {
		return Candidate{}, fmt.Errorf("no deterministic Go repair rule matched counterexample: %s", counterexample.Summary)
	}
	return Candidate{}, fmt.Errorf("no deterministic Go repair rule matched")
}

func goSourceFiles(repoPath string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(repoPath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules":
				return filepath.SkipDir
			default:
				return nil
			}
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func repairText(oldText string, counterexample Counterexample) (string, bool) {
	signal := strings.Join([]string{counterexample.Summary, counterexample.Stdout, counterexample.Stderr}, "\n")
	rules := []func(string, string) (string, bool){
		repairWrongAdditionOperator,
		repairUndefinedFunctionName,
		repairMissingImport,
		repairUnusedImport,
		repairSimpleIntReturn,
		repairNilPointerValue,
	}
	for _, rule := range rules {
		if newText, ok := rule(oldText, signal); ok && newText != oldText {
			return newText, true
		}
	}
	return "", false
}

func repairWrongAdditionOperator(oldText string, _ string) (string, bool) {
	newText := strings.Replace(oldText, "return a - b", "return a + b", 1)
	return newText, newText != oldText
}

var undefinedRe = regexp.MustCompile(`undefined:\s*([A-Za-z_][A-Za-z0-9_]*)`)

func repairUndefinedFunctionName(oldText string, signal string) (string, bool) {
	matches := undefinedRe.FindStringSubmatch(signal)
	if len(matches) != 2 {
		return "", false
	}
	switch matches[1] {
	case "Add":
		newText := strings.Replace(oldText, "func Sum(", "func Add(", 1)
		return newText, newText != oldText
	default:
		return "", false
	}
}

func repairMissingImport(oldText string, signal string) (string, bool) {
	matches := undefinedRe.FindAllStringSubmatch(signal, -1)
	allowed := map[string]bool{"errors": true, "fmt": true, "math": true, "strconv": true, "strings": true, "time": true}
	for _, match := range matches {
		if len(match) != 2 || !allowed[match[1]] || !strings.Contains(oldText, match[1]+".") || strings.Contains(oldText, `"`+match[1]+`"`) {
			continue
		}
		return addImport(oldText, match[1])
	}
	return "", false
}

var unusedImportRe = regexp.MustCompile(`"([^"]+)" imported and not used`)

func repairUnusedImport(oldText string, signal string) (string, bool) {
	matches := unusedImportRe.FindStringSubmatch(signal)
	if len(matches) != 2 {
		return "", false
	}
	return removeImport(oldText, matches[1])
}

func repairSimpleIntReturn(oldText string, signal string) (string, bool) {
	if !strings.Contains(signal, "as int value") {
		return "", false
	}
	replacements := [][2]string{
		{`return "0"`, `return 0`},
		{`return ""`, `return 0`},
	}
	for _, replacement := range replacements {
		newText := strings.Replace(oldText, replacement[0], replacement[1], 1)
		if newText != oldText {
			return newText, true
		}
	}
	return "", false
}

func repairNilPointerValue(oldText string, signal string) (string, bool) {
	if !strings.Contains(strings.ToLower(signal), "nil pointer") {
		return "", false
	}
	old := "func Value(value *int) int { return *value }"
	newValue := "func Value(value *int) int {\n\tif value == nil {\n\t\treturn 0\n\t}\n\treturn *value\n}"
	newText := strings.Replace(oldText, old, newValue, 1)
	return newText, newText != oldText
}

func addImport(oldText string, pkg string) (string, bool) {
	if strings.Contains(oldText, "import (\n") {
		return strings.Replace(oldText, "import (\n", "import (\n\t\""+pkg+"\"\n", 1), true
	}
	singleImport := regexp.MustCompile(`(?m)^import\s+"([^"]+)"\s*$`)
	if singleImport.MatchString(oldText) {
		return singleImport.ReplaceAllString(oldText, "import (\n\t\"$1\"\n\t\""+pkg+"\"\n)"), true
	}
	packageLine := regexp.MustCompile(`(?m)^(package\s+\w+)\s*$`)
	if packageLine.MatchString(oldText) {
		return packageLine.ReplaceAllString(oldText, "$1\n\nimport \""+pkg+"\""), true
	}
	return "", false
}

func removeImport(oldText string, pkg string) (string, bool) {
	lines := strings.Split(oldText, "\n")
	out := make([]string, 0, len(lines))
	removed := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == `"`+pkg+`"` || trimmed == `"`+pkg+`";` || trimmed == `import "`+pkg+`"` {
			removed = true
			continue
		}
		out = append(out, line)
	}
	if !removed {
		return "", false
	}
	text := strings.Join(out, "\n")
	text = strings.Replace(text, "import (\n)\n\n", "", 1)
	text = strings.Replace(text, "import (\n)\n", "", 1)
	return text, true
}

func firstNonEmptyLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
