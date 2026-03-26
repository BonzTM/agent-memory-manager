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
