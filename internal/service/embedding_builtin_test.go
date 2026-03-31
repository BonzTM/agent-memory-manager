package service

import (
	"context"
	"math"
	"testing"
)

func TestBuiltinEmbeddingAvailability(t *testing.T) {
	available := BuiltinEmbeddingAvailable()
	provider := NewBuiltinEmbeddingProvider()

	if available {
		if provider == nil {
			t.Fatal("expected non-nil provider when builtin_embeddings tag is active")
		}
		if provider.Name() == "" {
			t.Error("provider name should not be empty")
		}
		if provider.Model() == "" {
			t.Error("provider model should not be empty")
		}
	} else {
		if provider != nil {
			t.Fatal("expected nil provider without builtin_embeddings tag")
		}
	}
}

func TestBuiltinEmbedding_EmptyInput(t *testing.T) {
	provider := NewBuiltinEmbeddingProvider()
	if provider == nil {
		t.Skip("builtin embeddings not compiled in")
	}

	vecs, err := provider.Embed(context.Background(), []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 0 {
		t.Errorf("expected 0 vectors, got %d", len(vecs))
	}
}

func TestBuiltinEmbedding_ProducesVectors(t *testing.T) {
	provider := NewBuiltinEmbeddingProvider()
	if provider == nil {
		t.Skip("builtin embeddings not compiled in")
	}

	texts := []string{
		"kubernetes deployment rollout",
		"the dog chased the cat",
	}

	vecs, err := provider.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}

	for i, vec := range vecs {
		if len(vec) != builtinEmbeddingDimension {
			t.Errorf("vector %d: expected %d dims, got %d", i, builtinEmbeddingDimension, len(vec))
		}
		// Should be approximately unit-length (L2 normalized)
		var norm float64
		for _, v := range vec {
			norm += float64(v) * float64(v)
		}
		norm = math.Sqrt(norm)
		if norm < 0.99 || norm > 1.01 {
			t.Errorf("vector %d: expected unit-length, got norm %.4f", i, norm)
		}
	}
}

func TestBuiltinEmbedding_SemanticSimilarity(t *testing.T) {
	provider := NewBuiltinEmbeddingProvider()
	if provider == nil {
		t.Skip("builtin embeddings not compiled in")
	}

	// Pairs that should be similar
	similarPairs := [][2]string{
		{"dog cat pet", "animal puppy kitten"},
		{"deploy deployment rollout", "release ship production"},
		{"database query table", "storage data records"},
	}

	// Pairs that should be dissimilar
	dissimilarPairs := [][2]string{
		{"dog cat pet", "infrastructure server network"},
		{"deploy deployment rollout", "poetry painting music"},
	}

	ctx := context.Background()

	for _, pair := range similarPairs {
		vecs, err := provider.Embed(ctx, []string{pair[0], pair[1]})
		if err != nil {
			t.Fatalf("embed error: %v", err)
		}
		sim := cosineSim(vecs[0], vecs[1])
		if sim < 0.3 {
			t.Errorf("expected similar: %q <-> %q = %.4f (want > 0.3)", pair[0], pair[1], sim)
		}
	}

	for _, pair := range dissimilarPairs {
		vecs, err := provider.Embed(ctx, []string{pair[0], pair[1]})
		if err != nil {
			t.Fatalf("embed error: %v", err)
		}
		sim := cosineSim(vecs[0], vecs[1])
		if sim > 0.7 {
			t.Errorf("expected dissimilar: %q <-> %q = %.4f (want < 0.7)", pair[0], pair[1], sim)
		}
	}
}

func TestBuiltinEmbedding_IdenticalTexts(t *testing.T) {
	provider := NewBuiltinEmbeddingProvider()
	if provider == nil {
		t.Skip("builtin embeddings not compiled in")
	}

	ctx := context.Background()
	vecs, err := provider.Embed(ctx, []string{"hello world", "hello world"})
	if err != nil {
		t.Fatalf("embed error: %v", err)
	}

	sim := cosineSim(vecs[0], vecs[1])
	if sim < 0.999 {
		t.Errorf("identical texts should have sim ~1.0, got %.6f", sim)
	}
}

func TestBuiltinEmbedding_UnknownWords(t *testing.T) {
	provider := NewBuiltinEmbeddingProvider()
	if provider == nil {
		t.Skip("builtin embeddings not compiled in")
	}

	// All unknown words should produce a zero vector
	ctx := context.Background()
	vecs, err := provider.Embed(ctx, []string{"xyzzy plugh abcdef"})
	if err != nil {
		t.Fatalf("embed error: %v", err)
	}

	allZero := true
	for _, v := range vecs[0] {
		if v != 0 {
			allZero = false
			break
		}
	}
	if !allZero {
		t.Error("all-unknown-words should produce zero vector")
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"Hello World", []string{"hello", "world"}},
		{"kubernetes-deployment", []string{"kubernetes-deployment"}},
		{"one, two, three!", []string{"one", "two", "three"}},
		{"  spaces  everywhere  ", []string{"spaces", "everywhere"}},
		{"API_KEY=value123", []string{"api_key", "value123"}},
		{"", nil},
	}

	for _, tt := range tests {
		got := tokenize(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("tokenize(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("tokenize(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

// cosineSim computes cosine similarity between two vectors.
func cosineSim(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
