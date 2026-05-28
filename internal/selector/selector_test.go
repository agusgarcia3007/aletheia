package selector

import "testing"

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
