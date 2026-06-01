package apiserver

import (
	"fmt"
	"testing"
)

func TestBatteryDump(t *testing.T) {
	server := batteryServer(t)
	prompts := []string{
		"en python, como invierto una lista?",
		"en go como parseo json a un struct?",
		"como hago un loop en kotlin?",
		"quien es el presidente de francia?",
		"cual es la moneda de japon?",
		"sos un robot o una persona?",
		"asdf asdf asdf",
		"cuanto es 15% de 200?",
		"escribime un poema corto sobre el mar",
		"contame un chiste",
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
