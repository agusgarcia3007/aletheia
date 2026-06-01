package apiserver

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
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

// goSnippetTypeChecks is stronger than a parse: it runs the Go type checker and
// tolerates only "undefined external symbol" / import errors (expected in a
// teaching fragment), while rejecting real type errors (bad assignments, wrong
// argument counts, type mismatches). In-process, no shell. Returns ok and the
// list of hard (non-tolerated) errors.
func goSnippetTypeChecks(code string) (bool, []string) {
	wraps := []string{
		code,
		"package p\n" + code,
		"package p\nfunc _() {\n" + code + "\n}\n",
	}
	var lastHard []string
	for _, src := range wraps {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, "snippet.go", src, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		var hard []string
		conf := types.Config{
			Importer:                 importer.Default(),
			DisableUnusedImportCheck: true,
			Error: func(e error) {
				if !tolerableTypeError(e.Error()) {
					hard = append(hard, e.Error())
				}
			},
		}

		_, _ = conf.Check("p", fset, []*ast.File{file}, nil)
		if len(hard) == 0 {
			return true, nil
		}
		lastHard = hard
	}
	return false, lastHard
}

// tolerableTypeError reports whether a type-check error is an expected artifact
// of checking a fragment in isolation (missing externals/imports), as opposed to
// a genuine semantic defect in the snippet itself.
func tolerableTypeError(msg string) bool {
	m := strings.ToLower(msg)
	return strings.Contains(m, "undefined:") ||
		strings.Contains(m, "undeclared name") ||
		strings.Contains(m, "could not import") ||
		strings.Contains(m, "undefined (type") ||
		strings.Contains(m, "declared and not used") ||
		strings.Contains(m, "declared but not used") ||
		strings.Contains(m, "imported and not used") ||
		strings.Contains(m, "is not used") ||
		(strings.Contains(m, "is not a type") && strings.Contains(m, "undefined"))
}
