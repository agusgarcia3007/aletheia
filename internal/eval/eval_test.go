package eval

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunBootstrapReportsBeamImprovement(t *testing.T) {
	root := t.TempDir()
	suite := filepath.Join(root, "evals", "bootstrap")
	if err := os.MkdirAll(suite, 0o755); err != nil {
		t.Fatal(err)
	}
	datasetDir := filepath.Join(root, "datasets")
	if err := os.MkdirAll(datasetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(datasetDir, "selector_bootstrap.jsonl"), []byte(selectorDatasetFixture()), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := RunBootstrap(context.Background(), suite)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Improved() {
		t.Fatalf("report = %+v, want beam improvement", report)
	}
	if len(report.Cases) != 3 {
		t.Fatalf("cases = %+v", report.Cases)
	}
	if report.Cases[0].CandidateGreedyStatus != "failed" || report.Cases[0].BeamStatus != "pass" {
		t.Fatalf("beam case = %+v", report.Cases[0])
	}
	if report.Cases[1].CandidateGreedyStatus != "failed" || report.Cases[1].LearnedSelectorStatus != "pass" {
		t.Fatalf("learned selector case = %+v", report.Cases[1])
	}
	if report.Cases[2].SkillReuseStatus != "pass" || report.Cases[2].SkillToolCalls >= report.Cases[2].BaselineToolCalls {
		t.Fatalf("skill reuse case = %+v", report.Cases[2])
	}
}

func selectorDatasetFixture() string {
	return `{"snapshot":{"max_tool_calls":8},"candidates":[{"action":"<ACT_RESPOND>","log_prob":0},{"action":"<ACT_RUN_TESTS>","log_prob":-0.1},{"action":"<ACT_PARSE_CODE>","log_prob":-0.5},{"action":"<ACT_MUTATE_CODE>","log_prob":-0.5},{"action":"<ACT_VERIFY>","log_prob":-0.5},{"action":"<ACT_ABSTAIN>","log_prob":-2}],"chosen":"<ACT_RUN_TESTS>","reward":1}
{"snapshot":{"has_run_tests":true,"last_verifier_status":"fail","tool_calls":1,"max_tool_calls":8},"candidates":[{"action":"<ACT_RESPOND>","log_prob":0},{"action":"<ACT_PARSE_CODE>","log_prob":-0.1},{"action":"<ACT_RUN_TESTS>","log_prob":-0.5},{"action":"<ACT_MUTATE_CODE>","log_prob":-0.5},{"action":"<ACT_VERIFY>","log_prob":-0.5},{"action":"<ACT_ABSTAIN>","log_prob":-2}],"chosen":"<ACT_PARSE_CODE>","reward":1}
{"snapshot":{"has_run_tests":true,"last_verifier_status":"fail","parsed":true,"pattern_found":true,"tool_calls":1,"max_tool_calls":8},"candidates":[{"action":"<ACT_RESPOND>","log_prob":0},{"action":"<ACT_MUTATE_CODE>","log_prob":-0.1},{"action":"<ACT_RUN_TESTS>","log_prob":-0.5},{"action":"<ACT_PARSE_CODE>","log_prob":-0.5},{"action":"<ACT_VERIFY>","log_prob":-0.5},{"action":"<ACT_ABSTAIN>","log_prob":-2}],"chosen":"<ACT_MUTATE_CODE>","reward":1}
{"snapshot":{"has_run_tests":true,"last_verifier_status":"fail","parsed":true,"pattern_found":true,"has_candidate_patch":true,"tool_calls":1,"max_tool_calls":8},"candidates":[{"action":"<ACT_RESPOND>","log_prob":0},{"action":"<ACT_VERIFY>","log_prob":-0.1},{"action":"<ACT_RUN_TESTS>","log_prob":-0.5},{"action":"<ACT_PARSE_CODE>","log_prob":-0.5},{"action":"<ACT_MUTATE_CODE>","log_prob":-0.5},{"action":"<ACT_ABSTAIN>","log_prob":-2}],"chosen":"<ACT_VERIFY>","reward":1}
{"snapshot":{"has_run_tests":true,"last_verifier_status":"pass","tool_calls":1,"max_tool_calls":8},"candidates":[{"action":"<ACT_ABSTAIN>","log_prob":0},{"action":"<ACT_RESPOND>","log_prob":-0.1},{"action":"<ACT_RUN_TESTS>","log_prob":-0.5},{"action":"<ACT_PARSE_CODE>","log_prob":-0.5},{"action":"<ACT_MUTATE_CODE>","log_prob":-0.5},{"action":"<ACT_VERIFY>","log_prob":-0.5}],"chosen":"<ACT_RESPOND>","reward":1}
{"snapshot":{"verified":true,"tool_calls":2,"max_tool_calls":8},"candidates":[{"action":"<ACT_ABSTAIN>","log_prob":0},{"action":"<ACT_RESPOND>","log_prob":-0.1},{"action":"<ACT_RUN_TESTS>","log_prob":-0.5},{"action":"<ACT_PARSE_CODE>","log_prob":-0.5},{"action":"<ACT_MUTATE_CODE>","log_prob":-0.5},{"action":"<ACT_VERIFY>","log_prob":-0.5}],"chosen":"<ACT_RESPOND>","reward":1}
`
}
