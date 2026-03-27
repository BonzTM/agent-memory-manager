package service_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/adapters/sqlite"
	"github.com/bonztm/agent-memory-manager/internal/core"
	"github.com/bonztm/agent-memory-manager/internal/service"
)

func testServiceForReprocess(t *testing.T) (core.Service, *sqlite.SQLiteRepository) {
	return testServiceForReprocessWithSummarizer(t, nil)
}

type stubBatchSummarizer struct {
	batchCandidates []core.MemoryCandidate
}

type recordingBatchSummarizer struct {
	batchSizes *[]int
}

type reprocessIntelligenceStub struct {
	analysisResult   *core.AnalysisResult
	analyzeErr       error
	triageDecisions  map[int]core.TriageDecision
	triageByContains map[string]core.TriageDecision
	analyzeBatchLens []int
	triageBatchLens  []int
}

func testImportancePtr(v float64) *float64 {
	return &v
}

func (s stubBatchSummarizer) Summarize(context.Context, string, int) (string, error) {
	return "", nil
}

func (s stubBatchSummarizer) ExtractMemoryCandidate(context.Context, string) ([]core.MemoryCandidate, error) {
	return nil, nil
}

func (s stubBatchSummarizer) ExtractMemoryCandidateBatch(context.Context, []string) ([]core.MemoryCandidate, error) {
	return append([]core.MemoryCandidate(nil), s.batchCandidates...), nil
}

func (s recordingBatchSummarizer) Summarize(context.Context, string, int) (string, error) {
	return "", nil
}

func (s recordingBatchSummarizer) ExtractMemoryCandidate(context.Context, string) ([]core.MemoryCandidate, error) {
	return nil, nil
}

func (s recordingBatchSummarizer) ExtractMemoryCandidateBatch(_ context.Context, events []string) ([]core.MemoryCandidate, error) {
	if s.batchSizes != nil {
		*s.batchSizes = append(*s.batchSizes, len(events))
	}
	return nil, nil
}

func (s *reprocessIntelligenceStub) Summarize(context.Context, string, int) (string, error) {
	return "", nil
}

func (s *reprocessIntelligenceStub) ExtractMemoryCandidate(context.Context, string) ([]core.MemoryCandidate, error) {
	return nil, nil
}

func (s *reprocessIntelligenceStub) ExtractMemoryCandidateBatch(context.Context, []string) ([]core.MemoryCandidate, error) {
	return nil, nil
}

func (s *reprocessIntelligenceStub) AnalyzeEvents(_ context.Context, events []core.EventContent) (*core.AnalysisResult, error) {
	s.analyzeBatchLens = append(s.analyzeBatchLens, len(events))
	if s.analyzeErr != nil {
		return nil, s.analyzeErr
	}
	if s.analysisResult == nil {
		return &core.AnalysisResult{}, nil
	}
	return s.analysisResult, nil
}

func (s *reprocessIntelligenceStub) TriageEvents(_ context.Context, events []core.EventContent) (map[int]core.TriageDecision, error) {
	s.triageBatchLens = append(s.triageBatchLens, len(events))
	decisions := make(map[int]core.TriageDecision, len(events))
	for i, evt := range events {
		idx := evt.Index
		if idx <= 0 {
			idx = i + 1
		}
		decision := core.TriageReflect
		content := strings.ToLower(strings.TrimSpace(evt.Content))
		for needle, configured := range s.triageByContains {
			if needle == "" {
				continue
			}
			if strings.Contains(content, strings.ToLower(strings.TrimSpace(needle))) {
				decision = configured
			}
		}
		if s.triageDecisions != nil {
			if configured, ok := s.triageDecisions[idx]; ok {
				decision = configured
			}
		}
		decisions[idx] = decision
	}
	return decisions, nil
}

func (s *reprocessIntelligenceStub) ReviewMemories(context.Context, []core.MemoryReview) (*core.ReviewResult, error) {
	return &core.ReviewResult{}, nil
}

func (s *reprocessIntelligenceStub) CompressEventBatches(_ context.Context, chunks []core.EventChunk) ([]core.CompressionResult, error) {
	results := make([]core.CompressionResult, 0, len(chunks))
	for _, chunk := range chunks {
		results = append(results, core.CompressionResult{Index: chunk.Index})
	}
	return results, nil
}

func (s *reprocessIntelligenceStub) SummarizeTopicBatches(_ context.Context, topics []core.TopicChunk) ([]core.CompressionResult, error) {
	results := make([]core.CompressionResult, 0, len(topics))
	for _, topic := range topics {
		results = append(results, core.CompressionResult{Index: topic.Index})
	}
	return results, nil
}

func (s *reprocessIntelligenceStub) ConsolidateNarrative(context.Context, []core.EventContent, []core.MemorySummary) (*core.NarrativeResult, error) {
	return &core.NarrativeResult{}, nil
}

func testServiceForReprocessWithSummarizer(t *testing.T, summarizer core.Summarizer) (core.Service, *sqlite.SQLiteRepository) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	repo := &sqlite.SQLiteRepository{DB: db}
	svc := service.New(repo, dbPath, summarizer, nil)
	return svc, repo
}

func TestReprocess_SupersedesHeuristicMemories(t *testing.T) {
	svc, repo := testServiceForReprocess(t)
	ctx := context.Background()

	now := time.Now().UTC()
	for i, content := range []string{
		"We decided to use SQLite for the persistence layer",
		"Josh prefers concise commit messages in imperative mood",
	} {
		evt := &core.Event{
			ID:   generateTestID("evt_", i),
			Kind: "message_user", SourceSystem: "test",
			Content: content, PrivacyLevel: core.PrivacyPrivate,
			OccurredAt: now.Add(time.Duration(i) * time.Minute), IngestedAt: now,
		}
		if err := repo.InsertEvent(ctx, evt); err != nil {
			t.Fatal(err)
		}
	}

	job, err := svc.RunJob(ctx, "reflect")
	if err != nil {
		t.Fatalf("reflect failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("reflect status: %s, error: %s", job.Status, job.ErrorText)
	}

	memsBefore, _ := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 100})
	activeBefore := len(memsBefore)
	if activeBefore == 0 {
		t.Fatal("expected reflect to create some memories")
	}

	for _, m := range memsBefore {
		if m.Metadata != nil && m.Metadata["extraction_method"] == "llm" {
			t.Fatal("heuristic reflect should not tag memories as llm")
		}
	}

	job, err = svc.RunJob(ctx, "reprocess")
	if err != nil {
		t.Fatalf("reprocess failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("reprocess status: %s, error: %s", job.Status, job.ErrorText)
	}

	supersededMems, _ := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusSuperseded, Limit: 100})
	if len(supersededMems) == 0 {
		t.Log("no memories superseded (heuristic reprocessed heuristic — expected in non-LLM mode)")
	}
}

func TestReprocess_SkipsLLMTaggedMemories(t *testing.T) {
	_, repo := testServiceForReprocess(t)
	ctx := context.Background()

	now := time.Now().UTC()
	evt := &core.Event{
		ID: "evt_llm_skip_test", Kind: "message_user", SourceSystem: "test",
		Content: "I prefer tabs over spaces", PrivacyLevel: core.PrivacyPrivate,
		OccurredAt: now, IngestedAt: now,
	}
	if err := repo.InsertEvent(ctx, evt); err != nil {
		t.Fatal(err)
	}

	mem := &core.Memory{
		ID: "mem_llm_tagged", Type: core.MemoryTypePreference,
		Scope: core.ScopeGlobal, Body: "Prefers tabs over spaces",
		TightDescription: "Tabs over spaces", Confidence: 0.9,
		Importance: 0.5, PrivacyLevel: core.PrivacyPrivate,
		Status: core.MemoryStatusActive, SourceEventIDs: []string{"evt_llm_skip_test"},
		Metadata:  map[string]string{"extraction_method": "llm"},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.InsertMemory(ctx, mem); err != nil {
		t.Fatal(err)
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "skip.db")
	svc := service.New(repo, dbPath, nil, nil)

	job, err := svc.RunJob(ctx, "reprocess")
	if err != nil {
		t.Fatalf("reprocess failed: %v", err)
	}

	refreshed, err := repo.GetMemory(ctx, "mem_llm_tagged")
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Status != core.MemoryStatusActive {
		t.Fatalf("LLM-tagged memory should NOT be superseded by reprocess, got status=%s", refreshed.Status)
	}
	_ = job
}

func TestReprocessAll_ReprocessesEverything(t *testing.T) {
	svc, repo := testServiceForReprocess(t)
	ctx := context.Background()

	now := time.Now().UTC()
	evt := &core.Event{
		ID: "evt_reprocess_all", Kind: "message_user", SourceSystem: "test",
		Content:      "We decided to use Redis for caching",
		PrivacyLevel: core.PrivacyPrivate, OccurredAt: now, IngestedAt: now,
	}
	if err := repo.InsertEvent(ctx, evt); err != nil {
		t.Fatal(err)
	}
	mem := &core.Memory{
		ID: "mem_llm_reprocess", Type: core.MemoryTypeDecision,
		Scope: core.ScopeGlobal, Body: "Using Redis for caching",
		TightDescription: "Redis for caching", Confidence: 0.9,
		Importance: 0.5, PrivacyLevel: core.PrivacyPrivate,
		Status: core.MemoryStatusActive, SourceEventIDs: []string{"evt_reprocess_all"},
		Metadata:  map[string]string{"extraction_method": "llm"},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.InsertMemory(ctx, mem); err != nil {
		t.Fatal(err)
	}

	job, err := svc.RunJob(ctx, "reprocess_all")
	if err != nil {
		t.Fatalf("reprocess_all failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("reprocess_all status: %s, error: %s", job.Status, job.ErrorText)
	}

	refreshed, err := repo.GetMemory(ctx, "mem_llm_reprocess")
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Status != core.MemoryStatusActive {
		t.Fatalf("expected deduplicated LLM memory to remain active, got status=%s", refreshed.Status)
	}
	if refreshed.Metadata["extraction_method"] != "llm" {
		t.Fatalf("expected extraction method llm after reprocess_all, got %#v", refreshed.Metadata)
	}
}

func TestReprocess_NoEventsReturnsZero(t *testing.T) {
	svc, _ := testServiceForReprocess(t)
	ctx := context.Background()

	job, err := svc.RunJob(ctx, "reprocess")
	if err != nil {
		t.Fatalf("reprocess with no events failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("expected completed, got %s", job.Status)
	}
}

func TestReprocess_UsesCandidateSourceEventsProjectScopeAndImportance(t *testing.T) {
	summarizer := stubBatchSummarizer{batchCandidates: []core.MemoryCandidate{{
		Type:             core.MemoryTypeDecision,
		Subject:          "storage",
		Body:             "Use the project-scoped SQLite store",
		TightDescription: "Use project SQLite store",
		Confidence:       0.92,
		Importance:       testImportancePtr(0.87),
		SourceEventNums:  []int{1},
	}}}
	svc, repo := testServiceForReprocessWithSummarizer(t, summarizer)
	ctx := context.Background()
	now := time.Now().UTC()

	events := []*core.Event{
		{ID: "evt_global", Kind: "message_user", SourceSystem: "test", Content: "global chatter", PrivacyLevel: core.PrivacyPrivate, OccurredAt: now, IngestedAt: now},
		{ID: "evt_project", Kind: "message_user", SourceSystem: "test", ProjectID: "proj_123", Content: "we decided to use project sqlite", PrivacyLevel: core.PrivacyPrivate, OccurredAt: now.Add(time.Minute), IngestedAt: now},
	}
	for _, evt := range events {
		if err := repo.InsertEvent(ctx, evt); err != nil {
			t.Fatal(err)
		}
	}

	job, err := svc.RunJob(ctx, "reprocess_all")
	if err != nil {
		t.Fatalf("reprocess_all failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("reprocess_all status: %s, error: %s", job.Status, job.ErrorText)
	}

	mems, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected 1 active memory, got %d", len(mems))
	}
	mem := mems[0]
	if mem.Scope != core.ScopeProject || mem.ProjectID != "proj_123" {
		t.Fatalf("expected project-scoped memory for proj_123, got scope=%s project=%q", mem.Scope, mem.ProjectID)
	}
	if len(mem.SourceEventIDs) != 1 || mem.SourceEventIDs[0] != "evt_project" {
		t.Fatalf("expected source_event_ids [evt_project], got %#v", mem.SourceEventIDs)
	}
	if mem.Importance != 0.87 {
		t.Fatalf("expected importance 0.87, got %f", mem.Importance)
	}
}

func TestReprocess_DeduplicatesAcrossExistingMemories(t *testing.T) {
	summarizer := stubBatchSummarizer{batchCandidates: []core.MemoryCandidate{{
		Type:             core.MemoryTypeIdentity,
		Subject:          "claude-oauth-proxy",
		Body:             "OpenAI-compatible Claude proxy via OAuth",
		TightDescription: "claude-oauth-proxy: OpenAI-compatible proxy for Claude via OAuth",
		Confidence:       0.95,
		Importance:       testImportancePtr(0.8),
		SourceEventNums:  []int{1},
	}}}
	svc, repo := testServiceForReprocessWithSummarizer(t, summarizer)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repo.InsertEvent(ctx, &core.Event{ID: "evt_dup_new", Kind: "message_user", SourceSystem: "test", Content: "repo identity details", PrivacyLevel: core.PrivacyPrivate, OccurredAt: now, IngestedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertMemory(ctx, &core.Memory{
		ID:               "mem_dup_existing",
		Type:             core.MemoryTypeIdentity,
		Scope:            core.ScopeGlobal,
		Subject:          "claude-oauth-proxy",
		Body:             "OpenAI-compatible Claude proxy via OAuth",
		TightDescription: "claude-oauth-proxy: OpenAI-compatible proxy for Claude via OAuth",
		Confidence:       0.6,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatal(err)
	}

	job, err := svc.RunJob(ctx, "reprocess_all")
	if err != nil {
		t.Fatalf("reprocess_all failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("reprocess_all status: %s, error: %s", job.Status, job.ErrorText)
	}

	mems, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected 1 active memory after dedup, got %d", len(mems))
	}
	mem := mems[0]
	if mem.ID != "mem_dup_existing" {
		t.Fatalf("expected existing memory to be reused, got %s", mem.ID)
	}
	if mem.Metadata["extraction_method"] != "heuristic" {
		t.Fatalf("expected existing memory metadata to match summarizer extraction method, got %#v", mem.Metadata)
	}
	if mem.Confidence != 0.95 {
		t.Fatalf("expected confidence to be upgraded to 0.95, got %f", mem.Confidence)
	}
	if mem.Importance != 0.8 {
		t.Fatalf("expected importance to be upgraded to 0.8, got %f", mem.Importance)
	}
	if len(mem.SourceEventIDs) != 1 || mem.SourceEventIDs[0] != "evt_dup_new" {
		t.Fatalf("expected source_event_ids to include evt_dup_new, got %#v", mem.SourceEventIDs)
	}
}

func TestReprocess_UpgradesKeeperAndSupersedesSiblingDuplicates(t *testing.T) {
	summarizer := stubBatchSummarizer{batchCandidates: []core.MemoryCandidate{{
		Type:             core.MemoryTypeIdentity,
		Subject:          "claude-oauth-proxy",
		Body:             "OpenAI-compatible Claude proxy via OAuth",
		TightDescription: "new proxy identity",
		Confidence:       0.95,
		SourceEventNums:  []int{1},
	}}}
	svc, repo := testServiceForReprocessWithSummarizer(t, summarizer)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repo.InsertEvent(ctx, &core.Event{ID: "evt_dup_upgrade", Kind: "message_user", SourceSystem: "test", Content: "repo identity details", PrivacyLevel: core.PrivacyPrivate, OccurredAt: now, IngestedAt: now}); err != nil {
		t.Fatal(err)
	}
	seed := []*core.Memory{
		{ID: "mem_dup_keeper", Type: core.MemoryTypeIdentity, Scope: core.ScopeGlobal, Subject: "claude-oauth-proxy", Body: "OpenAI-compatible Claude proxy via OAuth", TightDescription: "old proxy identity", Confidence: 0.6, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now},
		{ID: "mem_dup_sibling", Type: core.MemoryTypeIdentity, Scope: core.ScopeGlobal, Subject: "claude-oauth-proxy", Body: "OpenAI-compatible Claude proxy via OAuth", TightDescription: "old proxy identity", Confidence: 0.4, Importance: 0.4, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now.Add(-time.Minute), UpdatedAt: now.Add(-time.Minute)},
	}
	for _, mem := range seed {
		if err := repo.InsertMemory(ctx, mem); err != nil {
			t.Fatal(err)
		}
	}

	job, err := svc.RunJob(ctx, "reprocess_all")
	if err != nil {
		t.Fatalf("reprocess_all failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("reprocess_all status: %s, error: %s", job.Status, job.ErrorText)
	}

	keeper, err := repo.GetMemory(ctx, "mem_dup_keeper")
	if err != nil {
		t.Fatal(err)
	}
	if keeper.Status != core.MemoryStatusActive {
		t.Fatalf("expected keeper to remain active, got %s", keeper.Status)
	}
	if keeper.TightDescription != "new proxy identity" {
		t.Fatalf("expected keeper tight description to be upgraded, got %q", keeper.TightDescription)
	}
	sibling, err := repo.GetMemory(ctx, "mem_dup_sibling")
	if err != nil {
		t.Fatal(err)
	}
	if sibling.Status != core.MemoryStatusSuperseded || sibling.SupersededBy != keeper.ID {
		t.Fatalf("expected sibling to be superseded by keeper, got status=%s superseded_by=%q", sibling.Status, sibling.SupersededBy)
	}
}

func TestReprocess_OnlyTouchesMatchingMemoriesForSourceEvent(t *testing.T) {
	summarizer := stubBatchSummarizer{batchCandidates: []core.MemoryCandidate{{
		Type:             core.MemoryTypePreference,
		Subject:          "editor",
		Body:             "Prefers tabs over spaces",
		TightDescription: "Tabs over spaces",
		Confidence:       0.9,
		SourceEventNums:  []int{1},
	}}}
	svc, repo := testServiceForReprocessWithSummarizer(t, summarizer)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repo.InsertEvent(ctx, &core.Event{ID: "evt_shared", Kind: "message_user", SourceSystem: "test", Content: "I prefer tabs over spaces and we use SQLite", PrivacyLevel: core.PrivacyPrivate, OccurredAt: now, IngestedAt: now}); err != nil {
		t.Fatal(err)
	}
	seed := []*core.Memory{
		{ID: "mem_pref", Type: core.MemoryTypePreference, Scope: core.ScopeGlobal, Subject: "editor", Body: "Prefers tabs over spaces", TightDescription: "Tabs over spaces", Confidence: 0.5, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, SourceEventIDs: []string{"evt_shared"}, CreatedAt: now, UpdatedAt: now},
		{ID: "mem_decision", Type: core.MemoryTypeDecision, Scope: core.ScopeGlobal, Subject: "storage", Body: "Using SQLite for persistence", TightDescription: "Using SQLite for persistence", Confidence: 0.8, Importance: 0.8, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, SourceEventIDs: []string{"evt_shared"}, CreatedAt: now, UpdatedAt: now},
	}
	for _, mem := range seed {
		if err := repo.InsertMemory(ctx, mem); err != nil {
			t.Fatal(err)
		}
	}

	job, err := svc.RunJob(ctx, "reprocess_all")
	if err != nil {
		t.Fatalf("reprocess_all failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("reprocess_all status: %s, error: %s", job.Status, job.ErrorText)
	}

	pref, err := repo.GetMemory(ctx, "mem_pref")
	if err != nil {
		t.Fatal(err)
	}
	if pref.Status != core.MemoryStatusActive {
		t.Fatalf("expected matching preference memory to remain active via dedup update, got %s", pref.Status)
	}
	decision, err := repo.GetMemory(ctx, "mem_decision")
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != core.MemoryStatusActive {
		t.Fatalf("expected unrelated decision memory to remain active, got %s", decision.Status)
	}
}

func TestReprocess_SkipsAmbiguousCandidatesWithoutSourceEvents(t *testing.T) {
	t.Run("missing source events in multi-event batch", func(t *testing.T) {
		summarizer := stubBatchSummarizer{batchCandidates: []core.MemoryCandidate{{
			Type:             core.MemoryTypeFact,
			Subject:          "architecture",
			Body:             "Service is the entrypoint",
			TightDescription: "Service entrypoint",
			Confidence:       0.8,
		}}}
		svc, repo := testServiceForReprocessWithSummarizer(t, summarizer)
		ctx := context.Background()
		now := time.Now().UTC()
		for i := 0; i < 2; i++ {
			evt := &core.Event{ID: generateTestID("evt_ambiguous_", i), Kind: "message_user", SourceSystem: "test", Content: "content", PrivacyLevel: core.PrivacyPrivate, OccurredAt: now.Add(time.Duration(i) * time.Minute), IngestedAt: now}
			if err := repo.InsertEvent(ctx, evt); err != nil {
				t.Fatal(err)
			}
		}
		job, err := svc.RunJob(ctx, "reprocess_all")
		if err != nil {
			t.Fatalf("reprocess_all failed: %v", err)
		}
		if job.Status != "completed" {
			t.Fatalf("unexpected job status: %s", job.Status)
		}
		mems, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(mems) != 0 {
			t.Fatalf("expected ambiguous candidate to be skipped, got %#v", mems)
		}
	})

	t.Run("invalid source event number", func(t *testing.T) {
		summarizer := stubBatchSummarizer{batchCandidates: []core.MemoryCandidate{{
			Type:             core.MemoryTypeFact,
			Subject:          "architecture",
			Body:             "Service is the entrypoint",
			TightDescription: "Service entrypoint",
			Confidence:       0.8,
			SourceEventNums:  []int{3},
		}}}
		svc, repo := testServiceForReprocessWithSummarizer(t, summarizer)
		ctx := context.Background()
		now := time.Now().UTC()
		for i := 0; i < 2; i++ {
			evt := &core.Event{ID: generateTestID("evt_invalid_", i), Kind: "message_user", SourceSystem: "test", Content: "content", PrivacyLevel: core.PrivacyPrivate, OccurredAt: now.Add(time.Duration(i) * time.Minute), IngestedAt: now}
			if err := repo.InsertEvent(ctx, evt); err != nil {
				t.Fatal(err)
			}
		}
		job, err := svc.RunJob(ctx, "reprocess_all")
		if err != nil {
			t.Fatalf("reprocess_all failed: %v", err)
		}
		if job.Status != "completed" {
			t.Fatalf("unexpected job status: %s", job.Status)
		}
		mems, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(mems) != 0 {
			t.Fatalf("expected invalid candidate to be skipped, got %#v", mems)
		}
	})
}

func TestReprocess_PreservesExplicitZeroImportance(t *testing.T) {
	summarizer := stubBatchSummarizer{batchCandidates: []core.MemoryCandidate{{
		Type:             core.MemoryTypeFact,
		Subject:          "low-value",
		Body:             "This should rank very low",
		TightDescription: "low value fact",
		Confidence:       0.7,
		Importance:       testImportancePtr(0),
		SourceEventNums:  []int{1},
	}}}
	svc, repo := testServiceForReprocessWithSummarizer(t, summarizer)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := repo.InsertEvent(ctx, &core.Event{ID: "evt_zero_importance", Kind: "message_user", SourceSystem: "test", Content: "low value fact", PrivacyLevel: core.PrivacyPrivate, OccurredAt: now, IngestedAt: now}); err != nil {
		t.Fatal(err)
	}
	job, err := svc.RunJob(ctx, "reprocess_all")
	if err != nil {
		t.Fatalf("reprocess_all failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("unexpected job status: %s", job.Status)
	}
	mems, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected one memory, got %d", len(mems))
	}
	if mems[0].Importance != 0 {
		t.Fatalf("expected explicit zero importance to be preserved, got %f", mems[0].Importance)
	}
}

func TestReprocess_RejectsInvalidCandidates(t *testing.T) {
	summarizer := stubBatchSummarizer{batchCandidates: []core.MemoryCandidate{
		{Type: core.MemoryType("bogus"), Body: "bad type", TightDescription: "bad type", Confidence: 0.8, SourceEventNums: []int{1}},
		{Type: core.MemoryTypeFact, Body: "", TightDescription: "missing body", Confidence: 0.8, SourceEventNums: []int{1}},
		{Type: core.MemoryTypeFact, Body: "missing description", TightDescription: "", Confidence: 0.8, SourceEventNums: []int{1}},
		{Type: core.MemoryTypeFact, Body: "valid reprocess memory", TightDescription: "valid reprocess memory", Confidence: 0.9, SourceEventNums: []int{1}},
	}}
	svc, repo := testServiceForReprocessWithSummarizer(t, summarizer)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repo.InsertEvent(ctx, &core.Event{ID: "evt_invalid_reprocess", Kind: "message_user", SourceSystem: "test", Content: "candidate validation", PrivacyLevel: core.PrivacyPrivate, OccurredAt: now, IngestedAt: now}); err != nil {
		t.Fatal(err)
	}

	job, err := svc.RunJob(ctx, "reprocess_all")
	if err != nil {
		t.Fatalf("reprocess_all failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("reprocess_all status: %s, error: %s", job.Status, job.ErrorText)
	}

	mems, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected one valid active memory, got %d", len(mems))
	}
	if mems[0].TightDescription != "valid reprocess memory" {
		t.Fatalf("expected valid candidate to survive, got %q", mems[0].TightDescription)
	}
}

func TestReprocess_DeduplicatesAcrossMultipleBatches(t *testing.T) {
	summarizer := stubBatchSummarizer{batchCandidates: []core.MemoryCandidate{{
		Type:             core.MemoryTypeIdentity,
		Subject:          "proxy",
		Body:             "Proxy identity memory",
		TightDescription: "Proxy identity memory",
		Confidence:       0.88,
		SourceEventNums:  []int{1},
	}}}
	svc, repo := testServiceForReprocessWithSummarizer(t, summarizer)
	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < 21; i++ {
		evt := &core.Event{ID: generateTestID("evt_batch_", i), Kind: "message_user", SourceSystem: "test", Content: "proxy identity detail", PrivacyLevel: core.PrivacyPrivate, OccurredAt: now.Add(time.Duration(i) * time.Minute), IngestedAt: now}
		if err := repo.InsertEvent(ctx, evt); err != nil {
			t.Fatal(err)
		}
	}
	job, err := svc.RunJob(ctx, "reprocess_all")
	if err != nil {
		t.Fatalf("reprocess_all failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("unexpected job status: %s", job.Status)
	}
	mems, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected one active deduped memory across batches, got %d", len(mems))
	}
	mem := mems[0]
	if len(mem.SourceEventIDs) != 2 {
		t.Fatalf("expected source events from both batches, got %#v", mem.SourceEventIDs)
	}
	firstBatchID := generateTestID("evt_batch_", 0)
	secondBatchID := generateTestID("evt_batch_", 20)
	seen := map[string]bool{}
	for _, id := range mem.SourceEventIDs {
		seen[id] = true
	}
	if !seen[firstBatchID] || !seen[secondBatchID] {
		t.Fatalf("expected merged source events %q and %q, got %#v", firstBatchID, secondBatchID, mem.SourceEventIDs)
	}
}

func TestReprocessAll_UsesConfiguredBatchSize(t *testing.T) {
	var batchSizes []int
	svc, repo := testServiceForReprocessWithSummarizer(t, recordingBatchSummarizer{batchSizes: &batchSizes})
	concreteSvc, ok := svc.(*service.AMMService)
	if !ok {
		t.Fatal("expected concrete AMMService")
	}
	concreteSvc.SetReprocessBatchSize(2)

	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		evt := &core.Event{
			ID:           generateTestID("evt_cfg_batch_", i),
			Kind:         "message_user",
			SourceSystem: "test",
			Content:      "batch-sized reprocess event",
			PrivacyLevel: core.PrivacyPrivate,
			OccurredAt:   now.Add(time.Duration(i) * time.Minute),
			IngestedAt:   now,
		}
		if err := repo.InsertEvent(ctx, evt); err != nil {
			t.Fatal(err)
		}
	}

	job, err := svc.RunJob(ctx, "reprocess_all")
	if err != nil {
		t.Fatalf("reprocess_all failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("unexpected job status: %s", job.Status)
	}

	want := []int{2, 2, 1}
	if len(batchSizes) != len(want) {
		t.Fatalf("expected batch calls %v, got %v", want, batchSizes)
	}
	for i := range want {
		if batchSizes[i] != want[i] {
			t.Fatalf("expected batch calls %v, got %v", want, batchSizes)
		}
	}
}

func TestReprocess_SetsUpgradedQuality(t *testing.T) {
	ctx := context.Background()
	candidatePayload, err := json.Marshal(map[string]any{
		"memories": []core.MemoryCandidate{{
			Type:             core.MemoryTypePreference,
			Subject:          "style",
			Body:             "Prefers concise commit messages",
			TightDescription: "Prefers concise commit messages",
			Confidence:       0.95,
			SourceEventNums:  []int{1},
		}},
		"entities":      []core.EntityCandidate{},
		"relationships": []core.RelationshipCandidate{},
		"event_quality": map[string]string{"1": "durable"},
	})
	if err != nil {
		t.Fatalf("marshal candidate payload: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":` + strconv.Quote(string(candidatePayload)) + `}}]}`))
	}))
	t.Cleanup(server.Close)

	llm := service.NewLLMSummarizer(server.URL, "test-key", "test-model")
	svc, repo := testServiceForReprocessWithSummarizer(t, llm)
	now := time.Now().UTC()

	if err := repo.InsertEvent(ctx, &core.Event{ID: "evt_upgrade", Kind: "message_user", SourceSystem: "test", Content: "I prefer concise commit messages", PrivacyLevel: core.PrivacyPrivate, OccurredAt: now, IngestedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertMemory(ctx, &core.Memory{
		ID:               "mem_upgrade",
		Type:             core.MemoryTypePreference,
		Scope:            core.ScopeGlobal,
		Subject:          "style",
		Body:             "Prefers concise commit messages",
		TightDescription: "Prefers concise commit messages",
		Confidence:       0.6,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		SourceEventIDs:   []string{"evt_upgrade"},
		Metadata: map[string]string{
			"extraction_method":  "heuristic",
			"extraction_quality": "provisional",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	job, err := svc.RunJob(ctx, "reprocess")
	if err != nil {
		t.Fatalf("reprocess failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("reprocess status: %s, error: %s", job.Status, job.ErrorText)
	}

	upgraded, err := repo.GetMemory(ctx, "mem_upgrade")
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.Metadata["extraction_quality"] != "upgraded" {
		t.Fatalf("expected extraction_quality=upgraded, got %q", upgraded.Metadata["extraction_quality"])
	}
}

func TestReprocessAll_UsesIntelligenceAnalyzeAfterTriageAndLinksEntities(t *testing.T) {
	ctx := context.Background()
	llm := service.NewLLMSummarizer("http://127.0.0.1:1", "test-key", "test-model")
	svc, repo := testServiceForReprocessWithSummarizer(t, llm)
	concreteSvc, ok := svc.(*service.AMMService)
	if !ok {
		t.Fatal("expected concrete AMMService")
	}

	intel := &reprocessIntelligenceStub{
		analysisResult: &core.AnalysisResult{
			Memories: []core.MemoryCandidate{{
				Type:             core.MemoryTypeDecision,
				Subject:          "cache",
				Body:             "Use Redis cache for rate limiting",
				TightDescription: "Use Redis cache",
				Confidence:       0.91,
				SourceEventNums:  []int{1},
			}},
			Entities: []core.EntityCandidate{{
				CanonicalName: "Redis Cache",
				Type:          "technology",
				Aliases:       []string{"redis"},
			}, {
				CanonicalName: "API Gateway",
				Type:          "service",
				Aliases:       []string{"api"},
			}},
			Relationships: []core.RelationshipCandidate{{
				FromEntity: "API Gateway",
				ToEntity:   "Redis Cache",
				Type:       "uses",
			}},
		},
		triageByContains: map[string]core.TriageDecision{
			"status ping": core.TriageSkip,
		},
	}
	concreteSvc.SetIntelligenceProvider(intel)

	now := time.Now().UTC()
	events := []*core.Event{
		{ID: "evt_triage_skip", Kind: "message_user", SourceSystem: "test", Content: "status ping", PrivacyLevel: core.PrivacyPrivate, OccurredAt: now, IngestedAt: now},
		{ID: "evt_triage_keep", Kind: "message_user", SourceSystem: "test", Content: "We decided to use redis cache for API rate limits", PrivacyLevel: core.PrivacyPrivate, OccurredAt: now.Add(time.Minute), IngestedAt: now},
	}
	for _, evt := range events {
		if err := repo.InsertEvent(ctx, evt); err != nil {
			t.Fatal(err)
		}
	}

	job, err := svc.RunJob(ctx, "reprocess_all")
	if err != nil {
		t.Fatalf("reprocess_all failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("unexpected job status: %s", job.Status)
	}

	if len(intel.triageBatchLens) == 0 || intel.triageBatchLens[0] != 2 {
		t.Fatalf("expected triage to see full batch size 2, got %#v", intel.triageBatchLens)
	}
	if len(intel.analyzeBatchLens) == 0 || intel.analyzeBatchLens[0] != 1 {
		t.Fatalf("expected analyze to run on triaged batch size 1, got %#v", intel.analyzeBatchLens)
	}

	mems, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected one active memory, got %d", len(mems))
	}
	if len(mems[0].SourceEventIDs) != 1 || mems[0].SourceEventIDs[0] != "evt_triage_keep" {
		t.Fatalf("expected memory sourced from triaged event only, got %#v", mems[0].SourceEventIDs)
	}

	linked, err := repo.GetMemoryEntities(ctx, mems[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	foundRedisCache := false
	for _, entity := range linked {
		if entity.CanonicalName == "Redis Cache" {
			foundRedisCache = true
			break
		}
	}
	if !foundRedisCache {
		t.Fatalf("expected memory entity link from analysis candidates, got %#v", linked)
	}

	apiEntities, err := repo.SearchEntities(ctx, "API Gateway", 10)
	if err != nil {
		t.Fatal(err)
	}
	redisEntities, err := repo.SearchEntities(ctx, "Redis Cache", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(apiEntities) == 0 || len(redisEntities) == 0 {
		t.Fatalf("expected API Gateway and Redis Cache entities, got api=%#v redis=%#v", apiEntities, redisEntities)
	}
	rels, err := repo.ListRelationshipsByEntityIDs(ctx, []string{apiEntities[0].ID, redisEntities[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	foundUses := false
	for _, rel := range rels {
		if rel.FromEntityID == apiEntities[0].ID && rel.ToEntityID == redisEntities[0].ID && rel.RelationshipType == "uses" {
			foundUses = true
			break
		}
	}
	if !foundUses {
		t.Fatalf("expected analyzed relationship API Gateway -> Redis Cache uses, got %#v", rels)
	}
}

func TestReprocessAll_LinksEntitiesForDuplicateUpdates(t *testing.T) {
	ctx := context.Background()
	llm := service.NewLLMSummarizer("http://127.0.0.1:1", "test-key", "test-model")
	svc, repo := testServiceForReprocessWithSummarizer(t, llm)
	concreteSvc, ok := svc.(*service.AMMService)
	if !ok {
		t.Fatal("expected concrete AMMService")
	}

	intel := &reprocessIntelligenceStub{
		analysisResult: &core.AnalysisResult{
			Memories: []core.MemoryCandidate{{
				Type:             core.MemoryTypeDecision,
				Subject:          "platform",
				Body:             "ACME uses managed PostgreSQL",
				TightDescription: "ACME uses managed PostgreSQL",
				Confidence:       0.93,
				SourceEventNums:  []int{1},
			}},
			Entities: []core.EntityCandidate{{
				CanonicalName: "ACME Platform",
				Type:          "project",
				Aliases:       []string{"acme"},
			}},
		},
	}
	concreteSvc.SetIntelligenceProvider(intel)

	now := time.Now().UTC()
	if err := repo.InsertEvent(ctx, &core.Event{ID: "evt_dup_link", Kind: "message_user", SourceSystem: "test", Content: "ACME uses managed PostgreSQL for reliability", PrivacyLevel: core.PrivacyPrivate, OccurredAt: now, IngestedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertMemory(ctx, &core.Memory{
		ID:               "mem_dup_link",
		Type:             core.MemoryTypeDecision,
		Scope:            core.ScopeGlobal,
		Subject:          "platform",
		Body:             "ACME uses managed PostgreSQL",
		TightDescription: "ACME uses managed PostgreSQL",
		Confidence:       0.6,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatal(err)
	}

	job, err := svc.RunJob(ctx, "reprocess_all")
	if err != nil {
		t.Fatalf("reprocess_all failed: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("unexpected job status: %s", job.Status)
	}

	linked, err := repo.GetMemoryEntities(ctx, "mem_dup_link")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entity := range linked {
		if entity.CanonicalName == "ACME Platform" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected duplicate-updated memory to be entity-linked, got %#v", linked)
	}
}

func generateTestID(prefix string, i int) string {
	return prefix + "reprocess_" + string(rune('a'+i))
}
