package apiserver

import (
	"fmt"
	"testing"
	"time"
)

// TestChatLatencyDeterministicExperts proves the sparse-expert path is fast:
// deterministic experts (smalltalk, math, coding, factual abstain, nonsense)
// answer without any neural generation, so per-request latency stays tiny.
func TestChatLatencyDeterministicExperts(t *testing.T) {
	server := batteryServer(t)
	prompts := []string{
		"hola, todo bien?",
		"cuanto es 17 por 23?",
		"cuanto es 15% de 200?",
		"en python como invierto una lista?",
		"quien es el presidente de francia?",
		"traduce al ingles: gracias",
		"blorf zibble",
		"analiza este repositorio",
	}
	const iterations = 50
	start := time.Now()
	n := 0
	for i := 0; i < iterations; i++ {
		for _, p := range prompts {
			body := `{"model":"aletheia-mikros","messages":[{"role":"user","content":"` + p + `"}]}`
			rec := serveJSON(t, server, "/v1/chat/completions", body, "secret")
			if rec.Code != 200 {
				t.Fatalf("status %d for %q", rec.Code, p)
			}
			n++
		}
	}
	elapsed := time.Since(start)
	avg := elapsed / time.Duration(n)
	fmt.Printf("\n[latency] %d requests, avg %.3f ms/req (%.0f req/s)\n", n, float64(avg.Microseconds())/1000, float64(n)/elapsed.Seconds())

	// Generous ceiling: even with SQLite-backed retrieval the deterministic
	// experts must stay well under 25ms/req. A regression past this means the
	// fast path lost its sparseness (e.g. uncached full scans).
	if avg > 25*time.Millisecond {
		t.Errorf("chat latency regressed: avg %v/req", avg)
	}
}
