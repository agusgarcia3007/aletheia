package selector

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHeuristicSelectorStageOrderAndFallback(t *testing.T) {
	s := HeuristicSelector{}
	decision := s.Select(Snapshot{}, []Candidate{
		{Action: "x", LogProb: 100, Source: "model"},
		{Action: ActRespond, LogProb: 0, Source: "model"},
	})
	if decision.Action != ActRunTests {
		t.Fatalf("action = %s, want %s", decision.Action, ActRunTests)
	}
	if decision.Source != "heuristic" {
		t.Fatalf("source = %s, want heuristic fallback", decision.Source)
	}

	decision = s.Select(Snapshot{
		HasRunTests:        true,
		LastVerifierStatus: "fail",
	}, []Candidate{{Action: ActParseCode, LogProb: -10, Source: "model"}})
	if decision.Action != ActParseCode {
		t.Fatalf("action = %s, want %s", decision.Action, ActParseCode)
	}

	decision = s.Select(Snapshot{
		HasRunTests:        true,
		LastVerifierStatus: "fail",
		Parsed:             true,
		PatternFound:       true,
		HasCandidatePatch:  true,
	}, []Candidate{{Action: ActVerify, LogProb: -1, Source: "model"}})
	if decision.Action != ActVerify {
		t.Fatalf("action = %s, want %s", decision.Action, ActVerify)
	}
}

func TestHeuristicSelectorRespondsAfterVerification(t *testing.T) {
	decision := (HeuristicSelector{}).Select(Snapshot{Verified: true}, []Candidate{
		{Action: ActRunTests, LogProb: 0, Source: "model"},
		{Action: ActRespond, LogProb: -5, Source: "model"},
	})
	if decision.Action != ActRespond {
		t.Fatalf("action = %s, want %s", decision.Action, ActRespond)
	}
}

func TestCandidateGreedySelectorChoosesHighestLogProbFunctionalCandidate(t *testing.T) {
	decision := (CandidateGreedySelector{}).Select(Snapshot{}, []Candidate{
		{Action: "not-functional", LogProb: 100, Source: "bad"},
		{Action: ActParseCode, LogProb: -1, Source: "low"},
		{Action: ActRunTests, LogProb: 2, Source: "high"},
	})
	if decision.Action != ActRunTests {
		t.Fatalf("action = %s, want %s", decision.Action, ActRunTests)
	}
}

func TestLinearSelectorTrainingSaveLoadAndPrediction(t *testing.T) {
	examples := noisySelectorExamples()
	model, report, err := TrainLinear(examples, TrainOptions{Epochs: 250, LearningRate: 0.1})
	if err != nil {
		t.Fatal(err)
	}
	if report.FinalLoss >= report.InitialLoss {
		t.Fatalf("loss did not improve: initial %.6f final %.6f", report.InitialLoss, report.FinalLoss)
	}
	if report.FinalAccuracy < 1 {
		t.Fatalf("final accuracy = %.4f, want 1", report.FinalAccuracy)
	}
	decision := model.Select(Snapshot{
		HasRunTests:        true,
		LastVerifierStatus: "fail",
	}, noisyCandidates(ActParseCode))
	if decision.Action != ActParseCode {
		t.Fatalf("decision = %+v, want %s", decision, ActParseCode)
	}

	dir := t.TempDir()
	if err := model.Save(dir); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadLinear(dir)
	if err != nil {
		t.Fatal(err)
	}
	loadedDecision := loaded.Select(Snapshot{
		HasRunTests:        true,
		LastVerifierStatus: "fail",
		Parsed:             true,
		PatternFound:       true,
	}, noisyCandidates(ActMutateCode))
	if loadedDecision.Action != ActMutateCode {
		t.Fatalf("loaded decision = %+v, want %s", loadedDecision, ActMutateCode)
	}
}

func TestLoadTrainingExamples(t *testing.T) {
	path := filepath.Join(t.TempDir(), "selector.jsonl")
	raw := `{"snapshot":{},"candidates":[{"action":"<ACT_RESPOND>","log_prob":0},{"action":"<ACT_RUN_TESTS>","log_prob":-0.1}],"chosen":"<ACT_RUN_TESTS>","reward":1}
{"snapshot":{"has_run_tests":true,"last_verifier_status":"fail"},"candidates":[{"action":"<ACT_RESPOND>","log_prob":0},{"action":"<ACT_PARSE_CODE>","log_prob":-0.1}],"chosen":"<ACT_PARSE_CODE>","reward":1}
`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	examples, err := LoadTrainingExamples(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(examples) != 2 || examples[1].Chosen != ActParseCode {
		t.Fatalf("examples = %+v", examples)
	}
}

func noisySelectorExamples() []TrainingExample {
	return []TrainingExample{
		{Snapshot: Snapshot{}, Candidates: noisyCandidates(ActRunTests), Chosen: ActRunTests, Reward: 1},
		{Snapshot: Snapshot{HasRunTests: true, LastVerifierStatus: "fail"}, Candidates: noisyCandidates(ActParseCode), Chosen: ActParseCode, Reward: 1},
		{Snapshot: Snapshot{HasRunTests: true, LastVerifierStatus: "fail", Parsed: true, PatternFound: true}, Candidates: noisyCandidates(ActMutateCode), Chosen: ActMutateCode, Reward: 1},
		{Snapshot: Snapshot{HasRunTests: true, LastVerifierStatus: "fail", Parsed: true, PatternFound: true, HasCandidatePatch: true}, Candidates: noisyCandidates(ActVerify), Chosen: ActVerify, Reward: 1},
		{Snapshot: Snapshot{HasRunTests: true, LastVerifierStatus: "pass"}, Candidates: noisyCandidates(ActRespond), Chosen: ActRespond, Reward: 1},
		{Snapshot: Snapshot{Verified: true}, Candidates: noisyCandidates(ActRespond), Chosen: ActRespond, Reward: 1},
	}
}

func noisyCandidates(good string) []Candidate {
	bad := ActRespond
	if good == ActRespond {
		bad = ActAbstain
	}
	return []Candidate{
		{Action: bad, LogProb: 0, Source: "noisy_bad"},
		{Action: good, LogProb: -0.1, Source: "noisy_good"},
	}
}
