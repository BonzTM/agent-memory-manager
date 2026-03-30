package service

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestIdle_CompressHistory_NoNewEventsAfterFrontier(t *testing.T) {
	svc, repo := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{})
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("compress idle event %d", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	events, err := repo.ListEvents(ctx, core.ListEventsOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	frontier := maxEventSequenceID(events) + 100

	now := time.Now().UTC()
	if err := repo.InsertJob(ctx, &core.Job{
		ID:         core.GenerateID("job_"),
		Kind:       "compress",
		Status:     "completed",
		StartedAt:  &now,
		FinishedAt: &now,
		Result: map[string]string{
			"max_sequence_id": fmt.Sprintf("%d", frontier),
			"created":         "5",
		},
		CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	var compressBatchCalls int32
	svc.SetIntelligenceProvider(consolidateTestIntelligence{compressBatchCallCountPtr: &compressBatchCalls})

	created, err := svc.CompressHistory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("expected 0 summaries created, got %d", created)
	}
	if atomic.LoadInt32(&compressBatchCalls) != 0 {
		t.Fatalf("expected 0 compress intelligence calls, got %d", atomic.LoadInt32(&compressBatchCalls))
	}
}

func TestIdle_ConsolidateSessions_NoNewEventsAfterFrontier(t *testing.T) {
	svc, repo := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{})
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			SessionID:    "sess_idle_frontier",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("consolidate idle event %d", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	events, err := repo.ListEvents(ctx, core.ListEventsOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	frontier := maxEventSequenceID(events) + 100

	now := time.Now().UTC()
	if err := repo.InsertJob(ctx, &core.Job{
		ID:         core.GenerateID("job_"),
		Kind:       "consolidate_sessions",
		Status:     "completed",
		StartedAt:  &now,
		FinishedAt: &now,
		Result: map[string]string{
			"max_sequence_id": fmt.Sprintf("%d", frontier),
			"created":         "5",
		},
		CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	var consolidateCalls int32
	svc.SetIntelligenceProvider(consolidateTestIntelligence{callCountPtr: &consolidateCalls})

	created, err := svc.ConsolidateSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("expected 0 session summaries created, got %d", created)
	}
	if atomic.LoadInt32(&consolidateCalls) != 0 {
		t.Fatalf("expected 0 consolidate intelligence calls, got %d", atomic.LoadInt32(&consolidateCalls))
	}
}

func TestIdle_ConsolidateSessions_AllSessionsAlreadySummarized(t *testing.T) {
	svc, repo := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{})
	ctx := context.Background()

	sessions := []string{"sess_idle_summary_a", "sess_idle_summary_b"}
	for i, sessionID := range sessions {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			SessionID:    sessionID,
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("session %s event", sessionID),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}

		now := time.Now().UTC().Add(time.Duration(i) * time.Second)
		if err := repo.InsertSummary(ctx, &core.Summary{
			ID:               core.GenerateID("sum_"),
			Kind:             "session",
			SessionID:        sessionID,
			Scope:            core.ScopeGlobal,
			Body:             "existing session summary",
			TightDescription: "existing session summary",
			PrivacyLevel:     core.PrivacyPrivate,
			CreatedAt:        now,
			UpdatedAt:        now,
		}); err != nil {
			t.Fatal(err)
		}
	}

	var consolidateCalls int32
	svc.SetIntelligenceProvider(consolidateTestIntelligence{callCountPtr: &consolidateCalls})

	created, err := svc.ConsolidateSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("expected 0 session summaries created, got %d", created)
	}
	if atomic.LoadInt32(&consolidateCalls) != 0 {
		t.Fatalf("expected 0 consolidate intelligence calls, got %d", atomic.LoadInt32(&consolidateCalls))
	}
}

func TestIdle_BuildTopicSummaries_AllLeavesAlreadyParented(t *testing.T) {
	svc, repo := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{})
	ctx := context.Background()

	leafIDs := make([]string, 3)
	for i, body := range []string{
		"Alice and Bob reviewed SQLite rollout for AMM",
		"Bob asked Alice about AMM SQLite recovery details",
		"AMM SQLite incident notes by Alice and Bob",
	} {
		leafIDs[i] = core.GenerateID("sum_")
		now := time.Now().UTC().Add(-10*time.Minute + time.Duration(i)*time.Second)
		if err := repo.InsertSummary(ctx, &core.Summary{
			ID:               leafIDs[i],
			Kind:             "leaf",
			Scope:            core.ScopeGlobal,
			Body:             body,
			TightDescription: fmt.Sprintf("leaf idle %d", i),
			PrivacyLevel:     core.PrivacyPrivate,
			CreatedAt:        now,
			UpdatedAt:        now,
		}); err != nil {
			t.Fatal(err)
		}
	}

	parentID := core.GenerateID("sum_")
	parentNow := time.Now().UTC().Add(-5 * time.Minute)
	if err := repo.InsertSummary(ctx, &core.Summary{
		ID:               parentID,
		Kind:             "topic",
		Scope:            core.ScopeGlobal,
		Body:             "existing topic summary",
		TightDescription: "existing topic",
		PrivacyLevel:     core.PrivacyPrivate,
		CreatedAt:        parentNow,
		UpdatedAt:        parentNow,
	}); err != nil {
		t.Fatal(err)
	}
	for order, childID := range leafIDs {
		if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{
			ParentSummaryID: parentID,
			ChildKind:       "summary",
			ChildID:         childID,
			EdgeOrder:       order,
		}); err != nil {
			t.Fatal(err)
		}
	}

	var topicBatchCalls int32
	svc.SetIntelligenceProvider(consolidateTestIntelligence{topicBatchCallCountPtr: &topicBatchCalls})

	created, err := svc.BuildTopicSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("expected 0 topic summaries created, got %d", created)
	}
	if atomic.LoadInt32(&topicBatchCalls) != 0 {
		t.Fatalf("expected 0 topic intelligence calls, got %d", atomic.LoadInt32(&topicBatchCalls))
	}
}

func TestIdle_BuildTopicSummaries_UngroupableLeavesRecordNoOpJob(t *testing.T) {
	svc, repo := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{})
	ctx := context.Background()

	now := time.Now().UTC()
	if err := repo.InsertSummary(ctx, &core.Summary{
			ID:               core.GenerateID("sum_"),
		Kind:             "leaf",
		Scope:            core.ScopeGlobal,
		Body:             "single isolated leaf about unique topic xyz",
		TightDescription: "isolated leaf",
		PrivacyLevel:     core.PrivacyPrivate,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatal(err)
	}

	var topicBatchCalls int32
	svc.SetIntelligenceProvider(consolidateTestIntelligence{topicBatchCallCountPtr: &topicBatchCalls})

	created, err := svc.BuildTopicSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("expected 0 topic summaries for ungroupable leaves, got %d", created)
	}
	if atomic.LoadInt32(&topicBatchCalls) != 0 {
		t.Fatalf("expected 0 topic intelligence calls, got %d", atomic.LoadInt32(&topicBatchCalls))
	}

	jobs, err := repo.ListJobs(ctx, core.ListJobsOptions{Kind: "build_topic_summaries", Status: "completed", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) == 0 {
		t.Fatal("expected no-op topic job to be recorded so gate short-circuits next run")
	}
	if jobs[0].Result["created"] != "0" {
		t.Fatalf("expected no-op job created=0, got %s", jobs[0].Result["created"])
	}
}

func TestIdle_EnrichMemories_AllAlreadyExtracted(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		if err := repo.InsertMemory(ctx, &core.Memory{
			ID:               core.GenerateID("mem_"),
			Type:             core.MemoryTypeFact,
			Scope:            core.ScopeGlobal,
			Body:             fmt.Sprintf("already enriched memory %d", i),
			TightDescription: fmt.Sprintf("already enriched memory %d", i),
			Confidence:       0.8,
			Importance:       0.5,
			PrivacyLevel:     core.PrivacyPrivate,
			Status:           core.MemoryStatusActive,
			Metadata: map[string]string{
				MetaEntitiesExtracted: "true",
			},
			CreatedAt: now.Add(time.Duration(i) * time.Second),
			UpdatedAt: now.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatal(err)
		}
	}

	var analyzeCalls int32
	svc.SetIntelligenceProvider(enrichIdleIntelligenceStub{analyzeCalls: &analyzeCalls, isLLM: true})

	enriched, err := svc.EnrichMemories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if enriched != 0 {
		t.Fatalf("expected 0 memories enriched, got %d", enriched)
	}
	if atomic.LoadInt32(&analyzeCalls) != 0 {
		t.Fatalf("expected 0 AnalyzeEvents calls, got %d", atomic.LoadInt32(&analyzeCalls))
	}
}

type enrichIdleIntelligenceStub struct {
	analyzeCalls *int32
	isLLM        bool
}

func (enrichIdleIntelligenceStub) Summarize(context.Context, string, int) (string, error) {
	return "", nil
}
func (enrichIdleIntelligenceStub) ExtractMemoryCandidate(context.Context, string) ([]core.MemoryCandidate, error) {
	return nil, nil
}
func (enrichIdleIntelligenceStub) ExtractMemoryCandidateBatch(context.Context, []string) ([]core.MemoryCandidate, error) {
	return nil, nil
}
func (s enrichIdleIntelligenceStub) IsLLMBacked() bool {
	return s.isLLM
}
func (enrichIdleIntelligenceStub) ModelName() string {
	return ""
}
func (s enrichIdleIntelligenceStub) AnalyzeEvents(_ context.Context, _ []core.EventContent) (*core.AnalysisResult, error) {
	atomic.AddInt32(s.analyzeCalls, 1)
	return &core.AnalysisResult{}, nil
}
func (enrichIdleIntelligenceStub) TriageEvents(_ context.Context, events []core.EventContent) (map[int]core.TriageDecision, error) {
	return map[int]core.TriageDecision{}, nil
}
func (enrichIdleIntelligenceStub) ReviewMemories(context.Context, []core.MemoryReview) (*core.ReviewResult, error) {
	return &core.ReviewResult{}, nil
}
func (enrichIdleIntelligenceStub) CompressEventBatches(context.Context, []core.EventChunk) ([]core.CompressionResult, error) {
	return nil, nil
}
func (enrichIdleIntelligenceStub) SummarizeTopicBatches(context.Context, []core.TopicChunk) ([]core.CompressionResult, error) {
	return nil, nil
}
func (enrichIdleIntelligenceStub) ConsolidateNarrative(context.Context, []core.EventContent, []core.MemorySummary) (*core.NarrativeResult, error) {
	return &core.NarrativeResult{}, nil
}

func TestIdle_CompressHistory_LegacyJobFallsBackToFinishedAt(t *testing.T) {
	svc, repo := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{})
	ctx := context.Background()

	past := time.Now().UTC().Add(-1 * time.Hour)
	for i := 0; i < 3; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("legacy compress event %d", i),
			OccurredAt:   past.Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	legacyFinished := time.Now().UTC()
	if err := repo.InsertJob(ctx, &core.Job{
		ID:         core.GenerateID("job_"),
		Kind:       "compress",
		Status:     "completed",
		StartedAt:  &legacyFinished,
		FinishedAt: &legacyFinished,
		Result:     map[string]string{"created": "3"},
		CreatedAt:  legacyFinished,
	}); err != nil {
		t.Fatal(err)
	}

	var compressBatchCalls int32
	svc.SetIntelligenceProvider(consolidateTestIntelligence{compressBatchCallCountPtr: &compressBatchCalls})

	created, err := svc.CompressHistory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("expected 0 summaries created with legacy fallback, got %d", created)
	}
	if atomic.LoadInt32(&compressBatchCalls) != 0 {
		t.Fatalf("expected 0 compress calls with legacy fallback, got %d", atomic.LoadInt32(&compressBatchCalls))
	}
}

func TestIdle_PolicyPrecedence_SourceOverridesKind(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repo.InsertIngestionPolicy(ctx, &core.IngestionPolicy{
		ID:          "pol_kind_ignore",
		PatternType: "kind",
		Pattern:     "tool_result",
		Mode:        "ignore",
		MatchMode:   "exact",
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}

	if err := repo.InsertIngestionPolicy(ctx, &core.IngestionPolicy{
		ID:          "pol_source_full",
		PatternType: "source",
		Pattern:     "important-source",
		Mode:        "full",
		MatchMode:   "exact",
		CreatedAt:   now,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatal(err)
	}

	evt, err := svc.IngestEvent(ctx, &core.Event{
		Kind:         "tool_result",
		SourceSystem: "important-source",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "this tool result should be stored because source overrides kind",
		OccurredAt:   now,
	})
	if err != nil {
		t.Fatal(err)
	}

	events, err := repo.ListEvents(ctx, core.ListEventsOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range events {
		if e.ID == evt.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected tool_result event to be stored because source=full policy overrides kind=ignore")
	}
}
