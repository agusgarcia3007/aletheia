package apiserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestShippedGoKnowledgeParses is a verifier-first gate: every Go code block in
// the shipped knowledge corpus must be syntactically valid Go. Knowledge that
// Aletheia serves as verified must actually parse.
func TestShippedGoKnowledgeParses(t *testing.T) {
	root := filepath.Join("..", "..", "knowledge", "coding")
	var checked int
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, block := range extractCodeBlocks(string(raw)) {
			if block.Lang != "go" {
				continue
			}
			checked++
			if !goSnippetParses(block.Code) {
				t.Errorf("invalid Go in %s:\n%s", path, block.Code)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked == 0 {
		t.Skip("no Go knowledge blocks found")
	}
	t.Logf("verified %d Go knowledge snippets parse", checked)
}

func TestGoSnippetParsesAcceptsValidRejectsGarbage(t *testing.T) {
	if !goSnippetParses("func Add(a, b int) int { return a + b }") {
		t.Error("valid top-level func should parse")
	}
	if !goSnippetParses("var wg sync.WaitGroup\nfor i := 0; i < 3; i++ {\n}\nwg.Wait()") {
		t.Error("valid statements should parse via func-body wrap")
	}
	if goSnippetParses("func (((( this is not go") {
		t.Error("garbage must not parse")
	}
}
