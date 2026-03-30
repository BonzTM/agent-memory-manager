package runtime

import (
	"os"
	"testing"
)

func TestDefaultConfig_SetsReprocessBatchSize(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Summarizer.ReprocessBatchSize != defaultReprocessBatchSize {
		t.Fatalf("expected default batch size %d, got %d", defaultReprocessBatchSize, cfg.Summarizer.ReprocessBatchSize)
	}
}

func TestDefaultConfig_SetsDefaultHTTPAddr(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.HTTP.Addr != ":8080" {
		t.Fatalf("expected default HTTP addr :8080, got %q", cfg.HTTP.Addr)
	}
}

func TestConfigFromEnv_OverridesReprocessBatchSize(t *testing.T) {
	t.Setenv("AMM_REPROCESS_BATCH_SIZE", "30")

	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.Summarizer.ReprocessBatchSize != 30 {
		t.Fatalf("expected env override batch size 30, got %d", cfg.Summarizer.ReprocessBatchSize)
	}
}

func TestConfigFromEnv_OverridesHTTPConfig(t *testing.T) {
	t.Setenv("AMM_HTTP_ADDR", "127.0.0.1:9090")
	t.Setenv("AMM_HTTP_CORS_ORIGINS", "https://example.com,https://app.example.com")

	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.HTTP.Addr != "127.0.0.1:9090" {
		t.Fatalf("expected HTTP addr override, got %q", cfg.HTTP.Addr)
	}
	if cfg.HTTP.CORSOrigins != "https://example.com,https://app.example.com" {
		t.Fatalf("expected HTTP CORS origins override, got %q", cfg.HTTP.CORSOrigins)
	}
}

func TestConfigFromEnv_IgnoresEmptyHTTPAddr(t *testing.T) {
	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.HTTP.Addr != ":8080" {
		t.Fatalf("expected default HTTP addr :8080, got %q", cfg.HTTP.Addr)
	}
}

func TestLoadConfig_ParsesHTTPToml(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.toml"
	content := []byte("[http]\naddr = \"0.0.0.0:9090\"\ncors_origins = \"https://example.com\"\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("expected TOML config to load, got error: %v", err)
	}
	if cfg.HTTP.Addr != "0.0.0.0:9090" {
		t.Fatalf("expected TOML HTTP addr 0.0.0.0:9090, got %q", cfg.HTTP.Addr)
	}
	if cfg.HTTP.CORSOrigins != "https://example.com" {
		t.Fatalf("expected TOML HTTP CORS origins https://example.com, got %q", cfg.HTTP.CORSOrigins)
	}
}

func TestConfigFromEnv_IgnoresInvalidReprocessBatchSize(t *testing.T) {
	t.Setenv("AMM_REPROCESS_BATCH_SIZE", "0")

	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.Summarizer.ReprocessBatchSize != defaultReprocessBatchSize {
		t.Fatalf("expected invalid env batch size to keep default %d, got %d", defaultReprocessBatchSize, cfg.Summarizer.ReprocessBatchSize)
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

func TestDefaultConfig_SetsAdditionalSummarizerBatchDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Summarizer.CompressChunkSize != defaultCompressChunkSize {
		t.Fatalf("expected default compress chunk size %d, got %d", defaultCompressChunkSize, cfg.Summarizer.CompressChunkSize)
	}
	if cfg.Summarizer.CompressMaxEvents != defaultCompressMaxEvents {
		t.Fatalf("expected default compress max events %d, got %d", defaultCompressMaxEvents, cfg.Summarizer.CompressMaxEvents)
	}
	if cfg.Summarizer.CompressBatchSize != defaultCompressBatchSize {
		t.Fatalf("expected default compress batch size %d, got %d", defaultCompressBatchSize, cfg.Summarizer.CompressBatchSize)
	}
	if cfg.Summarizer.TopicBatchSize != defaultTopicBatchSize {
		t.Fatalf("expected default topic batch size %d, got %d", defaultTopicBatchSize, cfg.Summarizer.TopicBatchSize)
	}
	if cfg.Summarizer.EmbeddingBatchSize != defaultEmbeddingBatchSize {
		t.Fatalf("expected default embedding batch size %d, got %d", defaultEmbeddingBatchSize, cfg.Summarizer.EmbeddingBatchSize)
	}
	if cfg.Summarizer.CrossProjectSimilarityThreshold != defaultCrossProjectSimilarity {
		t.Fatalf("expected default cross-project similarity threshold %.2f, got %.2f", defaultCrossProjectSimilarity, cfg.Summarizer.CrossProjectSimilarityThreshold)
	}
}

func TestConfigFromEnv_OverridesAdditionalSummarizerBatchValues(t *testing.T) {
	t.Setenv("AMM_COMPRESS_CHUNK_SIZE", "12")
	t.Setenv("AMM_COMPRESS_MAX_EVENTS", "240")
	t.Setenv("AMM_COMPRESS_BATCH_SIZE", "18")
	t.Setenv("AMM_TOPIC_BATCH_SIZE", "9")
	t.Setenv("AMM_EMBEDDING_BATCH_SIZE", "80")
	t.Setenv("AMM_CROSS_PROJECT_SIMILARITY_THRESHOLD", "0.82")

	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.Summarizer.CompressChunkSize != 12 {
		t.Fatalf("expected compress chunk size 12, got %d", cfg.Summarizer.CompressChunkSize)
	}
	if cfg.Summarizer.CompressMaxEvents != 240 {
		t.Fatalf("expected compress max events 240, got %d", cfg.Summarizer.CompressMaxEvents)
	}
	if cfg.Summarizer.CompressBatchSize != 18 {
		t.Fatalf("expected compress batch size 18, got %d", cfg.Summarizer.CompressBatchSize)
	}
	if cfg.Summarizer.TopicBatchSize != 9 {
		t.Fatalf("expected topic batch size 9, got %d", cfg.Summarizer.TopicBatchSize)
	}
	if cfg.Summarizer.EmbeddingBatchSize != 80 {
		t.Fatalf("expected embedding batch size 80, got %d", cfg.Summarizer.EmbeddingBatchSize)
	}
	if cfg.Summarizer.CrossProjectSimilarityThreshold != 0.82 {
		t.Fatalf("expected cross-project similarity threshold 0.82, got %.2f", cfg.Summarizer.CrossProjectSimilarityThreshold)
	}
}

func TestConfigFromEnv_IgnoresInvalidAdditionalSummarizerBatchValues(t *testing.T) {
	t.Setenv("AMM_COMPRESS_CHUNK_SIZE", "0")
	t.Setenv("AMM_COMPRESS_MAX_EVENTS", "-1")
	t.Setenv("AMM_COMPRESS_BATCH_SIZE", "0")
	t.Setenv("AMM_TOPIC_BATCH_SIZE", "abc")
	t.Setenv("AMM_EMBEDDING_BATCH_SIZE", "0")
	t.Setenv("AMM_CROSS_PROJECT_SIMILARITY_THRESHOLD", "invalid")

	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.Summarizer.CompressChunkSize != defaultCompressChunkSize {
		t.Fatalf("expected invalid compress chunk size to keep default %d, got %d", defaultCompressChunkSize, cfg.Summarizer.CompressChunkSize)
	}
	if cfg.Summarizer.CompressMaxEvents != defaultCompressMaxEvents {
		t.Fatalf("expected invalid compress max events to keep default %d, got %d", defaultCompressMaxEvents, cfg.Summarizer.CompressMaxEvents)
	}
	if cfg.Summarizer.CompressBatchSize != defaultCompressBatchSize {
		t.Fatalf("expected invalid compress batch size to keep default %d, got %d", defaultCompressBatchSize, cfg.Summarizer.CompressBatchSize)
	}
	if cfg.Summarizer.TopicBatchSize != defaultTopicBatchSize {
		t.Fatalf("expected invalid topic batch size to keep default %d, got %d", defaultTopicBatchSize, cfg.Summarizer.TopicBatchSize)
	}
	if cfg.Summarizer.EmbeddingBatchSize != defaultEmbeddingBatchSize {
		t.Fatalf("expected invalid embedding batch size to keep default %d, got %d", defaultEmbeddingBatchSize, cfg.Summarizer.EmbeddingBatchSize)
	}
	if cfg.Summarizer.CrossProjectSimilarityThreshold != defaultCrossProjectSimilarity {
		t.Fatalf("expected invalid cross-project similarity threshold to keep default %.2f, got %.2f", defaultCrossProjectSimilarity, cfg.Summarizer.CrossProjectSimilarityThreshold)
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

func TestDefaultConfig_APIConfigEmpty(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.API.URL != "" {
		t.Fatalf("expected empty API URL by default, got %q", cfg.API.URL)
	}
	if cfg.API.Key != "" {
		t.Fatalf("expected empty API key by default, got %q", cfg.API.Key)
	}
}

func TestDefaultConfig_MaxExpandDepth(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.MaxExpandDepth != defaultMaxExpandDepth {
		t.Fatalf("expected max expand depth default %d, got %d", defaultMaxExpandDepth, cfg.MaxExpandDepth)
	}
}

func TestConfigFromEnv_OverridesMaxExpandDepth(t *testing.T) {
	t.Setenv("AMM_MAX_EXPAND_DEPTH", "-1")

	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.MaxExpandDepth != -1 {
		t.Fatalf("expected max expand depth -1, got %d", cfg.MaxExpandDepth)
	}
}

func TestConfigFromEnv_IgnoresInvalidMaxExpandDepth(t *testing.T) {
	t.Setenv("AMM_MAX_EXPAND_DEPTH", "-2")

	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.MaxExpandDepth != defaultMaxExpandDepth {
		t.Fatalf("expected invalid max expand depth to keep default %d, got %d", defaultMaxExpandDepth, cfg.MaxExpandDepth)
	}
}

func TestConfigFromEnv_OverridesAPIConfig(t *testing.T) {
	t.Setenv("AMM_API_URL", "http://localhost:8080")
	t.Setenv("AMM_API_KEY", "test-key-123")

	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.API.URL != "http://localhost:8080" {
		t.Fatalf("expected API URL http://localhost:8080, got %q", cfg.API.URL)
	}
	if cfg.API.Key != "test-key-123" {
		t.Fatalf("expected API key test-key-123, got %q", cfg.API.Key)
	}
}

func TestConfigFromEnv_IgnoresEmptyAPIConfig(t *testing.T) {
	cfg := ConfigFromEnv(DefaultConfig())
	if cfg.API.URL != "" {
		t.Fatalf("expected empty API URL when env not set, got %q", cfg.API.URL)
	}
	if cfg.API.Key != "" {
		t.Fatalf("expected empty API key when env not set, got %q", cfg.API.Key)
	}
}

func TestLoadConfig_ParsesAPIToml(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.toml"
	content := []byte("[api]\nurl = \"http://remote:8080\"\nkey = \"toml-key\"\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("expected TOML config to load, got error: %v", err)
	}
	if cfg.API.URL != "http://remote:8080" {
		t.Fatalf("expected TOML API URL http://remote:8080, got %q", cfg.API.URL)
	}
	if cfg.API.Key != "toml-key" {
		t.Fatalf("expected TOML API key toml-key, got %q", cfg.API.Key)
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
