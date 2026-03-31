package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

type spyEmbeddingProvider struct {
	model string
	err   error
	texts []string
}

func (p *spyEmbeddingProvider) Name() string { return "spy" }

func (p *spyEmbeddingProvider) Model() string { return p.model }

func (p *spyEmbeddingProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	p.texts = append(p.texts, texts...)
	if p.err != nil {
		return nil, p.err
	}
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{float32(len(texts[i]))}
	}
	return vectors, nil
}

type passthroughSummarizer struct{}

func (passthroughSummarizer) Summarize(_ context.Context, content string, maxLen int) (string, error) {
	if len(content) <= maxLen {
		return content, nil
	}
	return content[:maxLen], nil
}

func (passthroughSummarizer) ExtractMemoryCandidate(context.Context, string) ([]core.MemoryCandidate, error) {
	return nil, nil
}

func (passthroughSummarizer) ExtractMemoryCandidateBatch(context.Context, []string) ([]core.MemoryCandidate, error) {
	return nil, nil
}

func seedEventForSummary(t *testing.T, svc *AMMService) {
	t.Helper()
	_, err := svc.IngestEvent(context.Background(), &core.Event{
		Kind:         "message",
		SourceSystem: "test",
		SessionID:    "sess_embed",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "summary source event",
		OccurredAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("IngestEvent: %v", err)
	}
}

func TestEmbeddings_RememberWritesEmbedding(t *testing.T) {
	svc, repo := testServiceAndRepoWithSummarizer(t, passthroughSummarizer{})
	provider := &spyEmbeddingProvider{model: "test-model"}
	svc.embeddingProvider = provider
	ctx := context.Background()

	memory, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Subject:          "alice",
		Body:             "likes concise replies",
		TightDescription: "alice preference",
	})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	if _, err := repo.GetEmbedding(ctx, memory.ID, "memory", provider.Model()); err != nil {
		t.Fatalf("GetEmbedding memory: %v", err)
	}

	joined := strings.Join(provider.texts, "\n")
	if !strings.Contains(joined, "alice") || !strings.Contains(joined, "likes concise replies") || !strings.Contains(joined, "alice preference") {
		t.Fatalf("expected memory embedding text to include subject/body/tight_description, got %q", joined)
	}
}

func TestEmbeddings_RebuildCatchesSummariesFromCompress(t *testing.T) {
	svc, repo := testServiceAndRepoWithSummarizer(t, passthroughSummarizer{})
	provider := &spyEmbeddingProvider{model: "test-model"}
	svc.embeddingProvider = provider
	ctx := context.Background()

	seedEventForSummary(t, svc)
	createdSummaries, err := svc.CompressHistory(ctx)
	if err != nil {
		t.Fatalf("CompressHistory: %v", err)
	}
	if createdSummaries == 0 {
		t.Fatal("expected at least one summary to be created")
	}

	summaries, err := repo.ListSummaries(ctx, core.ListSummariesOptions{Limit: 1})
	if err != nil {
		t.Fatalf("ListSummaries: %v", err)
	}
	if len(summaries) == 0 {
		t.Fatal("expected at least one summary")
	}

	if _, err := repo.GetEmbedding(ctx, summaries[0].ID, "summary", provider.Model()); err == nil {
		t.Fatal("expected no summary embedding before rebuild_indexes")
	}

	if _, err := svc.RunJob(ctx, "rebuild_indexes"); err != nil {
		t.Fatalf("rebuild_indexes: %v", err)
	}

	if _, err := repo.GetEmbedding(ctx, summaries[0].ID, "summary", provider.Model()); err != nil {
		t.Fatalf("expected summary embedding after rebuild_indexes: %v", err)
	}
}

func TestEmbeddings_CanonicalWritesSucceedWhenProviderFails(t *testing.T) {
	svc, repo := testServiceAndRepoWithSummarizer(t, passthroughSummarizer{})
	provider := &spyEmbeddingProvider{model: "broken-model", err: errors.New("embed failed")}
	svc.embeddingProvider = provider
	ctx := context.Background()

	memory, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "canonical write should survive embedding failure",
		TightDescription: "write survives",
	})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}
	if _, err := repo.GetMemory(ctx, memory.ID); err != nil {
		t.Fatalf("GetMemory: %v", err)
	}

	seedEventForSummary(t, svc)
	if _, err := svc.CompressHistory(ctx); err != nil {
		t.Fatalf("CompressHistory: %v", err)
	}
	summaries, err := repo.ListSummaries(ctx, core.ListSummariesOptions{Limit: 1})
	if err != nil {
		t.Fatalf("ListSummaries: %v", err)
	}
	if len(summaries) == 0 {
		t.Fatal("expected summary write to succeed even with embedding failures")
	}
}

func TestEmbeddings_RebuildIndexesRepopulatesDeletedEmbeddings(t *testing.T) {
	svc, repo := testServiceAndRepoWithSummarizer(t, passthroughSummarizer{})
	provider := &spyEmbeddingProvider{model: "rebuild-model"}
	svc.embeddingProvider = provider
	ctx := context.Background()

	memory, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "memory that should get rebuilt embedding",
		TightDescription: "rebuild memory",
	})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	seedEventForSummary(t, svc)
	if _, err := svc.CompressHistory(ctx); err != nil {
		t.Fatalf("CompressHistory: %v", err)
	}
	summaries, err := repo.ListSummaries(ctx, core.ListSummariesOptions{Limit: 1})
	if err != nil {
		t.Fatalf("ListSummaries: %v", err)
	}
	if len(summaries) == 0 {
		t.Fatal("expected summary to exist")
	}
	summaryID := summaries[0].ID

	if err := repo.DeleteEmbeddings(ctx, memory.ID, "memory", provider.Model()); err != nil {
		t.Fatalf("DeleteEmbeddings memory: %v", err)
	}
	if err := repo.DeleteEmbeddings(ctx, summaryID, "summary", provider.Model()); err != nil {
		t.Fatalf("DeleteEmbeddings summary: %v", err)
	}

	if _, err := repo.GetEmbedding(ctx, memory.ID, "memory", provider.Model()); err == nil {
		t.Fatal("expected memory embedding to be deleted before rebuild")
	}
	if _, err := repo.GetEmbedding(ctx, summaryID, "summary", provider.Model()); err == nil {
		t.Fatal("expected summary embedding to be deleted before rebuild")
	}

	if _, err := svc.RunJob(ctx, "rebuild_indexes"); err != nil {
		t.Fatalf("RunJob rebuild_indexes: %v", err)
	}

	if _, err := repo.GetEmbedding(ctx, memory.ID, "memory", provider.Model()); err != nil {
		t.Fatalf("GetEmbedding memory after rebuild: %v", err)
	}
	if _, err := repo.GetEmbedding(ctx, summaryID, "summary", provider.Model()); err != nil {
		t.Fatalf("GetEmbedding summary after rebuild: %v", err)
	}
}

func TestEmbeddings_RememberSkipsZeroMagnitudeVectors(t *testing.T) {
	candidate := &core.Memory{
		Type:             core.MemoryTypeFact,
		Subject:          "oov subject",
		Body:             "oov body",
		TightDescription: "oov tight description",
	}
	provider := staticEmbeddingProvider{
		model: "zero-model",
		vectors: map[string][]float32{
			buildMemoryEmbeddingText(candidate): {0, 0},
		},
	}
	svc, repo := testServiceAndRepoWithEmbeddingProvider(t, provider)
	ctx := context.Background()

	memory, err := svc.Remember(ctx, candidate)
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	if _, err := repo.GetEmbedding(ctx, memory.ID, "memory", provider.Model()); err == nil {
		t.Fatal("expected zero-magnitude embedding to be skipped")
	}
}

func TestEmbeddings_RebuildFullDeletesZeroMagnitudeVectors(t *testing.T) {
	candidate := &core.Memory{
		Type:             core.MemoryTypeFact,
		Subject:          "persisted subject",
		Body:             "persisted body",
		TightDescription: "persisted tight description",
	}
	text := buildMemoryEmbeddingText(candidate)
	provider := staticEmbeddingProvider{
		model: "rebuild-zero-model",
		vectors: map[string][]float32{
			text: {1, 0},
		},
	}
	svc, repo := testServiceAndRepoWithEmbeddingProvider(t, provider)
	ctx := context.Background()

	memory, err := svc.Remember(ctx, candidate)
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}
	if _, err := repo.GetEmbedding(ctx, memory.ID, "memory", provider.Model()); err != nil {
		t.Fatalf("expected initial embedding: %v", err)
	}

	provider.vectors[text] = []float32{0, 0}
	svc.embeddingProvider = provider

	if _, err := svc.RunJob(ctx, "rebuild_indexes_full"); err != nil {
		t.Fatalf("RunJob rebuild_indexes_full: %v", err)
	}
	if _, err := repo.GetEmbedding(ctx, memory.ID, "memory", provider.Model()); err == nil {
		t.Fatal("expected rebuild_indexes_full to delete zero-magnitude embeddings")
	}
}

func TestBuildQueryEmbedding_SkipsZeroMagnitudeVectors(t *testing.T) {
	provider := staticEmbeddingProvider{
		model: "query-zero-model",
		vectors: map[string][]float32{
			"all oov query": {0, 0},
		},
	}
	svc, _ := testServiceAndRepoWithEmbeddingProvider(t, provider)

	if vec := svc.buildQueryEmbedding(context.Background(), "all oov query"); vec != nil {
		t.Fatalf("expected nil query embedding for zero-magnitude vector, got %#v", vec)
	}
}
