package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aletheia/internal/selector"
)

const (
	FixSimpleGoTestFailure = "fix_simple_go_test_failure"
	TriggerCalculatorSub   = "go_test:calculator_return_subtract"
)

type Definition struct {
	Name           string
	Trigger        string
	ActionSequence []string
}

func DetectTrigger(repoPath string, successCommand string) (string, bool, error) {
	if strings.TrimSpace(successCommand) != "go test ./..." {
		return "", false, nil
	}
	raw, err := os.ReadFile(filepath.Join(repoPath, "calculator.go"))
	if os.IsNotExist(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if strings.Contains(string(raw), "return a - b") {
		return TriggerCalculatorSub, true, nil
	}
	return "", false, nil
}

func DefinitionForTrigger(trigger string) (Definition, bool) {
	switch trigger {
	case TriggerCalculatorSub:
		return Definition{
			Name:    FixSimpleGoTestFailure,
			Trigger: TriggerCalculatorSub,
			ActionSequence: []string{
				selector.ActParseCode,
				selector.ActMutateCode,
				selector.ActVerify,
				selector.ActRespond,
			},
		}, true
	default:
		return Definition{}, false
	}
}

func FilesForTrigger(trigger string) ([]string, bool) {
	switch trigger {
	case TriggerCalculatorSub:
		return []string{"calculator.go"}, true
	default:
		return nil, false
	}
}

func Compress(actions []string, verified bool) (Definition, bool) {
	if !verified {
		return Definition{}, false
	}
	want := []string{
		selector.ActParseCode,
		selector.ActMutateCode,
		selector.ActVerify,
		selector.ActRespond,
	}
	if containsOrdered(actions, want) {
		def, _ := DefinitionForTrigger(TriggerCalculatorSub)
		return def, true
	}
	return Definition{}, false
}

func containsOrdered(actions []string, want []string) bool {
	next := 0
	for _, action := range actions {
		if next < len(want) && action == want[next] {
			next++
		}
	}
	return next == len(want)
}

func MarshalActionSequence(actions []string) (string, error) {
	for _, action := range actions {
		if !selector.IsFunctional(action) {
			return "", fmt.Errorf("unsupported skill action %q", action)
		}
	}
	raw, err := json.Marshal(actions)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func UnmarshalActionSequence(text string) ([]string, error) {
	var actions []string
	if err := json.Unmarshal([]byte(text), &actions); err != nil {
		return nil, err
	}
	for _, action := range actions {
		if !selector.IsFunctional(action) {
			return nil, fmt.Errorf("unsupported skill action %q", action)
		}
	}
	return actions, nil
}
