package core

import "context"

type EmbeddingProvider interface {
	Name() string
	Model() string
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}
