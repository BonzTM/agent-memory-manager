//go:build !builtin_embeddings

package service

import "github.com/bonztm/agent-memory-manager/internal/core"

// BuiltinEmbeddingAvailable reports whether the binary was compiled with
// built-in embedding support (build tag: builtin_embeddings).
func BuiltinEmbeddingAvailable() bool { return false }

// NewBuiltinEmbeddingProvider returns nil when built without the
// builtin_embeddings tag.
func NewBuiltinEmbeddingProvider() core.EmbeddingProvider { return nil }
