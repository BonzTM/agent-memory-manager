//go:build builtin_embeddings

package service

import (
	"context"
	"math"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	builtinEmbeddingProviderName  = "builtin-onnx"
	builtinEmbeddingProviderModel = "all-MiniLM-L6-v2"
	builtinEmbeddingDimension     = 384
)

// BuiltinEmbeddingAvailable reports whether the binary was compiled with
// built-in embedding support.
func BuiltinEmbeddingAvailable() bool { return true }

// BuiltinEmbeddingProvider runs a local ONNX model for embedding generation
// without requiring an external API endpoint.
//
// TODO: integrate an ONNX runtime Go binding (e.g. github.com/yalue/onnxruntime_go)
// and bundle the all-MiniLM-L6-v2 model weights. Until then, this provider
// returns a deterministic hash-based embedding suitable for development and
// testing of the embedding pipeline, but not for production semantic search.
type BuiltinEmbeddingProvider struct{}

var _ core.EmbeddingProvider = (*BuiltinEmbeddingProvider)(nil)

// NewBuiltinEmbeddingProvider returns an embedding provider that runs locally.
func NewBuiltinEmbeddingProvider() core.EmbeddingProvider {
	return &BuiltinEmbeddingProvider{}
}

func (p *BuiltinEmbeddingProvider) Name() string { return builtinEmbeddingProviderName }
func (p *BuiltinEmbeddingProvider) Model() string { return builtinEmbeddingProviderModel }

// Embed produces deterministic hash-based vectors for each input text.
// These vectors preserve basic text similarity (identical texts get identical
// vectors) but do not capture semantic meaning. Replace with real ONNX
// inference when the runtime binding is integrated.
func (p *BuiltinEmbeddingProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return [][]float32{}, nil
	}
	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		vectors[i] = hashEmbedding(text, builtinEmbeddingDimension)
	}
	return vectors, nil
}

// hashEmbedding produces a deterministic unit-length vector from text using
// FNV-1a hashing. This is a placeholder for real ONNX inference.
func hashEmbedding(text string, dim int) []float32 {
	vec := make([]float32, dim)
	if text == "" {
		return vec
	}
	for d := 0; d < dim; d++ {
		h := uint32(2166136261) ^ uint32(d)*16777619
		for j := 0; j < len(text); j++ {
			h ^= uint32(text[j])
			h *= 16777619
		}
		vec[d] = float32(int32(h)) / float32(1<<31)
	}
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if norm > 0 {
		scale := float32(1.0 / math.Sqrt(norm))
		for d := range vec {
			vec[d] *= scale
		}
	}
	return vec
}
