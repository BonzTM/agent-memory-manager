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
