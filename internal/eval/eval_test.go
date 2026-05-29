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
	if len(report.Cases) != 9 {
		t.Fatalf("cases = %+v", report.Cases)
	}
	for i, name := range []string{"go_compile", "go_tests", "doc_qa", "abstention", "memory"} {
		if report.Cases[i].Name != name || report.Cases[i].Status != "pass" {
			t.Fatalf("case %d = %+v", i, report.Cases[i])
		}
	}
	if report.Cases[5].CandidateGreedyStatus != "failed" || report.Cases[5].BeamStatus != "pass" {
		t.Fatalf("beam case = %+v", report.Cases[5])
	}
	if report.Cases[6].CandidateGreedyStatus != "failed" || report.Cases[6].MCTSStatus != "pass" {
		t.Fatalf("mcts case = %+v", report.Cases[6])
	}
	if report.Cases[7].CandidateGreedyStatus != "failed" || report.Cases[7].LearnedSelectorStatus != "pass" {
		t.Fatalf("learned selector case = %+v", report.Cases[7])
	}
	if report.Cases[8].SkillReuseStatus != "pass" || report.Cases[8].SkillToolCalls >= report.Cases[8].BaselineToolCalls {
		t.Fatalf("skill reuse case = %+v", report.Cases[8])
	}
	if report.Metrics.VerifiedSuccessRate != 1 || report.Metrics.AbstentionAccuracy != 1 || report.Metrics.MemoryHitRate != 1 {
		t.Fatalf("metrics = %+v", report.Metrics)
	}
}

func TestRunProductionReportsReleaseGateMetrics(t *testing.T) {
	root := t.TempDir()
	suite := filepath.Join(root, "evals", "production")
	if err := os.MkdirAll(suite, 0o755); err != nil {
		t.Fatal(err)
	}
	report, err := Run(context.Background(), suite)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Improved() {
		t.Fatalf("report = %+v", report)
	}
	if len(report.Cases) != 100 {
		t.Fatalf("cases = %d, want 100", len(report.Cases))
	}
	if report.Metrics.FalseVerifiedRate != 0 || report.Metrics.CitationValidity < 0.98 || report.Metrics.RepairPassRate < 0.40 {
		t.Fatalf("metrics = %+v", report.Metrics)
	}
}

func TestRunMikrosArtifactReportsProductGate(t *testing.T) {
	root := t.TempDir()
	suite := filepath.Join(root, "evals", "mikros_artifact")
	if err := os.MkdirAll(suite, 0o755); err != nil {
		t.Fatal(err)
	}
	report, err := Run(context.Background(), suite)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Improved() {
		t.Fatalf("report = %+v", report)
	}
	if len(report.Cases) != 160 {
		t.Fatalf("cases = %d, want 160", len(report.Cases))
	}
	if report.Metrics.FalseVerifiedRate != 0 || report.Metrics.CitationValidity < 0.98 {
		t.Fatalf("metrics = %+v", report.Metrics)
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
