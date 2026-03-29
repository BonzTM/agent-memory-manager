package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func writeRuntimeConfigFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}
	return path
}

func TestDefaultConfig_AllDefaults(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}

	cfg := DefaultConfig()

	if cfg.Storage.DBPath != filepath.Join(home, ".amm", "amm.db") {
		t.Fatalf("expected default db path under home dir, got %q", cfg.Storage.DBPath)
	}
	if cfg.Storage.Backend != "sqlite" {
		t.Fatalf("expected sqlite backend by default, got %q", cfg.Storage.Backend)
	}
	if cfg.Storage.PostgresDSN != "" {
		t.Fatalf("expected empty postgres DSN by default, got %q", cfg.Storage.PostgresDSN)
	}

	if cfg.Retrieval.DefaultLimit != 10 {
		t.Fatalf("expected default retrieval limit 10, got %d", cfg.Retrieval.DefaultLimit)
	}
	if cfg.Retrieval.AmbientLimit != 5 {
		t.Fatalf("expected default ambient limit 5, got %d", cfg.Retrieval.AmbientLimit)
	}
	if cfg.Retrieval.EnableSemantic {
		t.Fatal("expected semantic retrieval disabled by default")
	}
	if !cfg.Retrieval.EnableExplain {
		t.Fatal("expected explain retrieval enabled by default")
	}

	if cfg.Privacy.DefaultPrivacy != "private" {
		t.Fatalf("expected default privacy private, got %q", cfg.Privacy.DefaultPrivacy)
	}

	if !cfg.Maintenance.AutoReflect || !cfg.Maintenance.AutoCompress || !cfg.Maintenance.AutoConsolidate || !cfg.Maintenance.AutoDetectContradictions {
		t.Fatalf("expected all maintenance flags enabled by default, got %+v", cfg.Maintenance)
	}

	if cfg.Summarizer.ReprocessBatchSize != defaultReprocessBatchSize {
		t.Fatalf("expected default summarizer batch size %d, got %d", defaultReprocessBatchSize, cfg.Summarizer.ReprocessBatchSize)
	}
	if cfg.Summarizer.ReflectBatchSize != defaultReflectBatchSize {
		t.Fatalf("expected default reflect batch size %d, got %d", defaultReflectBatchSize, cfg.Summarizer.ReflectBatchSize)
	}
	if cfg.Summarizer.ReflectLLMBatchSize != defaultReflectLLMBatchSize {
		t.Fatalf("expected default reflect LLM batch size %d, got %d", defaultReflectLLMBatchSize, cfg.Summarizer.ReflectLLMBatchSize)
	}
	if cfg.Summarizer.LifecycleReviewBatchSize != defaultLifecycleReviewBatchSize {
		t.Fatalf("expected default lifecycle review batch size %d, got %d", defaultLifecycleReviewBatchSize, cfg.Summarizer.LifecycleReviewBatchSize)
	}
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
		t.Fatalf("expected default cross-project similarity %.2f, got %.2f", defaultCrossProjectSimilarity, cfg.Summarizer.CrossProjectSimilarityThreshold)
	}

	if cfg.Embeddings.Enabled {
		t.Fatal("expected embeddings disabled by default")
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Fatalf("expected default HTTP addr :8080, got %q", cfg.HTTP.Addr)
	}
	if cfg.HTTP.CORSOrigins != "" {
		t.Fatalf("expected empty HTTP CORS origins by default, got %q", cfg.HTTP.CORSOrigins)
	}
}

func TestLoadConfig_NonexistentFileReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("expected missing config to fall back to defaults, got error: %v", err)
	}

	if cfg != DefaultConfig() {
		t.Fatalf("expected default config for missing file, got %+v", cfg)
	}
}

func TestLoadConfig_JSONMergesValuesWithDefaults(t *testing.T) {
	path := writeRuntimeConfigFile(t, "config.json", `{
		"storage": {
			"db_path": "/tmp/amm-json.db",
			"backend": "postgres",
			"postgres_dsn": "postgres://json-user:json-pass@localhost/amm"
		},
		"retrieval": {
			"default_limit": 42,
			"enable_semantic": true,
			"enable_explain": false
		},
		"privacy": {
			"default_privacy": "shared"
		},
		"maintenance": {
			"auto_reflect": false,
			"auto_compress": false,
			"auto_consolidate": false,
			"auto_detect_contradictions": false
		},
		"summarizer": {
			"endpoint": "https://summary.example/v1",
			"api_key": "summary-key",
			"model": "summary-model",
			"review_endpoint": "https://review.example/v1",
			"review_api_key": "review-key",
			"review_model": "review-model",
			"reprocess_batch_size": 31,
			"reflect_batch_size": 41,
			"reflect_llm_batch_size": 19,
			"lifecycle_review_batch_size": 23,
			"compress_chunk_size": 8,
			"compress_max_events": 199,
			"compress_batch_size": 17,
			"topic_batch_size": 13,
			"embedding_batch_size": 77,
			"cross_project_similarity_threshold": 0.91
		},
		"embeddings": {
			"enabled": true,
			"provider": "api",
			"endpoint": "https://embed.example/v1",
			"api_key": "embed-key",
			"model": "embed-model"
		},
		"http": {
			"addr": "127.0.0.1:9999",
			"cors_origins": "https://example.com"
		}
	}`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load JSON config: %v", err)
	}

	if cfg.Storage.DBPath != "/tmp/amm-json.db" || cfg.Storage.Backend != "postgres" || cfg.Storage.PostgresDSN != "postgres://json-user:json-pass@localhost/amm" {
		t.Fatalf("unexpected storage config: %+v", cfg.Storage)
	}
	if cfg.Retrieval.DefaultLimit != 42 || cfg.Retrieval.AmbientLimit != 5 || !cfg.Retrieval.EnableSemantic || cfg.Retrieval.EnableExplain {
		t.Fatalf("unexpected retrieval config: %+v", cfg.Retrieval)
	}
	if cfg.Privacy.DefaultPrivacy != "shared" {
		t.Fatalf("unexpected privacy config: %+v", cfg.Privacy)
	}
	if cfg.Maintenance.AutoReflect || cfg.Maintenance.AutoCompress || cfg.Maintenance.AutoConsolidate || cfg.Maintenance.AutoDetectContradictions {
		t.Fatalf("unexpected maintenance config: %+v", cfg.Maintenance)
	}
	if cfg.Summarizer.Endpoint != "https://summary.example/v1" || cfg.Summarizer.APIKey != "summary-key" || cfg.Summarizer.Model != "summary-model" {
		t.Fatalf("unexpected summarizer config: %+v", cfg.Summarizer)
	}
	if cfg.Summarizer.ReviewEndpoint != "https://review.example/v1" || cfg.Summarizer.ReviewAPIKey != "review-key" || cfg.Summarizer.ReviewModel != "review-model" {
		t.Fatalf("unexpected review routing config: %+v", cfg.Summarizer)
	}
	if cfg.Summarizer.ReprocessBatchSize != 31 || cfg.Summarizer.ReflectBatchSize != 41 || cfg.Summarizer.ReflectLLMBatchSize != 19 || cfg.Summarizer.LifecycleReviewBatchSize != 23 {
		t.Fatalf("unexpected summarizer batch config: %+v", cfg.Summarizer)
	}
	if cfg.Summarizer.CompressChunkSize != 8 || cfg.Summarizer.CompressMaxEvents != 199 || cfg.Summarizer.CompressBatchSize != 17 || cfg.Summarizer.TopicBatchSize != 13 || cfg.Summarizer.EmbeddingBatchSize != 77 {
		t.Fatalf("unexpected compression/topic config: %+v", cfg.Summarizer)
	}
	if cfg.Summarizer.CrossProjectSimilarityThreshold != 0.91 {
		t.Fatalf("expected cross-project similarity 0.91, got %.2f", cfg.Summarizer.CrossProjectSimilarityThreshold)
	}
	if !cfg.Embeddings.Enabled || cfg.Embeddings.Provider != "api" || cfg.Embeddings.Endpoint != "https://embed.example/v1" || cfg.Embeddings.APIKey != "embed-key" || cfg.Embeddings.Model != "embed-model" {
		t.Fatalf("unexpected embeddings config: %+v", cfg.Embeddings)
	}
	if cfg.HTTP.Addr != "127.0.0.1:9999" || cfg.HTTP.CORSOrigins != "https://example.com" {
		t.Fatalf("unexpected HTTP config: %+v", cfg.HTTP)
	}
}

func TestLoadConfig_TOMLParsesSectionsAndAppliesDefaults(t *testing.T) {
	path := writeRuntimeConfigFile(t, "config.toml", `
[storage]
db_path = "/tmp/amm-toml.db"
backend = "postgres"
postgres_dsn = "postgres://toml-user:toml-pass@localhost/amm"

[retrieval]
default_limit = 21
ambient_limit = 7
enable_semantic = true
enable_explain = false

[privacy]
default_privacy = "public_safe"

[maintenance]
auto_reflect = false
auto_compress = false
auto_consolidate = false
auto_detect_contradictions = false

[summarizer]
endpoint = "https://summary.toml/v1"
api_key = "toml-key"
model = "toml-model"
review_endpoint = "https://review.toml/v1"
review_api_key = "toml-review-key"
review_model = "toml-review-model"
 reprocess_batch_size = 44
reflect_batch_size = 55
reflect_llm_batch_size = 12
lifecycle_review_batch_size = 28
compress_chunk_size = 6
compress_max_events = 222
compress_batch_size = 16
topic_batch_size = 11
embedding_batch_size = 88
cross_project_similarity_threshold = 0.76

[embeddings]
enabled = true
provider = "noop-local"
endpoint = "https://embed.toml/v1"
api_key = "embed-toml-key"
model = "embed-toml-model"

malformed line without equals

[http]
addr = ""
cors_origins = "https://a.example,https://b.example"
`)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load TOML config: %v", err)
	}

	if cfg.Storage.DBPath != "/tmp/amm-toml.db" || cfg.Storage.Backend != "postgres" || cfg.Storage.PostgresDSN != "postgres://toml-user:toml-pass@localhost/amm" {
		t.Fatalf("unexpected storage config: %+v", cfg.Storage)
	}
	if cfg.Retrieval.DefaultLimit != 21 || cfg.Retrieval.AmbientLimit != 7 || !cfg.Retrieval.EnableSemantic || cfg.Retrieval.EnableExplain {
		t.Fatalf("unexpected retrieval config: %+v", cfg.Retrieval)
	}
	if cfg.Privacy.DefaultPrivacy != "public_safe" {
		t.Fatalf("unexpected privacy config: %+v", cfg.Privacy)
	}
	if cfg.Maintenance.AutoReflect || cfg.Maintenance.AutoCompress || cfg.Maintenance.AutoConsolidate || cfg.Maintenance.AutoDetectContradictions {
		t.Fatalf("unexpected maintenance config: %+v", cfg.Maintenance)
	}
	if cfg.Summarizer.Endpoint != "https://summary.toml/v1" || cfg.Summarizer.APIKey != "toml-key" || cfg.Summarizer.Model != "toml-model" {
		t.Fatalf("unexpected summarizer config: %+v", cfg.Summarizer)
	}
	if cfg.Summarizer.ReviewEndpoint != "https://review.toml/v1" || cfg.Summarizer.ReviewAPIKey != "toml-review-key" || cfg.Summarizer.ReviewModel != "toml-review-model" {
		t.Fatalf("unexpected review routing config: %+v", cfg.Summarizer)
	}
	if cfg.Summarizer.ReprocessBatchSize != 44 || cfg.Summarizer.ReflectBatchSize != 55 || cfg.Summarizer.ReflectLLMBatchSize != 12 || cfg.Summarizer.LifecycleReviewBatchSize != 28 {
		t.Fatalf("unexpected summarizer batch config: %+v", cfg.Summarizer)
	}
	if cfg.Summarizer.CompressChunkSize != 6 || cfg.Summarizer.CompressMaxEvents != 222 || cfg.Summarizer.CompressBatchSize != 16 || cfg.Summarizer.TopicBatchSize != 11 || cfg.Summarizer.EmbeddingBatchSize != 88 {
		t.Fatalf("unexpected compression/topic config: %+v", cfg.Summarizer)
	}
	if cfg.Summarizer.CrossProjectSimilarityThreshold != 0.76 {
		t.Fatalf("expected cross-project similarity 0.76, got %.2f", cfg.Summarizer.CrossProjectSimilarityThreshold)
	}
	if !cfg.Embeddings.Enabled || cfg.Embeddings.Provider != "noop-local" || cfg.Embeddings.Endpoint != "https://embed.toml/v1" || cfg.Embeddings.APIKey != "embed-toml-key" || cfg.Embeddings.Model != "embed-toml-model" {
		t.Fatalf("unexpected embeddings config: %+v", cfg.Embeddings)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Fatalf("expected empty TOML HTTP addr to fall back to :8080, got %q", cfg.HTTP.Addr)
	}
	if cfg.HTTP.CORSOrigins != "https://a.example,https://b.example" {
		t.Fatalf("unexpected HTTP config: %+v", cfg.HTTP)
	}
}

func TestLoadConfig_InvalidJSONReturnsError(t *testing.T) {
	path := writeRuntimeConfigFile(t, "config.json", `{"storage":`)

	cfg, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected invalid JSON config to return an error")
	}
	if cfg != DefaultConfig() {
		t.Fatalf("expected invalid JSON to return default config, got %+v", cfg)
	}
}

func TestConfigFromEnv_OverridesAllFields(t *testing.T) {
	env := map[string]string{
		"AMM_DB_PATH":                            "/tmp/from-env.db",
		"AMM_STORAGE_BACKEND":                    "  POSTGRES  ",
		"AMM_POSTGRES_DSN":                       "postgres://env-user:env-pass@localhost/amm",
		"AMM_DEFAULT_LIMIT":                      "24",
		"AMM_AMBIENT_LIMIT":                      "12",
		"AMM_ENABLE_SEMANTIC":                    "true",
		"AMM_ENABLE_EXPLAIN":                     "false",
		"AMM_DEFAULT_PRIVACY":                    "shared",
		"AMM_AUTO_REFLECT":                       "false",
		"AMM_AUTO_COMPRESS":                      "false",
		"AMM_AUTO_CONSOLIDATE":                   "false",
		"AMM_AUTO_DETECT_CONTRADICTIONS":         "false",
		"AMM_SUMMARIZER_ENDPOINT":                "https://summary.env/v1",
		"AMM_SUMMARIZER_API_KEY":                 "summary-env-key",
		"AMM_SUMMARIZER_MODEL":                   "summary-env-model",
		"AMM_REPROCESS_BATCH_SIZE":                "90",
		"AMM_REVIEW_ENDPOINT":                    "https://review.env/v1",
		"AMM_REVIEW_API_KEY":                     "review-env-key",
		"AMM_REVIEW_MODEL":                       "review-env-model",
		"AMM_REFLECT_BATCH_SIZE":                 "80",
		"AMM_REFLECT_LLM_BATCH_SIZE":             "11",
		"AMM_LIFECYCLE_REVIEW_BATCH_SIZE":        "18",
		"AMM_COMPRESS_CHUNK_SIZE":                "5",
		"AMM_COMPRESS_MAX_EVENTS":                "155",
		"AMM_COMPRESS_BATCH_SIZE":                "14",
		"AMM_TOPIC_BATCH_SIZE":                   "9",
		"AMM_EMBEDDING_BATCH_SIZE":               "66",
		"AMM_CROSS_PROJECT_SIMILARITY_THRESHOLD": "0.84",
		"AMM_EMBEDDINGS_ENABLED":                 "true",
		"AMM_EMBEDDINGS_PROVIDER":                "local-noop",
		"AMM_EMBEDDINGS_ENDPOINT":                "https://embed.env/v1",
		"AMM_EMBEDDINGS_API_KEY":                 "embed-env-key",
		"AMM_EMBEDDINGS_MODEL":                   "embed-env-model",
		"AMM_HTTP_ADDR":                          "0.0.0.0:8181",
		"AMM_HTTP_CORS_ORIGINS":                  "https://one.example,https://two.example",
	}
	for key, value := range env {
		t.Setenv(key, value)
	}

	cfg := ConfigFromEnv(DefaultConfig())

	if cfg.Storage.DBPath != "/tmp/from-env.db" || cfg.Storage.Backend != "postgres" || cfg.Storage.PostgresDSN != "postgres://env-user:env-pass@localhost/amm" {
		t.Fatalf("unexpected storage config: %+v", cfg.Storage)
	}
	if cfg.Retrieval.DefaultLimit != 24 || cfg.Retrieval.AmbientLimit != 12 || !cfg.Retrieval.EnableSemantic || cfg.Retrieval.EnableExplain {
		t.Fatalf("unexpected retrieval config: %+v", cfg.Retrieval)
	}
	if cfg.Privacy.DefaultPrivacy != "shared" {
		t.Fatalf("unexpected privacy config: %+v", cfg.Privacy)
	}
	if cfg.Maintenance.AutoReflect || cfg.Maintenance.AutoCompress || cfg.Maintenance.AutoConsolidate || cfg.Maintenance.AutoDetectContradictions {
		t.Fatalf("unexpected maintenance config: %+v", cfg.Maintenance)
	}
	if cfg.Summarizer.Endpoint != "https://summary.env/v1" || cfg.Summarizer.APIKey != "summary-env-key" || cfg.Summarizer.Model != "summary-env-model" {
		t.Fatalf("unexpected summarizer config: %+v", cfg.Summarizer)
	}
	if cfg.Summarizer.ReviewEndpoint != "https://review.env/v1" || cfg.Summarizer.ReviewAPIKey != "review-env-key" || cfg.Summarizer.ReviewModel != "review-env-model" {
		t.Fatalf("unexpected review routing config: %+v", cfg.Summarizer)
	}
	if cfg.Summarizer.ReprocessBatchSize != 90 || cfg.Summarizer.ReflectBatchSize != 80 || cfg.Summarizer.ReflectLLMBatchSize != 11 || cfg.Summarizer.LifecycleReviewBatchSize != 18 {
		t.Fatalf("unexpected summarizer batch config: %+v", cfg.Summarizer)
	}
	if cfg.Summarizer.CompressChunkSize != 5 || cfg.Summarizer.CompressMaxEvents != 155 || cfg.Summarizer.CompressBatchSize != 14 || cfg.Summarizer.TopicBatchSize != 9 || cfg.Summarizer.EmbeddingBatchSize != 66 {
		t.Fatalf("unexpected compression/topic config: %+v", cfg.Summarizer)
	}
	if cfg.Summarizer.CrossProjectSimilarityThreshold != 0.84 {
		t.Fatalf("expected cross-project similarity 0.84, got %.2f", cfg.Summarizer.CrossProjectSimilarityThreshold)
	}
	if !cfg.Embeddings.Enabled || cfg.Embeddings.Provider != "local-noop" || cfg.Embeddings.Endpoint != "https://embed.env/v1" || cfg.Embeddings.APIKey != "embed-env-key" || cfg.Embeddings.Model != "embed-env-model" {
		t.Fatalf("unexpected embeddings config: %+v", cfg.Embeddings)
	}
	if cfg.HTTP.Addr != "0.0.0.0:8181" || cfg.HTTP.CORSOrigins != "https://one.example,https://two.example" {
		t.Fatalf("unexpected HTTP config: %+v", cfg.HTTP)
	}
}

func TestConfigFromEnv_InvalidAndEmptyValuesDoNotOverride(t *testing.T) {
	base := DefaultConfig()
	base.Storage.DBPath = "/tmp/base.db"
	base.Storage.Backend = ""
	base.Storage.PostgresDSN = "postgres://base"
	base.Retrieval.DefaultLimit = 17
	base.Retrieval.AmbientLimit = 8
	base.Retrieval.EnableSemantic = true
	base.Retrieval.EnableExplain = false
	base.Privacy.DefaultPrivacy = "shared"
	base.Maintenance.AutoReflect = false
	base.Maintenance.AutoCompress = false
	base.Maintenance.AutoConsolidate = false
	base.Maintenance.AutoDetectContradictions = false
	base.Summarizer.Endpoint = "https://base-summary"
	base.Summarizer.APIKey = "base-key"
	base.Summarizer.Model = "base-model"
	base.Summarizer.ReviewEndpoint = "https://base-review"
	base.Summarizer.ReviewAPIKey = "base-review-key"
	base.Summarizer.ReviewModel = "base-review-model"
	base.Summarizer.ReprocessBatchSize = 29
	base.Summarizer.ReflectBatchSize = 39
	base.Summarizer.ReflectLLMBatchSize = 49
	base.Summarizer.LifecycleReviewBatchSize = 59
	base.Summarizer.CompressChunkSize = 69
	base.Summarizer.CompressMaxEvents = 79
	base.Summarizer.CompressBatchSize = 89
	base.Summarizer.TopicBatchSize = 99
	base.Summarizer.EmbeddingBatchSize = 109
	base.Summarizer.CrossProjectSimilarityThreshold = 0.63
	base.Embeddings.Enabled = true
	base.Embeddings.Provider = "base-provider"
	base.Embeddings.Endpoint = "https://base-embed"
	base.Embeddings.APIKey = "base-embed-key"
	base.Embeddings.Model = "base-embed-model"
	base.HTTP.Addr = ""
	base.HTTP.CORSOrigins = "base-origin"

	env := map[string]string{
		"AMM_DB_PATH":                            "",
		"AMM_STORAGE_BACKEND":                    "   ",
		"AMM_POSTGRES_DSN":                       "",
		"AMM_DEFAULT_LIMIT":                      "zero",
		"AMM_AMBIENT_LIMIT":                      "0",
		"AMM_ENABLE_SEMANTIC":                    "not-bool",
		"AMM_ENABLE_EXPLAIN":                     "not-bool",
		"AMM_DEFAULT_PRIVACY":                    "",
		"AMM_AUTO_REFLECT":                       "not-bool",
		"AMM_AUTO_COMPRESS":                      "not-bool",
		"AMM_AUTO_CONSOLIDATE":                   "not-bool",
		"AMM_AUTO_DETECT_CONTRADICTIONS":         "not-bool",
		"AMM_SUMMARIZER_ENDPOINT":                "",
		"AMM_SUMMARIZER_API_KEY":                 "",
		"AMM_SUMMARIZER_MODEL":                   "",
		"AMM_REPROCESS_BATCH_SIZE":                "0",
		"AMM_REVIEW_ENDPOINT":                    "",
		"AMM_REVIEW_API_KEY":                     "",
		"AMM_REVIEW_MODEL":                       "",
		"AMM_REFLECT_BATCH_SIZE":                 "-1",
		"AMM_REFLECT_LLM_BATCH_SIZE":             "bad",
		"AMM_LIFECYCLE_REVIEW_BATCH_SIZE":        "0",
		"AMM_COMPRESS_CHUNK_SIZE":                "0",
		"AMM_COMPRESS_MAX_EVENTS":                "-10",
		"AMM_COMPRESS_BATCH_SIZE":                "zero",
		"AMM_TOPIC_BATCH_SIZE":                   "0",
		"AMM_EMBEDDING_BATCH_SIZE":               "-20",
		"AMM_CROSS_PROJECT_SIMILARITY_THRESHOLD": "0",
		"AMM_EMBEDDINGS_ENABLED":                 "not-bool",
		"AMM_EMBEDDINGS_PROVIDER":                "",
		"AMM_EMBEDDINGS_ENDPOINT":                "",
		"AMM_EMBEDDINGS_API_KEY":                 "",
		"AMM_EMBEDDINGS_MODEL":                   "",
		"AMM_HTTP_ADDR":                          "",
		"AMM_HTTP_CORS_ORIGINS":                  "",
	}
	for key, value := range env {
		t.Setenv(key, value)
	}

	cfg := ConfigFromEnv(base)

	if cfg.Storage.DBPath != "/tmp/base.db" {
		t.Fatalf("expected empty DB path env to keep base value, got %q", cfg.Storage.DBPath)
	}
	if cfg.Storage.Backend != "sqlite" {
		t.Fatalf("expected blank backend to normalize to sqlite, got %q", cfg.Storage.Backend)
	}
	if cfg.Storage.PostgresDSN != "postgres://base" {
		t.Fatalf("expected empty DSN env to keep base value, got %q", cfg.Storage.PostgresDSN)
	}
	if cfg.Retrieval.DefaultLimit != 17 || cfg.Retrieval.AmbientLimit != 8 || !cfg.Retrieval.EnableSemantic || cfg.Retrieval.EnableExplain {
		t.Fatalf("unexpected retrieval config after invalid env: %+v", cfg.Retrieval)
	}
	if cfg.Privacy.DefaultPrivacy != "shared" {
		t.Fatalf("expected empty privacy env to keep base value, got %q", cfg.Privacy.DefaultPrivacy)
	}
	if cfg.Maintenance.AutoReflect || cfg.Maintenance.AutoCompress || cfg.Maintenance.AutoConsolidate || cfg.Maintenance.AutoDetectContradictions {
		t.Fatalf("expected invalid maintenance env to keep base values, got %+v", cfg.Maintenance)
	}
	if cfg.Summarizer.Endpoint != "https://base-summary" || cfg.Summarizer.APIKey != "base-key" || cfg.Summarizer.Model != "base-model" {
		t.Fatalf("unexpected summarizer config after invalid env: %+v", cfg.Summarizer)
	}
	if cfg.Summarizer.ReviewEndpoint != "https://base-review" || cfg.Summarizer.ReviewAPIKey != "base-review-key" || cfg.Summarizer.ReviewModel != "base-review-model" {
		t.Fatalf("unexpected review routing config after invalid env: %+v", cfg.Summarizer)
	}
	if cfg.Summarizer.ReprocessBatchSize != 29 || cfg.Summarizer.ReflectBatchSize != 39 || cfg.Summarizer.ReflectLLMBatchSize != 49 || cfg.Summarizer.LifecycleReviewBatchSize != 59 {
		t.Fatalf("unexpected summarizer batch config after invalid env: %+v", cfg.Summarizer)
	}
	if cfg.Summarizer.CompressChunkSize != 69 || cfg.Summarizer.CompressMaxEvents != 79 || cfg.Summarizer.CompressBatchSize != 89 || cfg.Summarizer.TopicBatchSize != 99 || cfg.Summarizer.EmbeddingBatchSize != 109 {
		t.Fatalf("unexpected compression/topic config after invalid env: %+v", cfg.Summarizer)
	}
	if cfg.Summarizer.CrossProjectSimilarityThreshold != 0.63 {
		t.Fatalf("expected invalid similarity env to keep base value, got %.2f", cfg.Summarizer.CrossProjectSimilarityThreshold)
	}
	if !cfg.Embeddings.Enabled || cfg.Embeddings.Provider != "base-provider" || cfg.Embeddings.Endpoint != "https://base-embed" || cfg.Embeddings.APIKey != "base-embed-key" || cfg.Embeddings.Model != "base-embed-model" {
		t.Fatalf("unexpected embeddings config after invalid env: %+v", cfg.Embeddings)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Fatalf("expected empty HTTP addr to default to :8080, got %q", cfg.HTTP.Addr)
	}
	if cfg.HTTP.CORSOrigins != "base-origin" {
		t.Fatalf("expected empty CORS env to keep base value, got %q", cfg.HTTP.CORSOrigins)
	}
}

func TestLoadConfigWithEnv_HomeConfigAndEnvPrecedence(t *testing.T) {
	home := t.TempDir()
	ammDir := filepath.Join(home, ".amm")
	if err := os.MkdirAll(ammDir, 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	jsonPath := filepath.Join(ammDir, "config.json")
	jsonContent := `{
		"storage": {
			"db_path": "/tmp/from-file.db",
			"backend": "postgres",
			"postgres_dsn": "postgres://file-user:file-pass@localhost/amm"
		},
		"summarizer": {
			"reprocess_batch_size": 33
		},
		"http": {
			"addr": "127.0.0.1:7000",
			"cors_origins": "https://file.example"
		}
	}`
	if err := os.WriteFile(jsonPath, []byte(jsonContent), 0o600); err != nil {
		t.Fatalf("write home config: %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("AMM_DB_PATH", "/tmp/from-env.db")
	t.Setenv("AMM_STORAGE_BACKEND", "sqlite")
	t.Setenv("AMM_REPROCESS_BATCH_SIZE", "54")
	t.Setenv("AMM_HTTP_ADDR", "127.0.0.1:8088")
	t.Setenv("AMM_HTTP_CORS_ORIGINS", "https://env.example")

	cfg := LoadConfigWithEnv()

	if cfg.Storage.DBPath != "/tmp/from-env.db" {
		t.Fatalf("expected env DB path to win, got %q", cfg.Storage.DBPath)
	}
	if cfg.Storage.Backend != "sqlite" {
		t.Fatalf("expected env backend to win, got %q", cfg.Storage.Backend)
	}
	if cfg.Storage.PostgresDSN != "postgres://file-user:file-pass@localhost/amm" {
		t.Fatalf("expected DSN from file to remain, got %q", cfg.Storage.PostgresDSN)
	}
	if cfg.Summarizer.ReprocessBatchSize != 54 {
		t.Fatalf("expected env batch size to win, got %d", cfg.Summarizer.ReprocessBatchSize)
	}
	if cfg.HTTP.Addr != "127.0.0.1:8088" || cfg.HTTP.CORSOrigins != "https://env.example" {
		t.Fatalf("expected env HTTP values to win, got %+v", cfg.HTTP)
	}
}

func TestLoadConfigWithEnv_WithoutHomeConfigReturnsDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := LoadConfigWithEnv()

	if cfg.Storage.Backend != "sqlite" {
		t.Fatalf("expected sqlite backend by default, got %q", cfg.Storage.Backend)
	}
	if cfg.Retrieval.DefaultLimit != 10 || cfg.Retrieval.AmbientLimit != 5 {
		t.Fatalf("expected default retrieval limits, got %+v", cfg.Retrieval)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Fatalf("expected default HTTP addr, got %q", cfg.HTTP.Addr)
	}
}

func TestApplyConfigDefaults_SetsHTTPAddrWhenBlank(t *testing.T) {
	cfg := Config{HTTP: HTTPConfig{Addr: "   "}}

	applyConfigDefaults(&cfg)

	if cfg.HTTP.Addr != ":8080" {
		t.Fatalf("expected blank HTTP addr to default to :8080, got %q", cfg.HTTP.Addr)
	}
}
