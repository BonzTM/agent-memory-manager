package service

import (
	"testing"
)

func TestBuiltinEmbeddingStub(t *testing.T) {
	// Without the builtin_embeddings tag, the stub should be active.
	// This test verifies the stub compiles and the provider returns nil.
	if BuiltinEmbeddingAvailable() {
		// If built with the tag, validate the provider works.
		provider := NewBuiltinEmbeddingProvider()
		if provider == nil {
			t.Fatal("expected non-nil provider when builtin_embeddings tag is active")
		}
		if provider.Name() == "" {
			t.Fatal("expected non-empty provider name")
		}
		if provider.Model() == "" {
			t.Fatal("expected non-empty provider model")
		}

		vectors, err := provider.Embed(nil, []string{"hello world", "test input"})
		if err != nil {
			t.Fatalf("Embed returned error: %v", err)
		}
		if len(vectors) != 2 {
			t.Fatalf("expected 2 vectors, got %d", len(vectors))
		}
		for i, vec := range vectors {
			if len(vec) == 0 {
				t.Fatalf("vector %d is empty", i)
			}
		}

		// Identical texts should produce identical vectors.
		vecs2, _ := provider.Embed(nil, []string{"same text", "same text"})
		if len(vecs2) == 2 && len(vecs2[0]) > 0 && len(vecs2[1]) > 0 {
			for j := range vecs2[0] {
				if vecs2[0][j] != vecs2[1][j] {
					t.Fatalf("identical texts produced different vectors at dim %d", j)
				}
			}
		}
	} else {
		provider := NewBuiltinEmbeddingProvider()
		if provider != nil {
			t.Fatal("expected nil provider without builtin_embeddings tag")
		}
	}
}
