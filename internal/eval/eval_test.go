package eval

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunBootstrapReportsBeamImprovement(t *testing.T) {
	suite := filepath.Join(t.TempDir(), "bootstrap")
	if err := os.MkdirAll(suite, 0o755); err != nil {
		t.Fatal(err)
	}
	report, err := RunBootstrap(context.Background(), suite)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Improved() {
		t.Fatalf("report = %+v, want beam improvement", report)
	}
	if len(report.Cases) != 1 || report.Cases[0].CandidateGreedyStatus != "failed" || report.Cases[0].BeamStatus != "pass" {
		t.Fatalf("cases = %+v", report.Cases)
	}
}
