//go:build fts5

package httpapi

import (
	nethttp "net/http"
	"net/http/httptest"
	"testing"
)

func TestContentTypeJSON(t *testing.T) {
	next := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		w.WriteHeader(nethttp.StatusNoContent)
	})
	h := contentTypeJSON(next)

	t.Run("rejects non-json post", func(t *testing.T) {
		req := httptest.NewRequest(nethttp.MethodPost, "/v1/memories", nil)
		req.Header.Set("Content-Type", "text/plain")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != nethttp.StatusUnsupportedMediaType {
			t.Fatalf("status=%d want=%d", w.Code, nethttp.StatusUnsupportedMediaType)
		}
	})

	t.Run("accepts json post", func(t *testing.T) {
		req := httptest.NewRequest(nethttp.MethodPost, "/v1/memories", nil)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != nethttp.StatusNoContent {
			t.Fatalf("status=%d want=%d", w.Code, nethttp.StatusNoContent)
		}
	})

	t.Run("allows get without content type", func(t *testing.T) {
		req := httptest.NewRequest(nethttp.MethodGet, "/healthz", nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != nethttp.StatusNoContent {
			t.Fatalf("status=%d want=%d", w.Code, nethttp.StatusNoContent)
		}
	})
}

func TestCORS(t *testing.T) {
	next := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		w.WriteHeader(nethttp.StatusNoContent)
	})
	h := cors("https://a.example,https://b.example")(next)

	t.Run("sets headers for matching origin", func(t *testing.T) {
		req := httptest.NewRequest(nethttp.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", "https://a.example")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://a.example" {
			t.Fatalf("allow-origin=%q want=%q", got, "https://a.example")
		}
		if w.Code != nethttp.StatusNoContent {
			t.Fatalf("status=%d want=%d", w.Code, nethttp.StatusNoContent)
		}
	})

	t.Run("preflight returns 204", func(t *testing.T) {
		req := httptest.NewRequest(nethttp.MethodOptions, "/v1/memories", nil)
		req.Header.Set("Origin", "https://a.example")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != nethttp.StatusNoContent {
			t.Fatalf("status=%d want=%d", w.Code, nethttp.StatusNoContent)
		}
	})

	t.Run("non matching origin has no allow-origin", func(t *testing.T) {
		req := httptest.NewRequest(nethttp.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", "https://other.example")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Fatalf("allow-origin=%q want empty", got)
		}
	})
}

func TestRequestLogging(t *testing.T) {
	next := nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		w.WriteHeader(nethttp.StatusTeapot)
	})
	h := requestLogging(next)

	req := httptest.NewRequest(nethttp.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != nethttp.StatusTeapot {
		t.Fatalf("status=%d want=%d", w.Code, nethttp.StatusTeapot)
	}
}
