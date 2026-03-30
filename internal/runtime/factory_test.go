package runtime

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/bonztm/agent-memory-manager/internal/core"
	"github.com/bonztm/agent-memory-manager/internal/service"
)

func TestBuildEmbeddingProvider_DefaultOff(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Embeddings.Enabled = false
	provider := buildEmbeddingProvider(cfg)
	if provider != nil {
		t.Fatalf("expected nil embedding provider when disabled, got %#v", provider)
	}
}

func TestBuildEmbeddingProvider_EnabledUsesNoop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Embeddings.Enabled = true
	cfg.Embeddings.Provider = "local-noop"
	cfg.Embeddings.Model = "test-model"

	provider := buildEmbeddingProvider(cfg)
	if provider == nil {
		t.Fatal("expected non-nil embedding provider when enabled")
	}
	if provider.Name() != "local-noop" {
		t.Fatalf("expected provider name local-noop, got %q", provider.Name())
	}
	if provider.Model() != "test-model" {
		t.Fatalf("expected provider model test-model, got %q", provider.Model())
	}
}

func TestBuildEmbeddingProvider_EnabledWithEndpointUsesAPI(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Embeddings.Enabled = true
	cfg.Embeddings.Endpoint = "http://localhost:11434/v1"
	cfg.Embeddings.APIKey = "test-key"
	cfg.Embeddings.Model = "test-model"

	provider := buildEmbeddingProvider(cfg)
	if provider == nil {
		t.Fatal("expected non-nil embedding provider when endpoint is configured")
	}
	if _, ok := provider.(*service.APIEmbeddingProvider); !ok {
		t.Fatalf("expected *service.APIEmbeddingProvider, got %T", provider)
	}
}

func TestNewService_DefaultConfigSafeWithoutEmbeddingProvider(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), "runtime-service.db")

	svc, cleanup, err := NewService(cfg)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	t.Cleanup(cleanup)

	status, err := svc.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Initialized {
		t.Fatal("expected initialized service")
	}
}

func TestNewService_AppliesMaxExpandDepth(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Storage.DBPath = filepath.Join(t.TempDir(), "runtime-depth.db")
	cfg.MaxExpandDepth = 1

	svc, cleanup, err := NewService(cfg)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	t.Cleanup(cleanup)

	mem, err := svc.Remember(context.Background(), &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "runtime max expand depth",
		TightDescription: "runtime max expand depth",
	})
	if err != nil {
		t.Fatalf("remember: %v", err)
	}

	_, err = svc.Expand(context.Background(), mem.ID, "memory", core.ExpandOptions{DelegationDepth: 1})
	if !errors.Is(err, core.ErrExpansionRecursionBlocked) {
		t.Fatalf("expected ErrExpansionRecursionBlocked, got %v", err)
	}
}
