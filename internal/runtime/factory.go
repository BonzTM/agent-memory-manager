package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/bonztm/agent-memory-manager/internal/adapters/postgres"
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
	enabled := cfg.Embeddings.Enabled
	// When compiled with builtin_embeddings, embeddings default to on.
	// Operators can still disable with AMM_EMBEDDINGS_ENABLED=false.
	if !enabled && service.BuiltinEmbeddingAvailable() && !cfg.Embeddings.ExplicitlyDisabled {
		enabled = true
	}
	if !enabled {
		return nil
	}
	if cfg.Embeddings.Endpoint != "" {
		model := cfg.Embeddings.Model
		if model == "" {
			model = "text-embedding-3-small"
		}
		return service.NewAPIEmbeddingProvider(cfg.Embeddings.Endpoint, cfg.Embeddings.APIKey, model)
	}
	if service.BuiltinEmbeddingAvailable() {
		return service.NewBuiltinEmbeddingProvider()
	}
	return service.NewNoopEmbeddingProvider(cfg.Embeddings.Provider, cfg.Embeddings.Model)
}

// NewService creates a fully initialized amm service from the given config.
// Returns the Service interface, a cleanup function, and any error.
// The caller must invoke the cleanup function when done (typically via defer).
func NewService(cfg Config) (core.Service, func(), error) {
	ctx := context.Background()
	backend := strings.ToLower(strings.TrimSpace(cfg.Storage.Backend))
	if backend == "" {
		backend = "sqlite"
	}

	var repo core.Repository
	var storagePath string
	var cleanup func()

	switch backend {
	case "sqlite":
		dbDir := filepath.Dir(cfg.Storage.DBPath)
		if err := os.MkdirAll(dbDir, 0o755); err != nil {
			return nil, nil, fmt.Errorf("create db directory %s: %w", dbDir, err)
		}
		db, err := sqlite.Open(ctx, cfg.Storage.DBPath)
		if err != nil {
			return nil, nil, fmt.Errorf("open sqlite database: %w", err)
		}
		if err := sqlite.Migrate(ctx, db); err != nil {
			db.Close()
			return nil, nil, fmt.Errorf("run sqlite migrations: %w", err)
		}
		repo = &sqlite.SQLiteRepository{DB: db}
		storagePath = cfg.Storage.DBPath
		cleanup = func() { _ = db.Close() }
	case "postgres":
		if strings.TrimSpace(cfg.Storage.PostgresDSN) == "" {
			return nil, nil, fmt.Errorf("postgres backend requires AMM_POSTGRES_DSN")
		}
		pgRepo := postgres.NewRepository()
		if err := pgRepo.Open(ctx, cfg.Storage.PostgresDSN); err != nil {
			return nil, nil, fmt.Errorf("open postgres database: %w", err)
		}
		if err := pgRepo.Migrate(ctx); err != nil {
			_ = pgRepo.Close()
			return nil, nil, fmt.Errorf("run postgres migrations: %w", err)
		}
		repo = pgRepo
		storagePath = maskDSN(cfg.Storage.PostgresDSN)
		cleanup = func() { _ = pgRepo.Close() }
		slog.Warn("postgres backend is experimental; some repository methods may return incomplete results")
	default:
		return nil, nil, fmt.Errorf("unsupported storage backend %q", backend)
	}

	summarizer := buildSummarizer(cfg)

	// When no LLM summarizer is configured and the operator hasn't explicitly
	// set a confidence gate, lower the minimum to 0.40 so heuristic-extracted
	// memories (confidence 0.45) can still be created.
	if summarizer == nil && cfg.IntakeQuality.MinConfidenceForCreation == defaultMinConfidenceForCreation {
		cfg.IntakeQuality.MinConfidenceForCreation = 0.40
	}

	svc := service.New(repo, storagePath, summarizer, buildEmbeddingProvider(cfg))
	svc.SetIntelligenceProvider(buildIntelligenceProvider(cfg, summarizer))
	svc.SetReprocessBatchSize(cfg.Summarizer.ReprocessBatchSize)
	svc.SetReflectBatchSize(cfg.Summarizer.ReflectBatchSize)
	svc.SetReflectLLMBatchSize(cfg.Summarizer.ReflectLLMBatchSize)
	svc.SetLifecycleReviewBatchSize(cfg.Summarizer.LifecycleReviewBatchSize)
	svc.SetCompressChunkSize(cfg.Summarizer.CompressChunkSize)
	svc.SetCompressMaxEvents(cfg.Summarizer.CompressMaxEvents)
	svc.SetCompressBatchSize(cfg.Summarizer.CompressBatchSize)
	svc.SetTopicBatchSize(cfg.Summarizer.TopicBatchSize)
	svc.SetEmbeddingBatchSize(cfg.Summarizer.EmbeddingBatchSize)
	svc.SetCrossProjectSimilarityThreshold(cfg.Summarizer.CrossProjectSimilarityThreshold)
	svc.SetEscalationDeterministicMaxChars(cfg.Compression.EscalationDeterministicMaxChars)
	svc.SetMinConfidenceForCreation(cfg.IntakeQuality.MinConfidenceForCreation)
	svc.SetMinImportanceForCreation(cfg.IntakeQuality.MinImportanceForCreation)
	svc.SetEntityHubThreshold(cfg.Retrieval.EntityHubThreshold)
	svc.SetMaxExpandDepth(cfg.MaxExpandDepth)

	return svc, cleanup, nil
}

func maskDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	if idx := strings.Index(dsn, "@"); idx >= 0 {
		return "postgres://***@" + dsn[idx+1:]
	}
	return "postgres://***"
}
