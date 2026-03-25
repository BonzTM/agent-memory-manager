package service

import (
	"context"
	"testing"
)

func TestNew_AllowsNilEmbeddingProvider(t *testing.T) {
	svc := New(nil, "", nil, nil)
	if svc.embeddingProvider != nil {
		t.Fatalf("expected nil embedding provider, got %#v", svc.embeddingProvider)
	}
	if svc.summarizer == nil {
		t.Fatal("expected default summarizer to be set")
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
