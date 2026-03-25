package service

import (
	"context"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	defaultNoopEmbeddingProviderName  = "noop-local"
	defaultNoopEmbeddingProviderModel = "noop"
)

type NoopEmbeddingProvider struct {
	name  string
	model string
}

var _ core.EmbeddingProvider = (*NoopEmbeddingProvider)(nil)

func NewNoopEmbeddingProvider(name, model string) *NoopEmbeddingProvider {
	if name == "" {
		name = defaultNoopEmbeddingProviderName
	}
	if model == "" {
		model = defaultNoopEmbeddingProviderModel
	}
	return &NoopEmbeddingProvider{name: name, model: model}
}

func (p *NoopEmbeddingProvider) Name() string {
	return p.name
}

func (p *NoopEmbeddingProvider) Model() string {
	return p.model
}

func (p *NoopEmbeddingProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = []float32{}
	}
	return vectors, nil
}
