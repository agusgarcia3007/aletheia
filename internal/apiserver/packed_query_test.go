package apiserver

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestUnwrapPackedUserMessage(t *testing.T) {
	packed := "Use the recent conversation context below to answer the current user message. Answer only the current user message.\n\nRecent conversation context:\nUser: gracias\nAssistant: De nada.\n\nCurrent user message:\nquien sos?"
	if got := unwrapPackedUserMessage(packed); got != "quien sos?" {
		t.Fatalf("unwrap = %q, want %q", got, "quien sos?")
	}
	if got := unwrapPackedUserMessage("quien sos?"); got != "quien sos?" {
		t.Fatalf("plain message changed: %q", got)
	}
	if got := unwrapPackedUserMessage("Current user message:\n"); got != "Current user message:\n" {
		t.Fatalf("empty extraction should keep original, got %q", got)
	}
}

// TestPackedContextStillRoutesToSmalltalk reproduces the deployed playground
// failure: a context-packed "quien sos?" must answer with identity, not be
// misrouted to research.
func TestPackedContextStillRoutesToSmalltalk(t *testing.T) {
	server := batteryServer(t)
	packed := `Use the recent conversation context below to answer the current user message. Answer only the current user message.\n\nRecent conversation context:\nUser: gracias\nAssistant: De nada.\n\nCurrent user message:\nquien sos?`
	content := chatContent(t, server, packed)
	if !strings.Contains(content, "Aletheia") {
		t.Fatalf("packed identity question misrouted, got: %q", content)
	}
	if strings.Contains(content, "job_id=") || strings.Contains(content, "investigaci") {
		t.Fatalf("packed identity question went to research: %q", content)
	}
	_ = filepath.Separator
}
