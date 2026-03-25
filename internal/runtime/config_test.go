package runtime

import "testing"

func TestDefaultConfig_SetsLLMBatchSize(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.LLM.BatchSize != defaultLLMBatchSize {
		t.Fatalf("expected default batch size %d, got %d", defaultLLMBatchSize, cfg.LLM.BatchSize)
	}
}

func TestConfigFromEnv_OverridesLLMBatchSize(t *testing.T) {
	t.Setenv("AMM_LLM_BATCH_SIZE", "30")

	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.LLM.BatchSize != 30 {
		t.Fatalf("expected env override batch size 30, got %d", cfg.LLM.BatchSize)
	}
}

func TestConfigFromEnv_IgnoresInvalidLLMBatchSize(t *testing.T) {
	t.Setenv("AMM_LLM_BATCH_SIZE", "0")

	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.LLM.BatchSize != defaultLLMBatchSize {
		t.Fatalf("expected invalid env batch size to keep default %d, got %d", defaultLLMBatchSize, cfg.LLM.BatchSize)
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
	t.Setenv("AMM_EMBEDDINGS_MODEL", "embed-test")

	cfg := ConfigFromEnv(DefaultConfig())
	if !cfg.Embeddings.Enabled {
		t.Fatal("expected embeddings enabled from env")
	}
	if cfg.Embeddings.Provider != "local-noop" {
		t.Fatalf("expected provider local-noop, got %q", cfg.Embeddings.Provider)
	}
	if cfg.Embeddings.Model != "embed-test" {
		t.Fatalf("expected model embed-test, got %q", cfg.Embeddings.Model)
	}
}
