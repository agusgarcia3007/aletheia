package research

import "testing"

func TestBalanceTrailingParensDropsDanglingOpen(t *testing.T) {
	in := "Un LLM (siglas para Large Language Model) consta de una red neuronal con muchos parametros (normalmente miles"
	out := balanceTrailingParens(in)
	if out != "Un LLM (siglas para Large Language Model) consta de una red neuronal con muchos parametros" {
		t.Fatalf("got %q", out)
	}
}

func TestBalanceTrailingParensLeavesBalanced(t *testing.T) {
	in := "Un LLM (siglas para Large Language Model) es un modelo."
	if out := balanceTrailingParens(in); out != in {
		t.Fatalf("balanced text changed: %q", out)
	}
}
