package selector

import "fmt"

const (
	ActRunTests   = "<ACT_RUN_TESTS>"
	ActParseCode  = "<ACT_PARSE_CODE>"
	ActMutateCode = "<ACT_MUTATE_CODE>"
	ActVerify     = "<ACT_VERIFY>"
	ActRespond    = "<ACT_RESPOND>"
	ActAbstain    = "<ACT_ABSTAIN>"
)

type Candidate struct {
	TokenID int
	Action  string
	LogProb float64
	Source  string
}

type Snapshot struct {
	HasRunTests        bool
	LastVerifierStatus string
	Parsed             bool
	PatternFound       bool
	HasCandidatePatch  bool
	Verified           bool
	Completed          bool
	ToolCalls          int
	MaxToolCalls       int
}

type Decision struct {
	Action     string
	Confidence float64
	Reason     string
	Source     string
}

type HeuristicSelector struct{}

func (HeuristicSelector) Select(snapshot Snapshot, candidates []Candidate) Decision {
	desired, reason := desiredAction(snapshot)
	if desired == "" {
		return Decision{Action: ActAbstain, Confidence: 0.1, Reason: "no safe stage action remains"}
	}

	best, ok := bestFunctionalCandidate(snapshot, candidates)
	if ok && best.Action == desired {
		return Decision{
			Action:     best.Action,
			Confidence: 0.9,
			Reason:     fmt.Sprintf("%s; selected model/mock candidate", reason),
			Source:     best.Source,
		}
	}

	for _, candidate := range candidates {
		if candidate.Action == desired && IsFunctional(candidate.Action) {
			return Decision{
				Action:     candidate.Action,
				Confidence: 0.8,
				Reason:     fmt.Sprintf("%s; selected matching candidate", reason),
				Source:     candidate.Source,
			}
		}
	}

	return Decision{
		Action:     desired,
		Confidence: 0.6,
		Reason:     fmt.Sprintf("%s; fallback safe stage action", reason),
		Source:     "heuristic",
	}
}

func IsFunctional(action string) bool {
	switch action {
	case ActRunTests, ActParseCode, ActMutateCode, ActVerify, ActRespond, ActAbstain:
		return true
	default:
		return false
	}
}

func desiredAction(snapshot Snapshot) (string, string) {
	if snapshot.Completed {
		return "", "state already completed"
	}
	if snapshot.MaxToolCalls > 0 && snapshot.ToolCalls >= snapshot.MaxToolCalls {
		return ActAbstain, "tool budget exhausted"
	}
	if snapshot.Verified {
		return ActRespond, "verified patch is available"
	}
	if snapshot.HasRunTests && snapshot.LastVerifierStatus == "pass" {
		return ActRespond, "verifier already passes"
	}
	if !snapshot.HasRunTests {
		return ActRunTests, "need initial verifier evidence"
	}
	if snapshot.LastVerifierStatus == "fail" && !snapshot.Parsed {
		return ActParseCode, "failing verifier needs code inspection"
	}
	if snapshot.Parsed && snapshot.PatternFound && !snapshot.HasCandidatePatch {
		return ActMutateCode, "known bug pattern can produce a patch candidate"
	}
	if snapshot.HasCandidatePatch && !snapshot.Verified {
		return ActVerify, "candidate patch must be verified before mutation is kept"
	}
	return ActAbstain, "no verified patch candidate is available"
}

func bestFunctionalCandidate(snapshot Snapshot, candidates []Candidate) (Candidate, bool) {
	var best Candidate
	bestScore := -1e100
	ok := false
	for _, candidate := range candidates {
		if !IsFunctional(candidate.Action) {
			continue
		}
		score := candidate.LogProb + stagePrior(snapshot, candidate.Action)
		if !ok || score > bestScore {
			best, bestScore, ok = candidate, score, true
		}
	}
	return best, ok
}

func stagePrior(snapshot Snapshot, action string) float64 {
	desired, _ := desiredAction(snapshot)
	if action == desired {
		return 10
	}
	switch action {
	case ActAbstain:
		return -2
	case ActRespond:
		if snapshot.Verified || snapshot.LastVerifierStatus == "pass" {
			return 4
		}
		return -4
	case ActVerify:
		if snapshot.HasCandidatePatch {
			return 4
		}
		return -5
	case ActMutateCode:
		if snapshot.Parsed && snapshot.PatternFound {
			return 3
		}
		return -5
	case ActParseCode:
		if snapshot.HasRunTests && snapshot.LastVerifierStatus == "fail" {
			return 3
		}
		return -3
	case ActRunTests:
		if !snapshot.HasRunTests {
			return 3
		}
		return -3
	default:
		return -10
	}
}
