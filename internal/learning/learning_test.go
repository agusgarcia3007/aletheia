package learning

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"aletheia/internal/memory"
	"aletheia/internal/selector"
)

func TestRunExportsSelectorExamplesAndVerifiedTrajectories(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	dbPath := filepath.Join(root, "memory.sqlite")
	store, err := memory.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	episodeID, err := store.CreateEpisode(ctx, "learn")
	if err != nil {
		t.Fatal(err)
	}
	example, err := json.Marshal(selector.TrainingExample{
		Snapshot: selector.Snapshot{MaxToolCalls: 8},
		Candidates: []selector.Candidate{
			{Action: selector.ActRunTests, LogProb: -0.1},
			{Action: selector.ActRespond, LogProb: 0},
		},
		Chosen: selector.ActRunTests,
		Reward: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordSelectorExample(ctx, episodeID, "1", string(example)); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordTrajectory(ctx, episodeID, []memory.TrajectoryRecord{
		{SearchNodeID: 1, Depth: 0, Status: "root"},
		{SearchNodeID: 2, ParentSearchNodeID: 1, Action: selector.ActVerify, Depth: 1, Status: "verified", Verified: true, Completed: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(root, "generated")
	report, err := Run(ctx, Options{DBPath: dbPath, OutDir: out})
	if err != nil {
		t.Fatal(err)
	}
	if report.SelectorExamples != 1 || report.VerifiedTrajectories != 1 {
		t.Fatalf("report = %+v", report)
	}
	raw, err := os.ReadFile(report.SelectorDatasetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "ACT_RUN_TESTS") {
		t.Fatalf("selector dataset:\n%s", raw)
	}
	raw, err = os.ReadFile(report.TrajectoryDatasetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"verified":true`) {
		t.Fatalf("trajectory dataset:\n%s", raw)
	}
}
