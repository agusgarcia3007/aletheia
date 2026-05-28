package tokenizer

import "testing"

func TestByteRoundTrip(t *testing.T) {
	tok := New()
	input := "hello\nbytes:\x00\x01\xff"

	ids := tok.Encode(input)
	got, err := tok.Decode(ids)
	if err != nil {
		t.Fatal(err)
	}
	if got != input {
		t.Fatalf("decode mismatch: got %q want %q", got, input)
	}
}

func TestFunctionalTokenIsAtomic(t *testing.T) {
	tok := New()
	id, ok := tok.ID("<ACT_RUN_TESTS>")
	if !ok {
		t.Fatal("missing functional token")
	}

	input := "<ACT_RUN_TESTS> then respond with <ACT_RESPOND>"
	ids := tok.Encode(input)
	if len(ids) == 0 || ids[0] != id {
		t.Fatalf("first token = %v, want %d", ids, id)
	}

	got, err := tok.Decode(ids)
	if err != nil {
		t.Fatal(err)
	}
	if got != input {
		t.Fatalf("decode mismatch: got %q want %q", got, input)
	}
}
