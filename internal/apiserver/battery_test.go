package apiserver

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type batteryCase struct {
	category string
	prompt   string
	tools    string // optional raw JSON for "tools" field
	// expectation closures return ("", true) on pass, (reason, false) on fail.
	check func(content string, isToolCall bool, raw string) (string, bool)
}

type catStat struct {
	name string
	pass int
	fail int
	// failures captures up to a few failing prompts for the report.
	failures []string
}

func batteryServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	writeTestCheckpoint(t, filepath.Join(root, "aletheia-mikros"), "aletheia-mikros", 1, []string{
		`{"prompt":"<USER>hola<ASSISTANT>","completion":"Hola desde Mikros.<EOS>"}`,
	})
	writeTestCheckpoint(t, filepath.Join(root, "aletheia-hephaestus"), "aletheia-hephaestus", 1, []string{
		`{"prompt":"<USER>codigo rust<ASSISTANT>","completion":"fn add(a: i32, b: i32) -> i32 { a + b }<EOS>"}`,
	})
	routerPath, err := filepath.Abs("../../checkpoints/router-mikros")
	if err != nil {
		t.Fatal(err)
	}
	knowledgePath, err := filepath.Abs("../../knowledge")
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{
		APIKey:           "secret",
		CheckpointsDir:   root,
		RouterCheckpoint: routerPath,
		Store:            newTestStore(t),
		KnowledgePath:    knowledgePath,
	}
	server, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

// global leak detectors applied to every response.
func globalLeaks(content string) []string {
	var leaks []string
	if strings.Contains(content, "<ACT_") {
		leaks = append(leaks, "action_token")
	}
	if strings.Contains(content, "chunk=") || strings.Contains(content, "EmbeddingID") ||
		strings.Contains(content, "web/source_") || strings.Contains(content, "OffsetStart") {
		leaks = append(leaks, "raw_chunk")
	}
	low := strings.ToLower(content)

	if strings.HasPrefix(strings.TrimSpace(low), "http") && len(strings.Fields(content)) < 6 {
		leaks = append(leaks, "links_only")
	}
	return leaks
}

func extractContent(t *testing.T, raw string) (string, bool) {
	var response struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []any  `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return "", false
	}
	if len(response.Choices) == 0 {
		return "", false
	}
	isTool := response.Choices[0].FinishReason == "tool_calls" || len(response.Choices[0].Message.ToolCalls) > 0
	return response.Choices[0].Message.Content, isTool
}

func mustContainAny(needles ...string) func(string, bool, string) (string, bool) {
	return func(content string, _ bool, _ string) (string, bool) {
		for _, n := range needles {
			if strings.Contains(strings.ToLower(content), strings.ToLower(n)) {
				return "", true
			}
		}
		return fmt.Sprintf("expected any of %v", needles), false
	}
}

func isAbstention(content string) bool {
	low := strings.ToLower(content)
	markers := []string{
		"no tengo evidencia", "no tengo informaci", "necesito un poco mas de contexto",
		"no puedo", "research", "investigaci", "no quiero inventar", "abstener",
		"necesito el lenguaje", "necesito mas", "habilitas research", "no voy a buscar",
		"demasiado larga", "frases cortas", "evidencia suficiente", "job_id=",
		"deshabilitada", "necesito un poco más de contexto",
		"no entiendo bien", "reformul", "necesito mas detalle", "necesito más detalle",
		"no quiero improvisar", "no quiero darte un ejemplo",
	}
	for _, m := range markers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

// abstains is a pass-condition: the system should decline rather than invent.
func abstains() func(string, bool, string) (string, bool) {
	return func(content string, _ bool, _ string) (string, bool) {
		if isAbstention(content) {
			return "", true
		}
		return "did not abstain (possible hallucination): " + truncate(content, 120), false
	}
}

func isToolCallCheck() func(string, bool, string) (string, bool) {
	return func(_ string, isTool bool, _ string) (string, bool) {
		if isTool {
			return "", true
		}
		return "expected tool_calls", false
	}
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len([]rune(s)) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

func TestBatteryReport(t *testing.T) {
	server := batteryServer(t)

	cases := batteryCases()

	stats := map[string]*catStat{}
	order := []string{}
	var totalLeaks, totalHallucination, totalLinksOnly, naturalAnswers, totalCases int

	for _, c := range cases {
		body := fmt.Sprintf(`{"model":"aletheia-mikros","messages":[{"role":"user","content":%q}]`, c.prompt)
		if c.tools != "" {
			body += `,"tools":` + c.tools
		}
		body += `}`
		rec := serveJSON(t, server, "/v1/chat/completions", body, "secret")
		totalCases++
		st, ok := stats[c.category]
		if !ok {
			st = &catStat{name: c.category}
			stats[c.category] = st
			order = append(order, c.category)
		}
		if rec.Code != 200 {
			st.fail++
			st.failures = append(st.failures, fmt.Sprintf("[HTTP %d] %s", rec.Code, c.prompt))
			continue
		}
		content, isTool := extractContent(t, rec.Body.String())

		leaks := globalLeaks(content)
		if len(leaks) > 0 {
			totalLeaks++
			for _, l := range leaks {
				if l == "links_only" {
					totalLinksOnly++
				}
			}
		}

		if isTool || (strings.TrimSpace(content) != "" && len(globalLeaks(content)) == 0) {
			naturalAnswers++
		}

		reason, pass := c.check(content, isTool, rec.Body.String())

		if len(leaks) > 0 {
			pass = false
			reason = "LEAK:" + strings.Join(leaks, ",") + " | " + reason
		}
		if pass {
			st.pass++
		} else {
			st.fail++
			if len(st.failures) < 5 {
				st.failures = append(st.failures, fmt.Sprintf("%q -> %s | got: %s", c.prompt, reason, truncate(content, 100)))
			}

			if c.category == "factual_no_research" && !isAbstention(content) {
				totalHallucination++
			}
		}
	}

	sort.Strings(order)
	fmt.Println("\n================ ALETHEIA MIKROS — BATERÍA OUT-OF-DISTRIBUTION ================")
	fmt.Printf("%-28s %6s %6s %8s\n", "categoría", "pass", "fail", "rate")
	fmt.Println(strings.Repeat("-", 56))
	var gp, gf int
	for _, name := range order {
		st := stats[name]
		tot := st.pass + st.fail
		rate := 0.0
		if tot > 0 {
			rate = float64(st.pass) / float64(tot)
		}
		fmt.Printf("%-28s %6d %6d %7.0f%%\n", name, st.pass, st.fail, rate*100)
		gp += st.pass
		gf += st.fail
	}
	fmt.Println(strings.Repeat("-", 56))
	fmt.Printf("%-28s %6d %6d %7.0f%%\n", "TOTAL", gp, gf, float64(gp)/float64(gp+gf)*100)

	fmt.Println("\n---- métricas headline del proyecto ----")
	fmt.Printf("total_cases           : %d\n", totalCases)
	fmt.Printf("raw_chunk/act_leak    : %d  (%.1f%%)  [objetivo: 0]\n", totalLeaks, pct(totalLeaks, totalCases))
	fmt.Printf("links_only            : %d  (%.1f%%)  [objetivo: 0]\n", totalLinksOnly, pct(totalLinksOnly, totalCases))
	fmt.Printf("hallucination (factual): %d  [objetivo: 0]\n", totalHallucination)
	fmt.Printf("natural_answer_rate   : %.1f%%\n", pct(naturalAnswers, totalCases))
	fmt.Printf("capability_pass_rate  : %.1f%%\n", float64(gp)/float64(gp+gf)*100)

	fmt.Println("\n---- muestras de fallos por categoría ----")
	for _, name := range order {
		st := stats[name]
		if len(st.failures) == 0 {
			continue
		}
		fmt.Printf("\n[%s]  (%d fallos)\n", name, st.fail)
		for _, f := range st.failures {
			fmt.Println("  •", f)
		}
	}
	fmt.Println("\n===============================================================================")

	if totalLeaks != 0 {
		t.Errorf("raw_chunk/action leak must be 0, got %d", totalLeaks)
	}
	if totalLinksOnly != 0 {
		t.Errorf("links_only must be 0, got %d", totalLinksOnly)
	}
	if totalHallucination != 0 {
		t.Errorf("factual hallucination must be 0, got %d", totalHallucination)
	}
	if naturalRate := float64(naturalAnswers) / float64(totalCases); naturalRate < 0.99 {
		t.Errorf("natural_answer_rate must be >= 99%%, got %.1f%%", naturalRate*100)
	}
	if capRate := float64(gp) / float64(gp+gf); capRate < 0.85 {
		t.Errorf("capability_pass_rate regressed below 85%%: %.1f%%", capRate*100)
	}
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}
