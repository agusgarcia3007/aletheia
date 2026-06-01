package apiserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func adminRequest(t *testing.T, server *Server, method, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/v1/aletheia/admin/pipeline", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Admin-Token", token)
	}
	server.Handler().ServeHTTP(rec, req)
	return rec
}

func TestAdminPipelineRequiresToken(t *testing.T) {
	store := newTestStore(t)
	server := newTestServer(t, Options{APIKey: "secret", AdminToken: "admintok", Store: store})

	// No token -> hidden (404).
	if rec := adminRequest(t, server, http.MethodPost, "{}", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("no-token POST status = %d, want 404", rec.Code)
	}
	// Wrong token -> hidden.
	if rec := adminRequest(t, server, http.MethodGet, "", "nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("wrong-token GET status = %d, want 404", rec.Code)
	}
}

func TestAdminPipelineDisabledWithoutAdminToken(t *testing.T) {
	store := newTestStore(t)
	server := newTestServer(t, Options{APIKey: "secret", Store: store}) // no AdminToken
	if rec := adminRequest(t, server, http.MethodPost, "{}", "anything"); rec.Code != http.StatusNotFound {
		t.Fatalf("disabled admin status = %d, want 404", rec.Code)
	}
}

func TestAdminPipelineRunsAndReportsStatus(t *testing.T) {
	store := newTestStore(t)
	server := newTestServer(t, Options{APIKey: "secret", AdminToken: "admintok", Store: store})

	// Start with an empty corpus and no seed topics: it should harvest 0 and
	// finish at "done" without attempting (slow) training.
	rec := adminRequest(t, server, http.MethodPost, "{}", "admintok")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start status = %d body=%s", rec.Code, rec.Body.String())
	}

	deadline := time.Now().Add(3 * time.Second)
	var snap map[string]any
	for time.Now().Before(deadline) {
		statusRec := adminRequest(t, server, http.MethodGet, "", "admintok")
		if statusRec.Code != http.StatusOK {
			t.Fatalf("status code = %d", statusRec.Code)
		}
		if err := json.Unmarshal(statusRec.Body.Bytes(), &snap); err != nil {
			t.Fatal(err)
		}
		if running, _ := snap["running"].(bool); !running {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if running, _ := snap["running"].(bool); running {
		t.Fatalf("pipeline still running after deadline: %v", snap)
	}
	if snap["phase"] != "done" {
		t.Fatalf("phase = %v, want done (%v)", snap["phase"], snap)
	}
	if harvested, _ := snap["harvested"].(float64); harvested != 0 {
		t.Fatalf("harvested = %v, want 0 on empty corpus", snap["harvested"])
	}
}

func TestTryStartPipelineIsSingleFlight(t *testing.T) {
	store := newTestStore(t)
	server := newTestServer(t, Options{APIKey: "secret", Store: store})
	if !server.tryStartPipeline() {
		t.Fatal("first start should succeed")
	}
	if server.tryStartPipeline() {
		t.Fatal("second start must be rejected while running")
	}
	// Release and confirm it can start again.
	server.adminState.mu.Lock()
	server.adminState.running = false
	server.adminState.mu.Unlock()
	if !server.tryStartPipeline() {
		t.Fatal("start should succeed after release")
	}
}
