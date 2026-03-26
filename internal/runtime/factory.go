package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bonztm/agent-memory-manager/internal/adapters/sqlite"
	"github.com/bonztm/agent-memory-manager/internal/core"
	"github.com/bonztm/agent-memory-manager/internal/service"
)

func buildSummarizer(cfg Config) core.Summarizer {
	if cfg.Summarizer.APIKey != "" && cfg.Summarizer.Endpoint != "" {
		model := cfg.Summarizer.Model
		if model == "" {
			model = "gpt-4o-mini"
		}
		return service.NewLLMSummarizer(cfg.Summarizer.Endpoint, cfg.Summarizer.APIKey, model)
	}
	return nil
}

func buildIntelligenceProvider(cfg Config, summarizer core.Summarizer) core.IntelligenceProvider {
	if llm, ok := summarizer.(*service.LLMSummarizer); ok {
		return service.NewLLMIntelligenceProviderWithReviewConfig(
			llm,
			cfg.Summarizer.ReviewEndpoint,
			cfg.Summarizer.ReviewAPIKey,
			cfg.Summarizer.ReviewModel,
		)
	}
	return service.NewSummarizerIntelligenceAdapter(summarizer)
}

func buildEmbeddingProvider(cfg Config) core.EmbeddingProvider {
	if !cfg.Embeddings.Enabled {
		return nil
	}
	if cfg.Embeddings.Endpoint != "" {
		model := cfg.Embeddings.Model
		if model == "" {
			model = "text-embedding-3-small"
		}
		return service.NewAPIEmbeddingProvider(cfg.Embeddings.Endpoint, cfg.Embeddings.APIKey, model)
	}
	return service.NewNoopEmbeddingProvider(cfg.Embeddings.Provider, cfg.Embeddings.Model)
}

// NewService creates a fully initialized amm service from the given config.
// Returns the Service interface, a cleanup function, and any error.
// The caller must invoke the cleanup function when done (typically via defer).
func NewService(cfg Config) (core.Service, func(), error) {
	dbDir := filepath.Dir(cfg.Storage.DBPath)
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create db directory %s: %w", dbDir, err)
	}

	ctx := context.Background()

	db, err := sqlite.Open(ctx, cfg.Storage.DBPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}

	if err := sqlite.Migrate(ctx, db); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("run migrations: %w", err)
	}

	repo := &sqlite.SQLiteRepository{DB: db}
	summarizer := buildSummarizer(cfg)
	svc := service.New(repo, cfg.Storage.DBPath, summarizer, buildEmbeddingProvider(cfg))
	svc.SetIntelligenceProvider(buildIntelligenceProvider(cfg, summarizer))
	svc.SetReprocessBatchSize(cfg.Summarizer.BatchSize)
	svc.SetReflectBatchSize(cfg.Summarizer.ReflectBatchSize)
	svc.SetReflectLLMBatchSize(cfg.Summarizer.ReflectLLMBatchSize)
	svc.SetLifecycleReviewBatchSize(cfg.Summarizer.LifecycleReviewBatchSize)
	svc.SetCompressChunkSize(cfg.Summarizer.CompressChunkSize)
	svc.SetCompressMaxEvents(cfg.Summarizer.CompressMaxEvents)
	svc.SetCompressBatchSize(cfg.Summarizer.CompressBatchSize)
	svc.SetTopicBatchSize(cfg.Summarizer.TopicBatchSize)
	svc.SetEmbeddingBatchSize(cfg.Summarizer.EmbeddingBatchSize)
	svc.SetCrossProjectSimilarityThreshold(cfg.Summarizer.CrossProjectSimilarityThreshold)

	cleanup := func() {
		db.Close()
	}

	return svc, cleanup, nil
}
