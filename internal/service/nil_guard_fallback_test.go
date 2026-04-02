package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
	svc, _ := testServiceAndRepoWithSummarizer(t, NewLLMSummarizer("http://127.0.0.1:1", "test-key", "test-model", 0))
	svc.SetMinConfidenceForCreation(0) // Allow heuristic fallback candidates (confidence 0.45)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := svc.IngestEvent(ctx, &core.Event{Kind: "message_user", SourceSystem: "test", PrivacyLevel: core.PrivacyPrivate, Content: "I prefer concise responses because verbosity requires more context", OccurredAt: now}); err != nil {
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

func TestReflect_RetriesHeuristicFallbackOnNextRun(t *testing.T) {
	ctx := context.Background()
	var analyzeCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		prompt := req.Messages[0].Content
		if !strings.Contains(prompt, "In addition to memories") {
			http.Error(w, "unexpected prompt", http.StatusBadRequest)
			return
		}

		call := analyzeCalls.Add(1)
		if call == 1 {
			http.Error(w, "temporary failure", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockChatResponse(`{
			"memories":[{"type":"decision","subject":"tool event policy","body":"Ignore tool events by default during ingestion","tight_description":"ignore tool events by default","confidence":0.93,"source_events":[1]}],
			"entities":[],
			"relationships":[],
			"event_quality":{"1":"durable"}
		}`)))
	}))
	defer server.Close()

	svc, repo := testServiceAndRepoWithSummarizer(t, NewLLMSummarizer(server.URL, "test-key", "test-model", 0))
	svc.SetMinConfidenceForCreation(0)
	now := time.Now().UTC().Truncate(time.Second)
	evt := &core.Event{
		ID:           "evt_reflect_retry",
		Kind:         "message_user",
		SourceSystem: "test",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "We decided to ignore tool events by default because it requires less noisy storage.",
		OccurredAt:   now,
		IngestedAt:   now,
	}
	if err := repo.InsertEvent(ctx, evt); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatalf("first reflect: %v", err)
	}
	if created != 1 {
		t.Fatalf("expected one heuristic memory on first reflect, got %d", created)
	}

	memories, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatalf("list memories after first reflect: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected one active memory after first reflect, got %d", len(memories))
	}
	if got := memories[0].Metadata[MetaExtractionMethod]; got != MethodHeuristic {
		t.Fatalf("expected heuristic extraction_method on first reflect, got %q", got)
	}
	if got := memories[0].Metadata[MetaFallbackCount]; got != "1" {
		t.Fatalf("expected fallback_count=1 on first reflect, got %q", got)
	}

	updatedEvent, err := repo.GetEvent(ctx, evt.ID)
	if err != nil {
		t.Fatalf("get event after first reflect: %v", err)
	}
	if updatedEvent.ReflectedAt != nil {
		t.Fatal("expected first reflect to clear reflected_at for retry")
	}

	created, err = svc.Reflect(ctx, "")
	if err != nil {
		t.Fatalf("second reflect: %v", err)
	}
	if created != 0 {
		t.Fatalf("expected second reflect to upgrade existing memory instead of creating a new one, got %d", created)
	}

	memories, err = repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatalf("list memories after second reflect: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected one active memory after second reflect, got %d", len(memories))
	}
	if got := memories[0].Metadata[MetaExtractionMethod]; got != MethodLLM {
		t.Fatalf("expected llm extraction_method after retry success, got %q", got)
	}
	if got := memories[0].Metadata[MetaFallbackCount]; got != "" {
		t.Fatalf("expected fallback_count to clear after retry success, got %q", got)
	}
	if memories[0].Body != "Ignore tool events by default during ingestion" {
		t.Fatalf("expected llm retry to replace memory body, got %q", memories[0].Body)
	}

	updatedEvent, err = repo.GetEvent(ctx, evt.ID)
	if err != nil {
		t.Fatalf("get event after second reflect: %v", err)
	}
	if updatedEvent.ReflectedAt == nil {
		t.Fatal("expected second reflect to leave reflected_at set after llm success")
	}
}

func TestReflect_RetriesEmptyHeuristicFallbackOnNextRun(t *testing.T) {
	ctx := context.Background()
	var extractionCalls atomic.Int32

	svc, repo := testServiceAndRepo(t)
	svc.SetMinConfidenceForCreation(0)
	svc.SetIntelligenceProvider(consolidateTestIntelligence{
		isLLM: true,
		extractBatchWithMethod: func(contents []string) ([]core.MemoryCandidate, string, error) {
			if len(contents) != 1 {
				t.Fatalf("expected one extraction input, got %d", len(contents))
			}
			if extractionCalls.Add(1) == 1 {
				return nil, MethodHeuristic, nil
			}
			return []core.MemoryCandidate{{
				Type:             core.MemoryTypeDecision,
				Subject:          "tool event policy",
				Body:             "Ignore tool events by default during ingestion",
				TightDescription: "ignore tool events by default",
				Confidence:       0.93,
				SourceEventNums:  []int{1},
			}}, MethodLLM, nil
		},
	})

	now := time.Now().UTC().Truncate(time.Second)
	evt := &core.Event{
		ID:           "evt_reflect_empty_retry",
		Kind:         "message_user",
		SourceSystem: "test",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "We decided to ignore tool events by default because it reduces noise.",
		OccurredAt:   now,
		IngestedAt:   now,
	}
	if err := repo.InsertEvent(ctx, evt); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatalf("first reflect: %v", err)
	}
	if created != 0 {
		t.Fatalf("expected no memories on empty heuristic fallback, got %d", created)
	}

	memories, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatalf("list memories after first reflect: %v", err)
	}
	if len(memories) != 0 {
		t.Fatalf("expected no active memories after empty heuristic fallback, got %d", len(memories))
	}

	updatedEvent, err := repo.GetEvent(ctx, evt.ID)
	if err != nil {
		t.Fatalf("get event after first reflect: %v", err)
	}
	if updatedEvent.ReflectedAt != nil {
		t.Fatal("expected empty heuristic fallback to clear reflected_at for retry")
	}
	if got := updatedEvent.Metadata[metaReflectFallbackCount]; got != "1" {
		t.Fatalf("expected reflect fallback count=1 after first empty fallback, got %q", got)
	}

	created, err = svc.Reflect(ctx, "")
	if err != nil {
		t.Fatalf("second reflect: %v", err)
	}
	if created != 1 {
		t.Fatalf("expected second reflect to create one llm memory, got %d", created)
	}

	memories, err = repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatalf("list memories after second reflect: %v", err)
	}
	if len(memories) != 1 {
		t.Fatalf("expected one active memory after retry success, got %d", len(memories))
	}
	if got := memories[0].Metadata[MetaExtractionMethod]; got != MethodLLM {
		t.Fatalf("expected llm extraction_method after retry success, got %q", got)
	}
	if memories[0].Body != "Ignore tool events by default during ingestion" {
		t.Fatalf("expected llm retry to create final memory body, got %q", memories[0].Body)
	}

	updatedEvent, err = repo.GetEvent(ctx, evt.ID)
	if err != nil {
		t.Fatalf("get event after second reflect: %v", err)
	}
	if updatedEvent.ReflectedAt == nil {
		t.Fatal("expected retry success to leave reflected_at set")
	}
	if got := updatedEvent.Metadata[metaReflectFallbackCount]; got != "" {
		t.Fatalf("expected reflect fallback count to clear after retry success, got %q", got)
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
