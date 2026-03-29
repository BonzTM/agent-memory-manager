package httpapi

import (
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAPISpec_ReturnsJSON(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)

	req := httptest.NewRequest(nethttp.MethodGet, "/openapi.json", nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != nethttp.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", w.Code, nethttp.StatusOK, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("content-type=%q want application/json", got)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("openapi payload is not valid json: %v", err)
	}
}

func TestOpenAPISpec_ContainsExpectedPaths(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)

	req := httptest.NewRequest(nethttp.MethodGet, "/openapi.json", nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)
	if w.Code != nethttp.StatusOK {
		t.Fatalf("status=%d want=%d", w.Code, nethttp.StatusOK)
	}

	var spec struct {
		Paths map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode openapi: %v", err)
	}

	for _, p := range []string{"/v1/memories", "/v1/recall", "/healthz"} {
		if _, ok := spec.Paths[p]; !ok {
			t.Fatalf("missing documented path %q", p)
		}
	}
}

func TestSwaggerUI_ReturnsVendoredIndex(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)

	req := httptest.NewRequest(nethttp.MethodGet, "/swagger/", nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != nethttp.StatusOK {
		t.Fatalf("status=%d want=%d body=%s", w.Code, nethttp.StatusOK, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, `<div id="swagger-ui"></div>`) {
		t.Fatalf("swagger index missing ui container: %s", body)
	}
}

func TestSwaggerUI_ReturnsVendoredAsset(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)

	req := httptest.NewRequest(nethttp.MethodGet, "/swagger/swagger-ui.css", nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != nethttp.StatusOK {
		t.Fatalf("status=%d want=%d", w.Code, nethttp.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/css") {
		t.Fatalf("content-type=%q want text/css", ct)
	}
}

func TestOpenAPIAndSwagger_BypassAPIKeyAuth(t *testing.T) {
	base, _, _ := testHTTPEnv(t)
	srv := NewServer(base.svc, Config{Addr: ":0", APIKey: "secret"})

	cases := []string{"/openapi.json", "/swagger/", "/swagger/swagger-ui-bundle.js"}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(nethttp.MethodGet, path, nil)
			w := httptest.NewRecorder()
			srv.server.Handler.ServeHTTP(w, req)
			if w.Code == nethttp.StatusUnauthorized {
				t.Fatalf("path %s should bypass auth", path)
			}
		})
	}
}

func TestSwaggerUI_UnknownAssetReturnsNotFound(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)

	req := httptest.NewRequest(nethttp.MethodGet, "/swagger/does-not-exist.js", nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != nethttp.StatusNotFound {
		t.Fatalf("status=%d want=%d", w.Code, nethttp.StatusNotFound)
	}
}
