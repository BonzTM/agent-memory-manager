package httpapi

import (
	"bytes"
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityRegression_StatusRequiresAuth(t *testing.T) {
	base, _, _ := testHTTPEnv(t)
	srv := NewServer(base.svc, Config{Addr: ":0", APIKey: "secret-key"})

	req := httptest.NewRequest(nethttp.MethodGet, "/v1/status", nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != nethttp.StatusUnauthorized {
		t.Fatalf("status=%d want=%d body=%s", w.Code, nethttp.StatusUnauthorized, w.Body.String())
	}
}

func TestSecurityRegression_OversizedBodyRejected(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)

	oversized := strings.Repeat("a", 11<<20)
	payload, err := json.Marshal(map[string]any{
		"type":              "fact",
		"scope":             "global",
		"body":              oversized,
		"tight_description": "oversized",
	})
	if err != nil {
		t.Fatalf("marshal oversized payload: %v", err)
	}

	req := httptest.NewRequest(nethttp.MethodPost, "/v1/memories", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != nethttp.StatusBadRequest {
		t.Fatalf("status=%d want=%d", w.Code, nethttp.StatusBadRequest)
	}
	if got := strings.ToLower(w.Body.String()); !strings.Contains(got, "too large") {
		t.Fatalf("expected too-large error message, got %s", w.Body.String())
	}
}

func TestSecurityRegression_CORSPreflight(t *testing.T) {
	base, _, _ := testHTTPEnv(t)
	srv := NewServer(base.svc, Config{Addr: ":0", CORSOrigins: "https://app.example"})

	req := httptest.NewRequest(nethttp.MethodOptions, "/v1/status", nil)
	req.Header.Set("Origin", "https://app.example")
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != nethttp.StatusNoContent {
		t.Fatalf("status=%d want=%d", w.Code, nethttp.StatusNoContent)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Fatalf("allow-origin=%q want=%q", got, "https://app.example")
	}
}
