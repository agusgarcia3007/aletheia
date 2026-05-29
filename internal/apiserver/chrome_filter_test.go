package apiserver

import (
	"strings"
	"testing"

	"aletheia/internal/memory"
)

// TestChromeFilteringRemovesPageNoise gates the production fix: navigation/promo
// chrome and interstitial sources must never reach a public answer.
func TestChromeFilteringRemovesPageNoise(t *testing.T) {
	// Exact noise observed from the deployed instance.
	noise := "🐍 También te puede interesar: Cómo obtener un subconjunto de una lista 💡 Ofrecemos servicios profesionales de desarrollo y capacitación en Python a personas y empresas."
	cleaned := cleanPublicResearchText("en python como invierto una lista", noise)
	if strings.Contains(strings.ToLower(cleaned), "te puede interesar") ||
		strings.Contains(strings.ToLower(cleaned), "ofrecemos servicios") ||
		strings.Contains(cleaned, "🐍") || strings.Contains(cleaned, "💡") {
		t.Fatalf("chrome survived cleaning: %q", cleaned)
	}

	if containsChrome("una lista se invierte con reversed()") {
		t.Fatal("clean sentence wrongly flagged as chrome")
	}
	if !containsChrome("También te puede interesar: otras notas") {
		t.Fatal("chrome phrase not detected")
	}

	// Cloudflare interstitial titles must be filtered from citations.
	if publicWebSourceAllowed(memory.WebSource{Title: "Just a moment...", FinalURL: "https://es.stackoverflow.com/x"}) {
		t.Fatal("interstitial 'Just a moment...' source should be rejected")
	}
	if publicWebSourceAllowed(memory.WebSource{Title: "Attention Required! | Cloudflare", FinalURL: "https://x.dev"}) {
		t.Fatal("'Attention Required' source should be rejected")
	}
	if !publicWebSourceAllowed(memory.WebSource{Title: "Reverse a list in Python", FinalURL: "https://docs.python.org/3/"}) {
		t.Fatal("legitimate source should be allowed")
	}
}
