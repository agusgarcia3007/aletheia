package skills

import (
	"os"
	"path/filepath"
	"testing"

	"aletheia/internal/selector"
)

func TestDetectTrigger(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "calculator.go"), []byte("package calculator\nfunc Add(a,b int) int { return a - b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	trigger, ok, err := DetectTrigger(repo, "go test ./...")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || trigger != TriggerCalculatorSub {
		t.Fatalf("trigger = %q ok=%v", trigger, ok)
	}
}

func TestDetectTriggerNoPattern(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "calculator.go"), []byte("package calculator\nfunc Add(a,b int) int { return a + b }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, ok, err := DetectTrigger(repo, "go test ./...")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("trigger should not match fixed code")
	}
}

func TestCompressVerifiedTrace(t *testing.T) {
	def, ok := Compress([]string{
		selector.ActRunTests,
		selector.ActParseCode,
		selector.ActMutateCode,
		selector.ActVerify,
		selector.ActRespond,
	}, true)
	if !ok {
		t.Fatal("expected compression")
	}
	if def.Name != FixSimpleGoTestFailure || len(def.ActionSequence) != 4 || def.ActionSequence[0] != selector.ActParseCode {
		t.Fatalf("definition = %+v", def)
	}
}

func TestCompressRejectsUnverifiedTrace(t *testing.T) {
	_, ok := Compress([]string{selector.ActParseCode, selector.ActMutateCode, selector.ActVerify, selector.ActRespond}, false)
	if ok {
		t.Fatal("unverified trace should not compress")
	}
}

func TestCompressRejectsOutOfOrderTrace(t *testing.T) {
	_, ok := Compress([]string{selector.ActVerify, selector.ActParseCode, selector.ActMutateCode, selector.ActRespond}, true)
	if ok {
		t.Fatal("out-of-order trace should not compress")
	}
}

func TestActionSequenceRoundTrip(t *testing.T) {
	text, err := MarshalActionSequence([]string{selector.ActParseCode, selector.ActMutateCode})
	if err != nil {
		t.Fatal(err)
	}
	actions, err := UnmarshalActionSequence(text)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 2 || actions[1] != selector.ActMutateCode {
		t.Fatalf("actions = %+v", actions)
	}
}
