package apiserver

import (
	"go/parser"
	"go/token"
	"regexp"
	"strings"
)

// codeBlock is a fenced code block extracted from markdown knowledge.
type codeBlock struct {
	Lang string
	Code string
}

var fenceRe = regexp.MustCompile("(?s)```([a-zA-Z0-9+#-]*)\\n(.*?)```")

// extractCodeBlocks returns the fenced code blocks in a markdown string.
func extractCodeBlocks(md string) []codeBlock {
	var out []codeBlock
	for _, m := range fenceRe.FindAllStringSubmatch(md, -1) {
		out = append(out, codeBlock{Lang: strings.ToLower(strings.TrimSpace(m[1])), Code: m[2]})
	}
	return out
}

// goSnippetParses reports whether a Go snippet is syntactically valid. It tries
// the snippet as a file, then as a package body, then as a function body, so
// both top-level declarations and bare statements are accepted. This is real,
// in-process verification (no shell) — the verifier-first principle applied to
// the knowledge Aletheia serves.
func goSnippetParses(code string) bool {
	candidates := []string{
		code,
		"package p\n" + code,
		"package p\nfunc _() {\n" + code + "\n}\n",
	}
	for _, src := range candidates {
		fset := token.NewFileSet()
		if _, err := parser.ParseFile(fset, "snippet.go", src, parser.SkipObjectResolution); err == nil {
			return true
		}
	}
	return false
}
