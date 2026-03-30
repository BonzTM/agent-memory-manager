package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

type recallBatchWarnRepo struct {
	core.Repository
}

func (r *recallBatchWarnRepo) RecordRecallBatch(context.Context, string, []core.RecallRecord) error {
	return errors.New("record recall batch failed")
}

func TestIngestEvent_NilGuard(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	_, err := svc.IngestEvent(context.Background(), nil)
	if !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestRemember_NilGuard(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	_, err := svc.Remember(context.Background(), nil)
	if !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestUpdateMemory_NilGuard(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	_, err := svc.UpdateMemory(context.Background(), nil)
	if !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestRegisterProject_NilGuard(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	_, err := svc.RegisterProject(context.Background(), nil)
	if !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestReflectFallbackWhenExtractorErrors(t *testing.T) {
	svc, _ := testServiceAndRepoWithSummarizer(t, NewLLMSummarizer("http://127.0.0.1:1", "test-key", "test-model"))
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := svc.IngestEvent(ctx, &core.Event{Kind: "message_user", SourceSystem: "test", PrivacyLevel: core.PrivacyPrivate, Content: "I prefer concise responses", OccurredAt: now}); err != nil {
		t.Fatalf("ingest event: %v", err)
	}

	job, err := svc.RunJob(ctx, "reflect")
	if err != nil {
		t.Fatalf("run reflect job: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("expected completed reflect job, got %+v", job)
	}

	memories, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 20})
	if err != nil {
		t.Fatalf("list memories after reflect fallback: %v", err)
	}
	if len(memories) == 0 {
		t.Fatal("expected heuristic fallback to still create reflected memory")
	}
}

func TestCompressFallbackWhenBatchCompressionErrors(t *testing.T) {
	fallbackSummarize := func(_ string, maxLen int) (string, error) {
		if maxLen <= 200 {
			return "fallback compress tight", nil
		}
		return "fallback compress body", nil
	}

	svc, repo := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{summarize: fallbackSummarize})
	svc.SetIntelligenceProvider(consolidateTestIntelligence{
		summarize: fallbackSummarize,
		compressEventBatches: func([]core.EventChunk) ([]core.CompressionResult, error) {
			return nil, errors.New("compress batches failed")
		},
	})

	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < 6; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message_user",
			SourceSystem: "test",
			SessionID:    "sess_compress_fallback",
			ProjectID:    "proj_compress_fallback",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      "compress fallback content",
			OccurredAt:   now.Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatalf("ingest event %d: %v", i, err)
		}
	}

	job, err := svc.RunJob(ctx, "compress_history")
	if err != nil {
		t.Fatalf("run compress_history job: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("expected completed compress_history job, got %+v", job)
	}

	summaries, err := repo.ListSummaries(ctx, core.ListSummariesOptions{Limit: 20})
	if err != nil {
		t.Fatalf("list summaries after compress fallback: %v", err)
	}
	if len(summaries) == 0 {
		t.Fatal("expected summaries created via fallback path")
	}
}

func TestRecall_RecordRecallBatchWarningPath(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	failingSvc := New(&recallBatchWarnRepo{Repository: repo}, svc.dbPath, nil, nil)
	ctx := context.Background()

	created, err := failingSvc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "recall warning memory",
		TightDescription: "recall warning",
	})
	if err != nil {
		t.Fatalf("remember: %v", err)
	}

	result, err := failingSvc.Recall(ctx, "recall warning", core.RecallOptions{Mode: core.RecallModeFacts, SessionID: "sess_warn", Limit: 10})
	if err != nil {
		t.Fatalf("recall should succeed even when RecordRecallBatch fails: %v", err)
	}
	if result == nil || len(result.Items) == 0 {
		t.Fatalf("expected recall items, got %+v", result)
	}

	found := false
	for _, item := range result.Items {
		if item.ID == created.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected recalled memory %s in results: %+v", created.ID, result.Items)
	}
}
