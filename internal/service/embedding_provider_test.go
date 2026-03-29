package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestNew_AllowsNilEmbeddingProvider(t *testing.T) {
	svc := New(nil, "", nil, nil)
	if svc.embeddingProvider != nil {
		t.Fatalf("expected nil embedding provider, got %#v", svc.embeddingProvider)
	}
	if svc.intelligence == nil {
		t.Fatal("expected default intelligence provider to be set")
	}
}

func TestNoopEmbeddingProvider_EmbedShape(t *testing.T) {
	provider := NewNoopEmbeddingProvider("", "")
	if provider.Name() != defaultNoopEmbeddingProviderName {
		t.Fatalf("expected default provider name %q, got %q", defaultNoopEmbeddingProviderName, provider.Name())
	}
	if provider.Model() != defaultNoopEmbeddingProviderModel {
		t.Fatalf("expected default model %q, got %q", defaultNoopEmbeddingProviderModel, provider.Model())
	}

	vectors, err := provider.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vectors) != 3 {
		t.Fatalf("expected 3 vectors, got %d", len(vectors))
	}
	for i := range vectors {
		if len(vectors[i]) != 0 {
			t.Fatalf("expected zero-length noop vector at index %d, got %#v", i, vectors[i])
		}
	}
}

func TestAPIEmbeddingProvider_HappyPath(t *testing.T) {
	t.Parallel()

	var gotAuth string
	var gotModel string
	var gotInput []string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("expected /v1/embeddings path, got %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")

		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		gotModel = req.Model
		gotInput = req.Input

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.3,0.4],"index":1},{"embedding":[0.1,0.2],"index":0}]}`))
	}))
	defer ts.Close()

	provider := NewAPIEmbeddingProvider(ts.URL+"/v1", "test-key", "test-model")
	vectors, err := provider.Embed(context.Background(), []string{"text1", "text2"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	if gotAuth != "Bearer test-key" {
		t.Fatalf("expected Authorization header %q, got %q", "Bearer test-key", gotAuth)
	}
	if gotModel != "test-model" {
		t.Fatalf("expected model %q, got %q", "test-model", gotModel)
	}
	if !reflect.DeepEqual(gotInput, []string{"text1", "text2"}) {
		t.Fatalf("expected input %#v, got %#v", []string{"text1", "text2"}, gotInput)
	}

	expected := [][]float32{{0.1, 0.2}, {0.3, 0.4}}
	if !reflect.DeepEqual(vectors, expected) {
		t.Fatalf("expected vectors %#v, got %#v", expected, vectors)
	}
}

func TestAPIEmbeddingProvider_HTTPError(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	provider := NewAPIEmbeddingProvider(ts.URL, "test-key", "test-model")
	_, err := provider.Embed(context.Background(), []string{"text1"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestAPIEmbeddingProvider_EmptyInput(t *testing.T) {
	t.Parallel()

	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	provider := NewAPIEmbeddingProvider(ts.URL, "test-key", "test-model")
	vectors, err := provider.Embed(context.Background(), []string{})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vectors) != 0 {
		t.Fatalf("expected empty vectors, got %#v", vectors)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("expected no HTTP calls, got %d", calls)
	}
}

func TestAPIEmbeddingProvider_NameAndModel(t *testing.T) {
	t.Parallel()

	provider := NewAPIEmbeddingProvider("http://example.com", "test-key", "test-model")
	if provider.Name() != "api" {
		t.Fatalf("expected name %q, got %q", "api", provider.Name())
	}
	if provider.Model() != "test-model" {
		t.Fatalf("expected model %q, got %q", "test-model", provider.Model())
	}
}
