package research

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestExtractPreservesParagraphStructureAndDropsChrome(t *testing.T) {
	html := `<html><head><title>Doc</title></head><body>
<nav>Inicio Productos Contacto</nav>
<h1>Qué es un LLM</h1>
<p>Un LLM es un modelo de lenguaje grande.</p>
<p>Consta de una red neuronal con muchos parámetros.</p>
<footer>Copyright 2026</footer>
</body></html>`
	doc, err := SimpleHTMLExtractor{}.Extract(context.Background(), FetchedPage{
		Body: []byte(html), StatusCode: http.StatusOK, ContentType: "text/html", FetchedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(doc.Text, "Inicio Productos") || strings.Contains(doc.Text, "Copyright") {
		t.Fatalf("chrome leaked: %q", doc.Text)
	}
	if !strings.Contains(doc.Text, "\n") {
		t.Fatalf("structure flattened (no newlines): %q", doc.Text)
	}
	// The two paragraphs must be separable as distinct sentences.
	sentences := splitSentences(doc.Text)
	var hasDef, hasNet bool
	for _, s := range sentences {
		if strings.Contains(s, "modelo de lenguaje grande") {
			hasDef = true
		}
		if strings.Contains(s, "red neuronal con muchos") {
			hasNet = true
		}
	}
	if !hasDef || !hasNet {
		t.Fatalf("paragraphs not cleanly split: %q -> %#v", doc.Text, sentences)
	}
}
