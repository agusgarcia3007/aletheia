package apiserver

import "testing"

func TestSubstantiveQueryClauseRoutesOnTheQuestion(t *testing.T) {
	// Composed greeting + question: route on the question clause.
	if got := substantiveQueryClause("Hola! que es un LLM?"); got != "que es un LLM" {
		t.Fatalf("composed: got %q, want %q", got, "que es un LLM")
	}
	// Bare greeting (single clause) is untouched.
	if got := substantiveQueryClause("hola"); got != "hola" {
		t.Fatalf("bare greeting changed: %q", got)
	}
	// A plain single-clause question is untouched.
	if got := substantiveQueryClause("que es la entropia"); got != "que es la entropia" {
		t.Fatalf("single clause changed: %q", got)
	}
	// Greeting + smalltalk (no real question) stays as the original message.
	if got := substantiveQueryClause("Hola! como estas?"); got != "Hola! como estas?" {
		t.Fatalf("greeting+smalltalk changed: %q", got)
	}
}
