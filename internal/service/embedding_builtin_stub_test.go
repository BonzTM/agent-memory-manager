//go:build !builtin_embeddings

package service

import "testing"

func TestBuiltinEmbeddingAvailability(t *testing.T) {
	if BuiltinEmbeddingAvailable() {
		t.Fatal("expected builtin embeddings to be unavailable without builtin_embeddings tag")
	}
	if provider := NewBuiltinEmbeddingProvider(); provider != nil {
		t.Fatal("expected nil provider without builtin_embeddings tag")
	}
}
