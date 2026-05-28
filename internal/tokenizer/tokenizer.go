package tokenizer

import (
	"fmt"
	"strings"
)

const ByteVocabSize = 256

var SpecialTokens = []string{
	"<BOS>",
	"<EOS>",
	"<PAD>",
	"<UNK>",
	"<USER>",
	"<SYSTEM>",
	"<ASSISTANT>",
	"<STATE>",
	"<EVIDENCE>",
	"<ACTION>",
	"<RESULT>",
	"<ACT_DECOMPOSE>",
	"<ACT_RETRIEVE>",
	"<ACT_PLAN_DAG>",
	"<ACT_GEN_CANDIDATES>",
	"<ACT_RUN_TESTS>",
	"<ACT_RUN_CMD>",
	"<ACT_PARSE_CODE>",
	"<ACT_MUTATE_CODE>",
	"<ACT_SEARCH_MEMORY>",
	"<ACT_VERIFY>",
	"<ACT_FIND_COUNTEREXAMPLE>",
	"<ACT_REPAIR>",
	"<ACT_RANK>",
	"<ACT_RESPOND>",
	"<ACT_ABSTAIN>",
	"<ACT_COMPRESS_SKILL>",
}

type Tokenizer struct {
	tokenToID map[string]int
	idToToken map[int]string
	specials  []string
}

func New() *Tokenizer {
	tokenToID := make(map[string]int, len(SpecialTokens))
	idToToken := make(map[int]string, len(SpecialTokens))
	specials := make([]string, len(SpecialTokens))
	copy(specials, SpecialTokens)

	for i, token := range specials {
		id := ByteVocabSize + i
		tokenToID[token] = id
		idToToken[id] = token
	}

	// Longest match keeps future overlapping functional tokens deterministic.
	for i := range specials {
		for j := i + 1; j < len(specials); j++ {
			if len(specials[j]) > len(specials[i]) {
				specials[i], specials[j] = specials[j], specials[i]
			}
		}
	}

	return &Tokenizer{
		tokenToID: tokenToID,
		idToToken: idToToken,
		specials:  specials,
	}
}

func (t *Tokenizer) ID(token string) (int, bool) {
	id, ok := t.tokenToID[token]
	return id, ok
}

func (t *Tokenizer) Token(id int) (string, bool) {
	token, ok := t.idToToken[id]
	return token, ok
}

func (t *Tokenizer) Encode(text string) []int {
	ids := make([]int, 0, len(text))
	for i := 0; i < len(text); {
		if id, n, ok := t.matchSpecial(text[i:]); ok {
			ids = append(ids, id)
			i += n
			continue
		}
		ids = append(ids, int(text[i]))
		i++
	}
	return ids
}

func (t *Tokenizer) Decode(ids []int) (string, error) {
	var b strings.Builder
	for _, id := range ids {
		switch {
		case id >= 0 && id < ByteVocabSize:
			b.WriteByte(byte(id))
		case id >= ByteVocabSize:
			token, ok := t.idToToken[id]
			if !ok {
				return "", fmt.Errorf("unknown token id %d", id)
			}
			b.WriteString(token)
		default:
			return "", fmt.Errorf("invalid token id %d", id)
		}
	}
	return b.String(), nil
}

func (t *Tokenizer) VocabSize() int {
	return ByteVocabSize + len(SpecialTokens)
}

func (t *Tokenizer) matchSpecial(text string) (int, int, bool) {
	for _, token := range t.specials {
		if strings.HasPrefix(text, token) {
			return t.tokenToID[token], len(token), true
		}
	}
	return 0, 0, false
}
