package apiserver

import (
	"fmt"
	"testing"
)

func TestBatteryDump(t *testing.T) {
	server := batteryServer(t)
	prompts := []string{
		"en python, como invierto una lista?",   // novel coding task, known lang
		"en go como parseo json a un struct?",   // novel coding task
		"como hago un loop en kotlin?",          // unsupported lang
		"quien es el presidente de francia?",    // factual, no research
		"cual es la moneda de japon?",           // factual -> garbage?
		"sos un robot o una persona?",           // smalltalk -> garbage?
		"asdf asdf asdf",                        // nonsense
		"cuanto es 15% de 200?",                 // math beyond
		"escribime un poema corto sobre el mar", // creative
		"contame un chiste",                     // creative
	}
	for _, p := range prompts {
		body := fmt.Sprintf(`{"model":"aletheia-mikros","messages":[{"role":"user","content":%q}]}`, p)
		rec := serveJSON(t, server, "/v1/chat/completions", body, "secret")
		content, isTool := extractContent(t, rec.Body.String())
		fmt.Printf("\n>>> PROMPT: %s\n", p)
		fmt.Printf("    tool_call=%v\n", isTool)
		fmt.Printf("    RESP: %q\n", content)
	}
}
