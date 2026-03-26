package runtime

import "testing"

func TestDefaultConfig_SetsSummarizerBatchSize(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Summarizer.BatchSize != defaultSummarizerBatchSize {
		t.Fatalf("expected default batch size %d, got %d", defaultSummarizerBatchSize, cfg.Summarizer.BatchSize)
	}
}

func TestConfigFromEnv_OverridesSummarizerBatchSize(t *testing.T) {
	t.Setenv("AMM_SUMMARIZER_BATCH_SIZE", "30")

	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.Summarizer.BatchSize != 30 {
		t.Fatalf("expected env override batch size 30, got %d", cfg.Summarizer.BatchSize)
	}
}

func TestConfigFromEnv_IgnoresInvalidSummarizerBatchSize(t *testing.T) {
	t.Setenv("AMM_SUMMARIZER_BATCH_SIZE", "0")

	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.Summarizer.BatchSize != defaultSummarizerBatchSize {
		t.Fatalf("expected invalid env batch size to keep default %d, got %d", defaultSummarizerBatchSize, cfg.Summarizer.BatchSize)
	}
}

func TestDefaultConfig_SetsReflectBatchSizes(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Summarizer.ReflectBatchSize != defaultReflectBatchSize {
		t.Fatalf("expected default reflect batch size %d, got %d", defaultReflectBatchSize, cfg.Summarizer.ReflectBatchSize)
	}
	if cfg.Summarizer.ReflectLLMBatchSize != defaultReflectLLMBatchSize {
		t.Fatalf("expected default reflect LLM batch size %d, got %d", defaultReflectLLMBatchSize, cfg.Summarizer.ReflectLLMBatchSize)
	}
	if cfg.Summarizer.LifecycleReviewBatchSize != defaultLifecycleReviewBatchSize {
		t.Fatalf("expected default lifecycle review batch size %d, got %d", defaultLifecycleReviewBatchSize, cfg.Summarizer.LifecycleReviewBatchSize)
	}
}

func TestConfigFromEnv_OverridesReflectBatchSizes(t *testing.T) {
	t.Setenv("AMM_REFLECT_BATCH_SIZE", "120")
	t.Setenv("AMM_REFLECT_LLM_BATCH_SIZE", "7")

	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.Summarizer.ReflectBatchSize != 120 {
		t.Fatalf("expected reflect batch size 120, got %d", cfg.Summarizer.ReflectBatchSize)
	}
	if cfg.Summarizer.ReflectLLMBatchSize != 7 {
		t.Fatalf("expected reflect LLM batch size 7, got %d", cfg.Summarizer.ReflectLLMBatchSize)
	}
}

func TestConfigFromEnv_IgnoresInvalidReflectBatchSizes(t *testing.T) {
	t.Setenv("AMM_REFLECT_BATCH_SIZE", "0")
	t.Setenv("AMM_REFLECT_LLM_BATCH_SIZE", "-3")

	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.Summarizer.ReflectBatchSize != defaultReflectBatchSize {
		t.Fatalf("expected invalid reflect batch size to keep default %d, got %d", defaultReflectBatchSize, cfg.Summarizer.ReflectBatchSize)
	}
	if cfg.Summarizer.ReflectLLMBatchSize != defaultReflectLLMBatchSize {
		t.Fatalf("expected invalid reflect LLM batch size to keep default %d, got %d", defaultReflectLLMBatchSize, cfg.Summarizer.ReflectLLMBatchSize)
	}
}

func TestConfigFromEnv_OverridesLifecycleReviewBatchSize(t *testing.T) {
	t.Setenv("AMM_LIFECYCLE_REVIEW_BATCH_SIZE", "75")

	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.Summarizer.LifecycleReviewBatchSize != 75 {
		t.Fatalf("expected lifecycle review batch size 75, got %d", cfg.Summarizer.LifecycleReviewBatchSize)
	}
}

func TestConfigFromEnv_IgnoresInvalidLifecycleReviewBatchSize(t *testing.T) {
	t.Setenv("AMM_LIFECYCLE_REVIEW_BATCH_SIZE", "0")

	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.Summarizer.LifecycleReviewBatchSize != defaultLifecycleReviewBatchSize {
		t.Fatalf("expected invalid lifecycle review batch size to keep default %d, got %d", defaultLifecycleReviewBatchSize, cfg.Summarizer.LifecycleReviewBatchSize)
	}
}

func TestDefaultConfig_EmbeddingsDisabled(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Embeddings.Enabled {
		t.Fatal("expected embeddings to be disabled by default")
	}
}

func TestConfigFromEnv_OverridesEmbeddingConfig(t *testing.T) {
	t.Setenv("AMM_EMBEDDINGS_ENABLED", "true")
	t.Setenv("AMM_EMBEDDINGS_PROVIDER", "local-noop")
	t.Setenv("AMM_EMBEDDINGS_ENDPOINT", "http://localhost:11434")
	t.Setenv("AMM_EMBEDDINGS_API_KEY", "embed-key")
	t.Setenv("AMM_EMBEDDINGS_MODEL", "embed-test")

	cfg := ConfigFromEnv(DefaultConfig())
	if !cfg.Embeddings.Enabled {
		t.Fatal("expected embeddings enabled from env")
	}
	if cfg.Embeddings.Provider != "local-noop" {
		t.Fatalf("expected provider local-noop, got %q", cfg.Embeddings.Provider)
	}
	if cfg.Embeddings.Endpoint != "http://localhost:11434" {
		t.Fatalf("expected endpoint http://localhost:11434, got %q", cfg.Embeddings.Endpoint)
	}
	if cfg.Embeddings.APIKey != "embed-key" {
		t.Fatalf("expected API key embed-key, got %q", cfg.Embeddings.APIKey)
	}
	if cfg.Embeddings.Model != "embed-test" {
		t.Fatalf("expected model embed-test, got %q", cfg.Embeddings.Model)
	}
}

func TestConfigFromEnv_OverridesReviewRoutingConfig(t *testing.T) {
	t.Setenv("AMM_REVIEW_ENDPOINT", "http://localhost:4000/v1")
	t.Setenv("AMM_REVIEW_API_KEY", "review-key")
	t.Setenv("AMM_REVIEW_MODEL", "review-model")

	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.Summarizer.ReviewEndpoint != "http://localhost:4000/v1" {
		t.Fatalf("expected review endpoint override, got %q", cfg.Summarizer.ReviewEndpoint)
	}
	if cfg.Summarizer.ReviewAPIKey != "review-key" {
		t.Fatalf("expected review api key override, got %q", cfg.Summarizer.ReviewAPIKey)
	}
	if cfg.Summarizer.ReviewModel != "review-model" {
		t.Fatalf("expected review model override, got %q", cfg.Summarizer.ReviewModel)
	}
}
