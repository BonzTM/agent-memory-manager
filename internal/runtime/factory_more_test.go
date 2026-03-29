package runtime

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bonztm/agent-memory-manager/internal/service"
)

func runtimeIntField(t *testing.T, svc *service.AMMService, field string) int {
	t.Helper()
	v := reflect.ValueOf(svc).Elem().FieldByName(field)
	if !v.IsValid() {
		t.Fatalf("missing field %q", field)
	}
	return int(v.Int())
}

func runtimeFloatField(t *testing.T, svc *service.AMMService, field string) float64 {
	t.Helper()
	v := reflect.ValueOf(svc).Elem().FieldByName(field)
	if !v.IsValid() {
		t.Fatalf("missing field %q", field)
	}
	return v.Float()
}

func runtimeInterfaceType(t *testing.T, svc *service.AMMService, field string) string {
	t.Helper()
	v := reflect.ValueOf(svc).Elem().FieldByName(field)
	if !v.IsValid() {
		t.Fatalf("missing field %q", field)
	}
	if v.IsNil() {
		return ""
	}
	return v.Elem().Type().String()
}

func llmModelField(t *testing.T, summarizer *service.LLMSummarizer) string {
	t.Helper()
	v := reflect.ValueOf(summarizer).Elem().FieldByName("model")
	if !v.IsValid() {
		t.Fatal("missing model field on LLM summarizer")
	}
	return v.String()
}

func TestBuildSummarizer_ReturnsNilWithoutCredentials(t *testing.T) {
	if summarizer := buildSummarizer(DefaultConfig()); summarizer != nil {
		t.Fatalf("expected nil summarizer without endpoint and API key, got %T", summarizer)
	}
}

func TestBuildSummarizer_UsesDefaultModelWhenUnset(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Summarizer.Endpoint = "https://summary.example/v1"
	cfg.Summarizer.APIKey = "summary-key"

	summarizer := buildSummarizer(cfg)
	llm, ok := summarizer.(*service.LLMSummarizer)
	if !ok {
		t.Fatalf("expected LLM summarizer, got %T", summarizer)
	}
	if got := llmModelField(t, llm); got != "gpt-4o-mini" {
		t.Fatalf("expected default LLM summarizer model gpt-4o-mini, got %q", got)
	}
}

func TestBuildIntelligenceProvider_RoutesLLMSummarizerToLLMProvider(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Summarizer.ReviewEndpoint = "https://review.example/v1"
	cfg.Summarizer.ReviewAPIKey = "review-key"
	cfg.Summarizer.ReviewModel = "review-model"

	provider := buildIntelligenceProvider(cfg, service.NewLLMSummarizer("https://summary.example/v1", "summary-key", "summary-model"))
	if _, ok := provider.(*service.LLMIntelligenceProvider); !ok {
		t.Fatalf("expected LLM intelligence provider, got %T", provider)
	}
}

func TestBuildIntelligenceProvider_UsesAdapterForNilSummarizer(t *testing.T) {
	provider := buildIntelligenceProvider(DefaultConfig(), nil)
	if _, ok := provider.(*service.HeuristicIntelligenceProvider); !ok {
		t.Fatalf("expected heuristic intelligence provider when summarizer is nil, got %T", provider)
	}
}

func TestBuildEmbeddingProvider_DefaultAPIModelWhenUnset(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Embeddings.Enabled = true
	cfg.Embeddings.Endpoint = "https://embed.example/v1"

	provider := buildEmbeddingProvider(cfg)
	apiProvider, ok := provider.(*service.APIEmbeddingProvider)
	if !ok {
		t.Fatalf("expected API embedding provider, got %T", provider)
	}
	if apiProvider.Model() != "text-embedding-3-small" {
		t.Fatalf("expected default embedding model text-embedding-3-small, got %q", apiProvider.Model())
	}
}

func TestNewService_SQLiteWiresConfiguredProvidersAndBatchSizes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), "runtime.db")
	cfg.Summarizer.Endpoint = "https://summary.example/v1"
	cfg.Summarizer.APIKey = "summary-key"
	cfg.Summarizer.BatchSize = 71
	cfg.Summarizer.ReflectBatchSize = 72
	cfg.Summarizer.ReflectLLMBatchSize = 73
	cfg.Summarizer.LifecycleReviewBatchSize = 74
	cfg.Summarizer.CompressChunkSize = 75
	cfg.Summarizer.CompressMaxEvents = 76
	cfg.Summarizer.CompressBatchSize = 77
	cfg.Summarizer.TopicBatchSize = 78
	cfg.Summarizer.EmbeddingBatchSize = 79
	cfg.Summarizer.CrossProjectSimilarityThreshold = 0.87
	cfg.Embeddings.Enabled = true
	cfg.Embeddings.Provider = "noop-provider"
	cfg.Embeddings.Model = "noop-model"

	svc, cleanup, err := NewService(cfg)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	defer cleanup()

	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Initialized {
		t.Fatal("expected initialized service")
	}

	impl, ok := svc.(*service.AMMService)
	if !ok {
		t.Fatalf("expected *service.AMMService, got %T", svc)
	}

	provider := buildIntelligenceProvider(cfg, buildSummarizer(cfg))
	if provider == nil || !provider.IsLLMBacked() {
		t.Fatal("expected configured intelligence provider to report LLM-backed")
	}
	if got := runtimeInterfaceType(t, impl, "intelligence"); got != "service.LLMIntelligenceProvider" && got != "*service.LLMIntelligenceProvider" {
		t.Fatalf("expected LLM intelligence provider type, got %q", got)
	}
	if got := runtimeInterfaceType(t, impl, "embeddingProvider"); got != "service.NoopEmbeddingProvider" && got != "*service.NoopEmbeddingProvider" {
		t.Fatalf("expected noop embedding provider type, got %q", got)
	}

	if got := runtimeIntField(t, impl, "reprocessBatchSize"); got != 71 {
		t.Fatalf("expected reprocess batch size 71, got %d", got)
	}
	if got := runtimeIntField(t, impl, "reflectBatchSize"); got != 72 {
		t.Fatalf("expected reflect batch size 72, got %d", got)
	}
	if got := runtimeIntField(t, impl, "reflectLLMBatchSize"); got != 73 {
		t.Fatalf("expected reflect LLM batch size 73, got %d", got)
	}
	if got := runtimeIntField(t, impl, "lifecycleReviewBatchSize"); got != 74 {
		t.Fatalf("expected lifecycle review batch size 74, got %d", got)
	}
	if got := runtimeIntField(t, impl, "compressChunkSize"); got != 75 {
		t.Fatalf("expected compress chunk size 75, got %d", got)
	}
	if got := runtimeIntField(t, impl, "compressMaxEvents"); got != 76 {
		t.Fatalf("expected compress max events 76, got %d", got)
	}
	if got := runtimeIntField(t, impl, "compressBatchSize"); got != 77 {
		t.Fatalf("expected compress batch size 77, got %d", got)
	}
	if got := runtimeIntField(t, impl, "topicBatchSize"); got != 78 {
		t.Fatalf("expected topic batch size 78, got %d", got)
	}
	if got := runtimeIntField(t, impl, "embeddingBatchSize"); got != 79 {
		t.Fatalf("expected embedding batch size 79, got %d", got)
	}
	if got := runtimeFloatField(t, impl, "crossProjectSimilarityThreshold"); got != 0.87 {
		t.Fatalf("expected cross-project similarity threshold 0.87, got %.2f", got)
	}
}

func TestNewService_CleanupCanBeCalledMultipleTimes(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), "cleanup.db")

	_, cleanup, err := NewService(cfg)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}

	cleanup()
	cleanup()
}

func TestNewService_ErrorsWhenDBParentIsFile(t *testing.T) {
	tempDir := t.TempDir()
	parent := filepath.Join(tempDir, "not-a-directory")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatalf("write parent file: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Storage.DBPath = filepath.Join(parent, "runtime.db")

	_, cleanup, err := NewService(cfg)
	if cleanup != nil {
		t.Fatal("expected nil cleanup on directory creation error")
	}
	if err == nil || !strings.Contains(err.Error(), "create db directory") {
		t.Fatalf("expected create db directory error, got %v", err)
	}
}

func TestNewService_ErrorsWhenDBDirectoryIsNotWritable(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "readonly")
	if err := os.MkdirAll(dir, 0o500); err != nil {
		t.Fatalf("mkdir readonly dir: %v", err)
	}
	defer os.Chmod(dir, 0o755)

	cfg := DefaultConfig()
	cfg.Storage.DBPath = filepath.Join(dir, "runtime.db")

	_, _, err := NewService(cfg)
	if err == nil {
		t.Fatal("expected sqlite open error for non-writable directory")
	}
	if !strings.Contains(err.Error(), "open sqlite database") {
		t.Fatalf("expected sqlite open error, got %v", err)
	}
}

func TestNewService_PostgresRequiresDSN(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Storage.Backend = "postgres"

	_, cleanup, err := NewService(cfg)
	if cleanup != nil {
		t.Fatal("expected nil cleanup for missing postgres DSN")
	}
	if err == nil || !strings.Contains(err.Error(), "postgres backend requires AMM_POSTGRES_DSN") {
		t.Fatalf("expected missing DSN error, got %v", err)
	}
}

func TestNewService_RejectsUnknownBackend(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Storage.Backend = "mystery"

	_, cleanup, err := NewService(cfg)
	if cleanup != nil {
		t.Fatal("expected nil cleanup for unknown backend")
	}
	if err == nil || !strings.Contains(err.Error(), `unsupported storage backend "mystery"`) {
		t.Fatalf("expected unknown backend error, got %v", err)
	}
}

func TestMaskDSN(t *testing.T) {
	if got := maskDSN(""); got != "" {
		t.Fatalf("expected empty DSN to stay empty, got %q", got)
	}
	if got := maskDSN("postgres://user:pass@localhost:5432/amm"); got != "postgres://***@localhost:5432/amm" {
		t.Fatalf("unexpected masked DSN: %q", got)
	}
	if got := maskDSN("postgres://localhost"); got != "postgres://***" {
		t.Fatalf("unexpected masked DSN without userinfo: %q", got)
	}
}
