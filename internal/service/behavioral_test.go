package service

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/adapters/sqlite"
	"github.com/bonztm/agent-memory-manager/internal/core"
)

// testServiceAndRepo creates an AMMService backed by a real SQLite DB and
// returns both the concrete service and the repository for direct inserts.
func testServiceAndRepo(t *testing.T) (*AMMService, *sqlite.SQLiteRepository) {
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
	repo := &sqlite.SQLiteRepository{DB: db}
	svc := New(repo, dbPath, nil, nil)
	t.Cleanup(func() { db.Close() })
	return svc, repo
}

func testServiceAndRepoWithSummarizer(t *testing.T, summarizer core.Summarizer) (*AMMService, *sqlite.SQLiteRepository) {
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
	repo := &sqlite.SQLiteRepository{DB: db}
	svc := New(repo, dbPath, summarizer, nil)
	t.Cleanup(func() { db.Close() })
	return svc, repo
}

func testServiceAndRepoWithEmbeddingProvider(t *testing.T, provider core.EmbeddingProvider) (*AMMService, *sqlite.SQLiteRepository) {
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
	repo := &sqlite.SQLiteRepository{DB: db}
	svc := New(repo, dbPath, nil, provider)
	t.Cleanup(func() { db.Close() })
	return svc, repo
}

type reflectTestSummarizer struct {
	extract         func(string) ([]core.MemoryCandidate, error)
	extractBatch    func([]string) ([]core.MemoryCandidate, error)
	individualCalls *int
	batchCalls      *int
	batchSizes      *[]int
}

type staticEmbeddingProvider struct {
	model   string
	vectors map[string][]float32
}

type failingEmbeddingProvider struct{}

func (p staticEmbeddingProvider) Name() string { return "test-static" }

func (p staticEmbeddingProvider) Model() string {
	if p.model == "" {
		return "test-model"
	}
	return p.model
}

func (p staticEmbeddingProvider) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		if vec, ok := p.vectors[text]; ok {
			out[i] = vec
			continue
		}
		out[i] = []float32{}
	}
	return out, nil
}

func (failingEmbeddingProvider) Name() string { return "test-failing" }

func (failingEmbeddingProvider) Model() string { return "test-model" }

func (failingEmbeddingProvider) Embed(context.Context, []string) ([][]float32, error) {
	return nil, fmt.Errorf("embedding provider failure")
}

type consolidateTestSummarizer struct {
	summarize func(content string, maxLen int) (string, error)
}

type consolidateTestIntelligence struct {
	summarize                 func(content string, maxLen int) (string, error)
	consolidate               func(events []core.EventContent, existingMemories []core.MemorySummary) (*core.NarrativeResult, error)
	compressEventBatches      func(chunks []core.EventChunk) ([]core.CompressionResult, error)
	summarizeTopicBatches     func(topics []core.TopicChunk) ([]core.CompressionResult, error)
	triage                    func(events []core.EventContent) (map[int]core.TriageDecision, error)
	callCountPtr              *int32
	compressBatchCallCountPtr *int32
	topicBatchCallCountPtr    *int32
}

func (s consolidateTestSummarizer) Summarize(_ context.Context, content string, maxLen int) (string, error) {
	if s.summarize == nil {
		return content, nil
	}
	return s.summarize(content, maxLen)
}

func (consolidateTestSummarizer) ExtractMemoryCandidate(context.Context, string) ([]core.MemoryCandidate, error) {
	return nil, nil
}

func (consolidateTestSummarizer) ExtractMemoryCandidateBatch(context.Context, []string) ([]core.MemoryCandidate, error) {
	return nil, nil
}

func (m consolidateTestIntelligence) Summarize(_ context.Context, content string, maxLen int) (string, error) {
	if m.summarize != nil {
		return m.summarize(content, maxLen)
	}
	return content, nil
}

func (consolidateTestIntelligence) ExtractMemoryCandidate(context.Context, string) ([]core.MemoryCandidate, error) {
	return nil, nil
}

func (consolidateTestIntelligence) ExtractMemoryCandidateBatch(context.Context, []string) ([]core.MemoryCandidate, error) {
	return nil, nil
}

func (consolidateTestIntelligence) AnalyzeEvents(context.Context, []core.EventContent) (*core.AnalysisResult, error) {
	return &core.AnalysisResult{}, nil
}

func (consolidateTestIntelligence) ReviewMemories(context.Context, []core.MemoryReview) (*core.ReviewResult, error) {
	return &core.ReviewResult{}, nil
}

func (m consolidateTestIntelligence) TriageEvents(_ context.Context, events []core.EventContent) (map[int]core.TriageDecision, error) {
	if m.triage != nil {
		return m.triage(events)
	}
	decisions := make(map[int]core.TriageDecision, len(events))
	for i, evt := range events {
		index := evt.Index
		if index <= 0 {
			index = i + 1
		}
		decisions[index] = core.TriageReflect
	}
	return decisions, nil
}

func (m consolidateTestIntelligence) CompressEventBatches(_ context.Context, chunks []core.EventChunk) ([]core.CompressionResult, error) {
	if m.compressBatchCallCountPtr != nil {
		atomic.AddInt32(m.compressBatchCallCountPtr, 1)
	}
	if m.compressEventBatches != nil {
		return m.compressEventBatches(chunks)
	}
	results := make([]core.CompressionResult, 0, len(chunks))
	for _, chunk := range chunks {
		results = append(results, core.CompressionResult{
			Index:            chunk.Index,
			Body:             strings.Join(chunk.Contents, "\n"),
			TightDescription: fmt.Sprintf("chunk-%d", chunk.Index),
		})
	}
	return results, nil
}

func (m consolidateTestIntelligence) SummarizeTopicBatches(_ context.Context, topics []core.TopicChunk) ([]core.CompressionResult, error) {
	if m.topicBatchCallCountPtr != nil {
		atomic.AddInt32(m.topicBatchCallCountPtr, 1)
	}
	if m.summarizeTopicBatches != nil {
		return m.summarizeTopicBatches(topics)
	}
	results := make([]core.CompressionResult, 0, len(topics))
	for _, topic := range topics {
		results = append(results, core.CompressionResult{
			Index:            topic.Index,
			Body:             strings.Join(topic.Contents, "\n\n"),
			TightDescription: fmt.Sprintf("topic-%d", topic.Index),
		})
	}
	return results, nil
}

func (m consolidateTestIntelligence) ConsolidateNarrative(_ context.Context, events []core.EventContent, existingMemories []core.MemorySummary) (*core.NarrativeResult, error) {
	if m.callCountPtr != nil {
		atomic.AddInt32(m.callCountPtr, 1)
	}
	if m.consolidate != nil {
		return m.consolidate(events, existingMemories)
	}
	return &core.NarrativeResult{}, nil
}

func (s reflectTestSummarizer) Summarize(context.Context, string, int) (string, error) {
	return "", nil
}

func (s reflectTestSummarizer) ExtractMemoryCandidate(_ context.Context, content string) ([]core.MemoryCandidate, error) {
	if s.individualCalls != nil {
		(*s.individualCalls)++
	}
	if s.extract == nil {
		return nil, nil
	}
	return s.extract(content)
}

func (s reflectTestSummarizer) ExtractMemoryCandidateBatch(_ context.Context, contents []string) ([]core.MemoryCandidate, error) {
	if s.batchCalls != nil {
		(*s.batchCalls)++
	}
	if s.batchSizes != nil {
		*s.batchSizes = append(*s.batchSizes, len(contents))
	}
	if s.extractBatch != nil {
		return s.extractBatch(contents)
	}

	all := make([]core.MemoryCandidate, 0, len(contents))
	for i, content := range contents {
		if s.extract == nil {
			continue
		}
		candidates, err := s.extract(content)
		if err != nil {
			return nil, err
		}
		for j := range candidates {
			if len(candidates[j].SourceEventNums) == 0 {
				candidates[j].SourceEventNums = []int{i + 1}
			}
		}
		all = append(all, candidates...)
	}
	return all, nil
}

// ---------------------------------------------------------------------------
// Reflect
// ---------------------------------------------------------------------------

func TestReflect_ExtractsPreferences(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	// Ingest events containing preference phrases.
	phrases := []string{
		"I prefer tabs over spaces for indentation",
		"always use dark mode in the editor",
	}
	for _, p := range phrases {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      p,
			OccurredAt:   time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created < 1 {
		t.Fatalf("expected at least 1 memory created, got %d", created)
	}

	// Verify at least one memory with type=preference.
	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range mems {
		if m.Type == core.MemoryTypePreference {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one preference memory after Reflect")
	}
}

func TestReflect_ExtractsDecisions(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.IngestEvent(ctx, &core.Event{
		Kind:         "message",
		SourceSystem: "test",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "We decided to use PostgreSQL for the database layer",
		OccurredAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created < 1 {
		t.Fatalf("expected at least 1 memory, got %d", created)
	}

	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range mems {
		if m.Type == core.MemoryTypeDecision {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one decision memory after Reflect")
	}
}

func TestReflect_AssignsTypeBasedImportance(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.IngestEvent(ctx, &core.Event{
		Kind:         "message",
		SourceSystem: "test",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "We decided to use PostgreSQL for the database layer",
		OccurredAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created < 1 {
		t.Fatalf("expected at least 1 memory, got %d", created)
	}

	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range mems {
		if m.Type == core.MemoryTypeDecision {
			if math.Abs(m.Importance-0.85) > 0.0001 {
				t.Fatalf("expected decision importance 0.85, got %f", m.Importance)
			}
			return
		}
	}
	t.Fatal("expected reflected decision memory")
}

func TestReflect_SkipsDuplicates(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.IngestEvent(ctx, &core.Event{
		Kind:         "message",
		SourceSystem: "test",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "I prefer using Go for backend services",
		OccurredAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if first < 1 {
		t.Fatalf("expected first Reflect to create >= 1, got %d", first)
	}

	second, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if second != 0 {
		t.Errorf("expected second Reflect to create 0 (dedup), got %d", second)
	}
}

func TestReflect_DeduplicatesParaphrases(t *testing.T) {
	summarizer := reflectTestSummarizer{extract: func(content string) ([]core.MemoryCandidate, error) {
		body := "Use Postgres for the primary application database"
		if strings.Contains(content, "paraphrase two") {
			body = "Use Postgres for primary application database"
		}
		return []core.MemoryCandidate{{
			Type:             core.MemoryTypeFact,
			Subject:          "database",
			Body:             body,
			TightDescription: body,
			Confidence:       0.9,
		}}, nil
	}}

	svc, repo := testServiceAndRepoWithSummarizer(t, summarizer)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i, content := range []string{"event paraphrase one", "event paraphrase two"} {
		evt := &core.Event{
			ID:           fmt.Sprintf("evt_reflect_paraphrase_%d", i),
			Kind:         "message_user",
			SourceSystem: "test",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      content,
			OccurredAt:   now,
			IngestedAt:   now,
		}
		if err := repo.InsertEvent(ctx, evt); err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("expected 1 created memory after paraphrase dedup, got %d", created)
	}

	all, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Type: core.MemoryTypeFact, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 {
		t.Fatalf("expected exactly 1 stored memory for paraphrases, got %d", len(all))
	}
	if len(all[0].SourceEventIDs) != 2 {
		t.Fatalf("expected deduped memory to retain both source event IDs, got %d", len(all[0].SourceEventIDs))
	}
}

func TestReflect_CreatesEntities(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	evt, err := svc.IngestEvent(ctx, &core.Event{
		Kind:         "message",
		SourceSystem: "test",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "We decided Josh should own Kubernetes rollout planning.",
		OccurredAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created < 1 {
		t.Fatalf("expected at least 1 memory, got %d", created)
	}

	entities, err := svc.repo.ListEntities(ctx, core.ListEntitiesOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}

	entityByName := make(map[string]core.Entity, len(entities))
	for _, ent := range entities {
		entityByName[strings.ToLower(ent.CanonicalName)] = ent
	}

	if _, ok := entityByName["josh"]; !ok {
		t.Fatalf("expected entity Josh to be created; got entities=%v", entities)
	}
	if _, ok := entityByName["kubernetes"]; !ok {
		t.Fatalf("expected entity Kubernetes to be created; got entities=%v", entities)
	}

	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var reflected *core.Memory
	for i := range mems {
		for _, srcID := range mems[i].SourceEventIDs {
			if srcID == evt.ID {
				reflected = &mems[i]
				break
			}
		}
		if reflected != nil {
			break
		}
	}
	if reflected == nil {
		t.Fatal("expected reflected memory linked to ingested event")
	}

	linkedEntities, err := svc.repo.GetMemoryEntities(ctx, reflected.ID)
	if err != nil {
		t.Fatal(err)
	}
	linkedNames := make(map[string]bool, len(linkedEntities))
	for _, ent := range linkedEntities {
		linkedNames[strings.ToLower(ent.CanonicalName)] = true
	}
	if !linkedNames["josh"] {
		t.Fatalf("expected reflected memory to be linked to Josh; got linked=%v", linkedEntities)
	}
	if !linkedNames["kubernetes"] {
		t.Fatalf("expected reflected memory to be linked to Kubernetes; got linked=%v", linkedEntities)
	}
}

func TestReflect_SetsProvisionalWhenHeuristic(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	evt, err := svc.IngestEvent(ctx, &core.Event{
		Kind:         "message_user",
		SourceSystem: "test",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "I prefer concise commit messages",
		OccurredAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created < 1 {
		t.Fatalf("expected at least one reflected memory, got %d", created)
	}

	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, mem := range mems {
		if len(mem.SourceEventIDs) == 1 && mem.SourceEventIDs[0] == evt.ID {
			if mem.Metadata["extraction_quality"] != "provisional" {
				t.Fatalf("expected extraction_quality=provisional, got %q", mem.Metadata["extraction_quality"])
			}
			return
		}
	}
	t.Fatalf("expected reflected memory linked to event %s", evt.ID)
}

func TestReflect_SetsVerifiedWhenLLM(t *testing.T) {
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mockChatResponse(`[{"type":"preference","subject":"style","body":"Prefers concise commit messages","tight_description":"Prefers concise commit messages","confidence":0.92}]`)))
	}))
	t.Cleanup(server.Close)

	llm := NewLLMSummarizer(server.URL, "test-key", "test-model")
	svc, _ := testServiceAndRepoWithSummarizer(t, llm)

	evt, err := svc.IngestEvent(ctx, &core.Event{
		Kind:         "message_user",
		SourceSystem: "test",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "I prefer concise commit messages",
		OccurredAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("expected 1 reflected memory, got %d", created)
	}

	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, mem := range mems {
		if len(mem.SourceEventIDs) == 1 && mem.SourceEventIDs[0] == evt.ID {
			if mem.Metadata["extraction_quality"] != "verified" {
				t.Fatalf("expected extraction_quality=verified, got %q", mem.Metadata["extraction_quality"])
			}
			return
		}
	}
	t.Fatalf("expected reflected memory linked to event %s", evt.ID)
}

func TestReflect_PaginatesBacklogBeyond100(t *testing.T) {
	summarizer := reflectTestSummarizer{extract: func(content string) ([]core.MemoryCandidate, error) {
		return []core.MemoryCandidate{{
			Type:             core.MemoryTypeFact,
			Subject:          "backlog",
			Body:             content,
			TightDescription: content,
			Confidence:       0.9,
		}}, nil
	}}
	svc, repo := testServiceAndRepoWithSummarizer(t, summarizer)
	ctx := context.Background()
	now := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)

	for i := 0; i < 205; i++ {
		evt := &core.Event{
			ID:           fmt.Sprintf("evt_reflect_page_%03d", i),
			Kind:         "message_user",
			SourceSystem: "test",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("reflect backlog event %03d", i),
			OccurredAt:   now,
			IngestedAt:   now,
		}
		if err := repo.InsertEvent(ctx, evt); err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created != 205 {
		t.Fatalf("expected 205 created memories across paginated backlog, got %d", created)
	}

	mems, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 205 {
		t.Fatalf("expected 205 active memories from 205 reflected events, got %d", len(mems))
	}

	superseded, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusSuperseded, Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	if len(superseded) != 0 {
		t.Fatalf("expected 0 superseded memories for non-duplicate backlog events, got %d", len(superseded))
	}

	createdAgain, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if createdAgain != 0 {
		t.Fatalf("expected second reflect run to create 0 memories, got %d", createdAgain)
	}
}

func TestReflect_StoresAllCandidatesPerEvent(t *testing.T) {
	eventID := "evt_multi_candidate"
	summarizer := reflectTestSummarizer{extract: func(content string) ([]core.MemoryCandidate, error) {
		return []core.MemoryCandidate{
			{Type: core.MemoryTypeFact, Subject: "alpha", Body: "first candidate", TightDescription: "first candidate", Confidence: 0.9},
			{Type: core.MemoryTypeDecision, Subject: "alpha", Body: "second candidate", TightDescription: "second candidate", Confidence: 0.95},
		}, nil
	}}
	svc, repo := testServiceAndRepoWithSummarizer(t, summarizer)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repo.InsertEvent(ctx, &core.Event{ID: eventID, Kind: "message_user", SourceSystem: "test", PrivacyLevel: core.PrivacyPrivate, Content: "event with multiple memories", OccurredAt: now, IngestedAt: now}); err != nil {
		t.Fatal(err)
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created != 2 {
		t.Fatalf("expected 2 created memories, got %d", created)
	}

	mems, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 2 {
		t.Fatalf("expected 2 active memories, got %d", len(mems))
	}
	for _, mem := range mems {
		if len(mem.SourceEventIDs) != 1 || mem.SourceEventIDs[0] != eventID {
			t.Fatalf("expected source_event_ids [%s], got %#v", eventID, mem.SourceEventIDs)
		}
	}
}

func TestReflect_RejectsInvalidCandidates(t *testing.T) {
	summarizer := reflectTestSummarizer{extract: func(content string) ([]core.MemoryCandidate, error) {
		return []core.MemoryCandidate{
			{Type: core.MemoryType("bogus"), Subject: "invalid", Body: "bad type", TightDescription: "bad type", Confidence: 0.8},
			{Type: core.MemoryTypeFact, Subject: "invalid", Body: "", TightDescription: "missing body", Confidence: 0.8},
			{Type: core.MemoryTypeFact, Subject: "invalid", Body: "missing description", TightDescription: "", Confidence: 0.8},
			{Type: core.MemoryTypeFact, Subject: "valid", Body: "valid memory body", TightDescription: "valid memory", Confidence: 0.9},
		}, nil
	}}
	svc, repo := testServiceAndRepoWithSummarizer(t, summarizer)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := repo.InsertEvent(ctx, &core.Event{ID: "evt_invalid_candidates", Kind: "message_user", SourceSystem: "test", PrivacyLevel: core.PrivacyPrivate, Content: "candidate validation event", OccurredAt: now, IngestedAt: now}); err != nil {
		t.Fatal(err)
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("expected 1 valid reflected memory, got %d", created)
	}

	mems, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected 1 active memory, got %d", len(mems))
	}
	if mems[0].TightDescription != "valid memory" {
		t.Fatalf("expected surviving memory to be the valid candidate, got %q", mems[0].TightDescription)
	}
}

func TestReflect_AdvancesCursorPastInvalidOnlyEvents(t *testing.T) {
	summarizer := reflectTestSummarizer{extract: func(content string) ([]core.MemoryCandidate, error) {
		if strings.Contains(content, "invalid") {
			return []core.MemoryCandidate{{
				Type:             core.MemoryTypeFact,
				Body:             "",
				TightDescription: "invalid candidate",
				Confidence:       0.8,
			}}, nil
		}
		return []core.MemoryCandidate{{
			Type:             core.MemoryTypeFact,
			Body:             content,
			TightDescription: content,
			Confidence:       0.9,
		}}, nil
	}}
	svc, repo := testServiceAndRepoWithSummarizer(t, summarizer)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	events := []*core.Event{
		{ID: "evt_invalid_only", Kind: "message_user", SourceSystem: "test", PrivacyLevel: core.PrivacyPrivate, Content: "invalid event", OccurredAt: now, IngestedAt: now},
		{ID: "evt_valid_after_invalid", Kind: "message_user", SourceSystem: "test", PrivacyLevel: core.PrivacyPrivate, Content: "valid event", OccurredAt: now, IngestedAt: now},
	}
	for _, evt := range events {
		if err := repo.InsertEvent(ctx, evt); err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("expected one valid memory from first reflect run, got %d", created)
	}

	// Second run should find zero unreflected events (all were claimed on first run).
	createdAgain, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if createdAgain != 0 {
		t.Fatalf("expected second reflect run to create 0 memories, got %d", createdAgain)
	}
}

func TestReflect_SupersedesConflictingExistingMemory(t *testing.T) {
	summarizer := reflectTestSummarizer{extract: func(content string) ([]core.MemoryCandidate, error) {
		return []core.MemoryCandidate{{
			Type:             core.MemoryTypeFact,
			Subject:          "database",
			Body:             "Use Postgres for storage",
			TightDescription: "database choice postgres",
			Confidence:       0.9,
		}}, nil
	}}
	svc, repo := testServiceAndRepoWithSummarizer(t, summarizer)
	ctx := context.Background()
	now := time.Now().UTC()

	old := &core.Memory{
		ID:               "mem_reflect_conflict_old",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Subject:          "database",
		Body:             "Use SQLite for storage",
		TightDescription: "database choice sqlite",
		Confidence:       0.8,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		CreatedAt:        now.Add(-time.Minute),
		UpdatedAt:        now.Add(-time.Minute),
	}
	if err := repo.InsertMemory(ctx, old); err != nil {
		t.Fatal(err)
	}

	if err := repo.InsertEvent(ctx, &core.Event{ID: "evt_reflect_conflict", Kind: "message_user", SourceSystem: "test", PrivacyLevel: core.PrivacyPrivate, Content: "we should use postgres", OccurredAt: now, IngestedAt: now}); err != nil {
		t.Fatal(err)
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("expected 1 created memory when content is not similar enough to merge, got %d", created)
	}

	updatedOld, err := repo.GetMemory(ctx, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedOld.Status != core.MemoryStatusActive {
		t.Fatalf("expected existing memory to remain active, got %s", updatedOld.Status)
	}
	if updatedOld.SupersededBy != "" {
		t.Fatalf("expected existing memory to have empty superseded_by, got %q", updatedOld.SupersededBy)
	}
	if updatedOld.Body != "Use SQLite for storage" {
		t.Fatalf("expected existing memory body to remain unchanged, got %q", updatedOld.Body)
	}
	if len(updatedOld.SourceEventIDs) != 0 {
		t.Fatalf("expected existing memory source event IDs unchanged, got %v", updatedOld.SourceEventIDs)
	}

	all, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Type: core.MemoryTypeFact, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected two fact memories when existing and candidate are not duplicates, got %d", len(all))
	}

	var postgresFound bool
	for _, mem := range all {
		if mem.Body == "Use Postgres for storage" {
			postgresFound = true
			break
		}
	}
	if !postgresFound {
		t.Fatal("expected reflected postgres memory to be inserted")
	}
}

func TestReflect_UsesBatchExtraction(t *testing.T) {
	batchCalls := 0
	individualCalls := 0
	summarizer := reflectTestSummarizer{
		extract: func(content string) ([]core.MemoryCandidate, error) {
			return []core.MemoryCandidate{{
				Type:             core.MemoryTypeFact,
				Subject:          "batch",
				Body:             content,
				TightDescription: content,
				Confidence:       0.9,
			}}, nil
		},
		batchCalls:      &batchCalls,
		individualCalls: &individualCalls,
	}

	svc, repo := testServiceAndRepoWithSummarizer(t, summarizer)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 5; i++ {
		evt := &core.Event{
			ID:           fmt.Sprintf("evt_reflect_batch_%d", i),
			Kind:         "message_user",
			SourceSystem: "test",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("batched reflect event %d", i),
			OccurredAt:   now,
			IngestedAt:   now,
		}
		if err := repo.InsertEvent(ctx, evt); err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created != 5 {
		t.Fatalf("expected 5 memories created, got %d", created)
	}
	if batchCalls != 1 {
		t.Fatalf("expected exactly 1 batch extraction call, got %d", batchCalls)
	}
	if individualCalls != 0 {
		t.Fatalf("expected 0 per-event extraction calls, got %d", individualCalls)
	}
}

func TestReflect_BatchSourceEventTracking(t *testing.T) {
	summarizer := reflectTestSummarizer{
		extractBatch: func(_ []string) ([]core.MemoryCandidate, error) {
			return []core.MemoryCandidate{
				{
					Type:             core.MemoryTypeFact,
					Subject:          "combined",
					Body:             "combined source memory",
					TightDescription: "combined source memory",
					Confidence:       0.9,
					SourceEventNums:  []int{1, 3},
				},
				{
					Type:             core.MemoryTypeFact,
					Subject:          "single",
					Body:             "single source memory",
					TightDescription: "single source memory",
					Confidence:       0.9,
					SourceEventNums:  []int{2},
				},
			}, nil
		},
	}
	svc, repo := testServiceAndRepoWithSummarizer(t, summarizer)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	eventIDs := []string{"evt_batch_src_1", "evt_batch_src_2", "evt_batch_src_3"}
	for i, id := range eventIDs {
		evt := &core.Event{
			ID:           id,
			Kind:         "message_user",
			SourceSystem: "test",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("source event %d", i+1),
			OccurredAt:   now,
			IngestedAt:   now,
		}
		if err := repo.InsertEvent(ctx, evt); err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created != 2 {
		t.Fatalf("expected 2 created memories, got %d", created)
	}

	mems, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Type: core.MemoryTypeFact, Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 2 {
		t.Fatalf("expected 2 active memories, got %d", len(mems))
	}

	byBody := make(map[string]core.Memory, len(mems))
	for _, mem := range mems {
		byBody[mem.Body] = mem
	}

	combined := byBody["combined source memory"]
	if len(combined.SourceEventIDs) != 2 || combined.SourceEventIDs[0] != eventIDs[0] || combined.SourceEventIDs[1] != eventIDs[2] {
		t.Fatalf("expected combined memory source_event_ids [%s %s], got %v", eventIDs[0], eventIDs[2], combined.SourceEventIDs)
	}

	single := byBody["single source memory"]
	if len(single.SourceEventIDs) != 1 || single.SourceEventIDs[0] != eventIDs[1] {
		t.Fatalf("expected single memory source_event_ids [%s], got %v", eventIDs[1], single.SourceEventIDs)
	}
}

func TestReflect_SingleEvent_StillBatched(t *testing.T) {
	batchCalls := 0
	individualCalls := 0
	summarizer := reflectTestSummarizer{
		extract: func(content string) ([]core.MemoryCandidate, error) {
			return []core.MemoryCandidate{{
				Type:             core.MemoryTypeFact,
				Body:             content,
				TightDescription: content,
				Confidence:       0.9,
			}}, nil
		},
		batchCalls:      &batchCalls,
		individualCalls: &individualCalls,
	}

	svc, repo := testServiceAndRepoWithSummarizer(t, summarizer)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	if err := repo.InsertEvent(ctx, &core.Event{ID: "evt_reflect_single_batch", Kind: "message_user", SourceSystem: "test", PrivacyLevel: core.PrivacyPrivate, Content: "single event", OccurredAt: now, IngestedAt: now}); err != nil {
		t.Fatal(err)
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("expected 1 created memory, got %d", created)
	}
	if batchCalls != 1 {
		t.Fatalf("expected 1 batch extraction call, got %d", batchCalls)
	}
	if individualCalls != 0 {
		t.Fatalf("expected 0 per-event extraction calls, got %d", individualCalls)
	}
}

func TestReflect_ConfigurableBatchSize(t *testing.T) {
	batchSizes := []int{}
	summarizer := reflectTestSummarizer{
		extract: func(content string) ([]core.MemoryCandidate, error) {
			return []core.MemoryCandidate{{
				Type:             core.MemoryTypeFact,
				Body:             content,
				TightDescription: content,
				Confidence:       0.9,
			}}, nil
		},
		batchSizes: &batchSizes,
	}

	svc, repo := testServiceAndRepoWithSummarizer(t, summarizer)
	svc.SetReflectLLMBatchSize(3)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 7; i++ {
		evt := &core.Event{
			ID:           fmt.Sprintf("evt_reflect_cfg_batch_%d", i),
			Kind:         "message_user",
			SourceSystem: "test",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("configurable reflect event %d", i),
			OccurredAt:   now,
			IngestedAt:   now,
		}
		if err := repo.InsertEvent(ctx, evt); err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created != 7 {
		t.Fatalf("expected 7 created memories, got %d", created)
	}
	if len(batchSizes) != 3 {
		t.Fatalf("expected 3 batch calls (3+3+1), got %d calls: %v", len(batchSizes), batchSizes)
	}
	if batchSizes[0] != 3 || batchSizes[1] != 3 || batchSizes[2] != 1 {
		t.Fatalf("expected batch sizes [3 3 1], got %v", batchSizes)
	}
}

// ---------------------------------------------------------------------------
// CompressHistory
// ---------------------------------------------------------------------------

func TestCompressHistory(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	// Ingest 15 events.
	for i := 0; i < 15; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("event number %d about compression testing", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.CompressHistory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// 15 events with chunk size 10 should create 2 leaf summaries.
	if created < 1 {
		t.Fatalf("expected at least 1 leaf summary, got %d", created)
	}

	// Verify summaries exist and have source_span.
	summaries, err := svc.repo.ListSummaries(ctx, core.ListSummariesOptions{Kind: "leaf", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) == 0 {
		t.Fatal("expected leaf summaries to be created")
	}
	for _, s := range summaries {
		if len(s.SourceSpan.EventIDs) == 0 {
			t.Errorf("summary %s has empty source_span.event_ids", s.ID)
		}
	}
}

func TestCompressHistory_GeneratesTightDescription(t *testing.T) {
	var bodyCallCount int
	var tightCallCount int
	svc, _ := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{summarize: func(content string, maxLen int) (string, error) {
		switch maxLen {
		case leafBodyMaxChars:
			bodyCallCount++
			return "compressed body summary", nil
		case 100:
			tightCallCount++
			if content != "compressed body summary" {
				return "", fmt.Errorf("expected tight description input to be summary body, got %q", content)
			}
			return "tight summary from summarizer", nil
		default:
			return "", fmt.Errorf("unexpected maxLen: %d", maxLen)
		}
	}})
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("compress tight event %d", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.CompressHistory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("expected 1 leaf summary, got %d", created)
	}
	if bodyCallCount != 1 {
		t.Fatalf("expected 1 body summarize call, got %d", bodyCallCount)
	}
	if tightCallCount != 1 {
		t.Fatalf("expected 1 tight summarize call, got %d", tightCallCount)
	}

	summaries, err := svc.repo.ListSummaries(ctx, core.ListSummariesOptions{Kind: "leaf", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 leaf summary, got %d", len(summaries))
	}
	if summaries[0].TightDescription != "tight summary from summarizer" {
		t.Fatalf("expected summarized tight description, got %q", summaries[0].TightDescription)
	}
	if strings.HasPrefix(summaries[0].TightDescription, "Summary of") {
		t.Fatalf("expected non-fallback tight description, got %q", summaries[0].TightDescription)
	}
}

func TestCompressHistory_UsesBatchedIntelligence(t *testing.T) {
	var summarizeCalls int32
	var batchCalls int32
	svc, _ := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{summarize: func(content string, maxLen int) (string, error) {
		atomic.AddInt32(&summarizeCalls, 1)
		return "unexpected fallback", nil
	}})
	ctx := context.Background()

	svc.SetIntelligenceProvider(consolidateTestIntelligence{
		compressBatchCallCountPtr: &batchCalls,
		compressEventBatches: func(chunks []core.EventChunk) ([]core.CompressionResult, error) {
			if len(chunks) != 2 {
				t.Fatalf("expected 2 chunks, got %d", len(chunks))
			}
			return []core.CompressionResult{
				{Index: chunks[0].Index, Body: "batched body 1", TightDescription: "batched tight 1"},
				{Index: chunks[1].Index, Body: "batched body 2", TightDescription: "batched tight 2"},
			}, nil
		},
	})

	for i := 0; i < 15; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("batched compress event %d", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.CompressHistory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created != 2 {
		t.Fatalf("expected 2 leaf summaries, got %d", created)
	}
	if atomic.LoadInt32(&batchCalls) != 1 {
		t.Fatalf("expected one batched compress call, got %d", atomic.LoadInt32(&batchCalls))
	}
	if atomic.LoadInt32(&summarizeCalls) != 0 {
		t.Fatalf("expected no fallback summarize calls, got %d", atomic.LoadInt32(&summarizeCalls))
	}

	summaries, err := svc.repo.ListSummaries(ctx, core.ListSummariesOptions{Kind: "leaf", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 leaf summaries, got %d", len(summaries))
	}
	gotTight := map[string]struct{}{}
	for _, summary := range summaries {
		gotTight[summary.TightDescription] = struct{}{}
	}
	if _, ok := gotTight["batched tight 1"]; !ok {
		t.Fatalf("expected batched tight 1 in summaries, got %#v", gotTight)
	}
	if _, ok := gotTight["batched tight 2"]; !ok {
		t.Fatalf("expected batched tight 2 in summaries, got %#v", gotTight)
	}
}

func TestCompressHistory_FallsBackWhenBatchCompressionFails(t *testing.T) {
	var bodyCalls int32
	var tightCalls int32
	var batchCalls int32
	svc, _ := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{summarize: func(content string, maxLen int) (string, error) {
		switch maxLen {
		case leafBodyMaxChars:
			atomic.AddInt32(&bodyCalls, 1)
			return "fallback leaf body", nil
		case 100:
			atomic.AddInt32(&tightCalls, 1)
			return "fallback leaf tight", nil
		default:
			return content, nil
		}
	}})
	ctx := context.Background()

	svc.SetIntelligenceProvider(consolidateTestIntelligence{
		compressBatchCallCountPtr: &batchCalls,
		compressEventBatches: func([]core.EventChunk) ([]core.CompressionResult, error) {
			return nil, fmt.Errorf("batch failed")
		},
	})

	for i := 0; i < 3; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("fallback compress event %d", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.CompressHistory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("expected 1 leaf summary, got %d", created)
	}
	if atomic.LoadInt32(&batchCalls) != 1 {
		t.Fatalf("expected one failed batch call, got %d", atomic.LoadInt32(&batchCalls))
	}
	if atomic.LoadInt32(&bodyCalls) != 1 || atomic.LoadInt32(&tightCalls) != 1 {
		t.Fatalf("expected fallback summarize calls body=1 tight=1, got body=%d tight=%d", atomic.LoadInt32(&bodyCalls), atomic.LoadInt32(&tightCalls))
	}

	summaries, err := svc.repo.ListSummaries(ctx, core.ListSummariesOptions{Kind: "leaf", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 leaf summary, got %d", len(summaries))
	}
	if summaries[0].Body != "fallback leaf body" || summaries[0].TightDescription != "fallback leaf tight" {
		t.Fatalf("expected fallback summary values, got body=%q tight=%q", summaries[0].Body, summaries[0].TightDescription)
	}
}

// ---------------------------------------------------------------------------
// ConsolidateSessions
// ---------------------------------------------------------------------------

func TestConsolidateSessions(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	sessID := "sess_consolidate_test"
	for i := 0; i < 5; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			SessionID:    sessID,
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("session event %d discussing consolidation", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.ConsolidateSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created < 1 {
		t.Fatalf("expected at least 1 session summary, got %d", created)
	}

	summaries, err := svc.repo.ListSummaries(ctx, core.ListSummariesOptions{Kind: "session", SessionID: sessID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 session summary, got %d", len(summaries))
	}
	if summaries[0].SessionID != sessID {
		t.Errorf("expected session_id=%s, got %s", sessID, summaries[0].SessionID)
	}
}

func TestConsolidateSessions_UsesSummarizer(t *testing.T) {
	var gotBodyInput string
	var gotBodyMaxLen int
	var gotTightInput string
	var gotTightMaxLen int
	svc, _ := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{summarize: func(content string, maxLen int) (string, error) {
		switch maxLen {
		case sessionBodyMaxChars:
			gotBodyInput = content
			gotBodyMaxLen = maxLen
			return "llm session summary", nil
		case 100:
			gotTightInput = content
			gotTightMaxLen = maxLen
			return "llm tight description", nil
		default:
			return "", fmt.Errorf("unexpected maxLen: %d", maxLen)
		}
	}})
	ctx := context.Background()

	sessID := "sess_consolidate_llm"
	for i := 0; i < 3; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			SessionID:    sessID,
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("llm consolidate event %d", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.ConsolidateSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("expected 1 session summary, got %d", created)
	}
	if gotBodyMaxLen != sessionBodyMaxChars {
		t.Fatalf("expected body summarize maxLen %d, got %d", sessionBodyMaxChars, gotBodyMaxLen)
	}
	if !strings.Contains(gotBodyInput, "llm consolidate event 0") || !strings.Contains(gotBodyInput, "llm consolidate event 2") {
		t.Fatalf("expected body summarize input to include concatenated event content, got %q", gotBodyInput)
	}
	if gotTightMaxLen != 100 {
		t.Fatalf("expected tight summarize maxLen 100, got %d", gotTightMaxLen)
	}
	if gotTightInput != "llm session summary" {
		t.Fatalf("expected tight summarize input to be summarized body, got %q", gotTightInput)
	}

	summaries, err := svc.repo.ListSummaries(ctx, core.ListSummariesOptions{Kind: "session", SessionID: sessID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 session summary, got %d", len(summaries))
	}
	if summaries[0].Body != "llm session summary" {
		t.Fatalf("expected summarized body, got %q", summaries[0].Body)
	}
	if summaries[0].TightDescription != "llm tight description" {
		t.Fatalf("expected summarized tight description, got %q", summaries[0].TightDescription)
	}
}

func TestConsolidateSessions_GeneratesTightDescription(t *testing.T) {
	svc, _ := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{summarize: func(content string, maxLen int) (string, error) {
		switch maxLen {
		case sessionBodyMaxChars:
			return "session body summary", nil
		case 100:
			if content != "session body summary" {
				return "", fmt.Errorf("expected tight description input to be summary body, got %q", content)
			}
			return "session tight description from summarizer", nil
		default:
			return "", fmt.Errorf("unexpected maxLen: %d", maxLen)
		}
	}})
	ctx := context.Background()

	sessID := "sess_consolidate_tight_desc"
	for i := 0; i < 3; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			SessionID:    sessID,
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("consolidate tight event %d", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.ConsolidateSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("expected 1 session summary, got %d", created)
	}

	summaries, err := svc.repo.ListSummaries(ctx, core.ListSummariesOptions{Kind: "session", SessionID: sessID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 session summary, got %d", len(summaries))
	}
	if summaries[0].TightDescription != "session tight description from summarizer" {
		t.Fatalf("expected summarized tight description, got %q", summaries[0].TightDescription)
	}
	if strings.HasPrefix(summaries[0].TightDescription, "Session summary:") {
		t.Fatalf("expected non-fallback tight description, got %q", summaries[0].TightDescription)
	}
}

func TestConsolidateSessions_CreatesEpisode(t *testing.T) {
	svc, _ := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{})
	ctx := context.Background()

	svc.SetIntelligenceProvider(consolidateTestIntelligence{
		consolidate: func(events []core.EventContent, _ []core.MemorySummary) (*core.NarrativeResult, error) {
			if len(events) != 3 {
				t.Fatalf("expected 3 events, got %d", len(events))
			}
			return &core.NarrativeResult{
				Summary:   "session narrative summary",
				TightDesc: "session narrative tight",
				Episode: &core.EpisodeCandidate{
					Title:        "Narrative Episode",
					Body:         "Episode body from narrative",
					Participants: []string{"agent", "user"},
					Outcomes:     []string{"implemented phase 5A"},
					Unresolved:   []string{"wire into phase 5B"},
				},
			}, nil
		},
	})

	sessID := "sess_consolidate_episode"
	for i := 0; i < 3; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			SessionID:    sessID,
			ProjectID:    "proj_episode",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("episode event %d", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.ConsolidateSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("expected 1 session summary, got %d", created)
	}

	episodes, err := svc.repo.ListEpisodes(ctx, core.ListEpisodesOptions{Scope: core.ScopeProject, ProjectID: "proj_episode", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(episodes))
	}
	ep := episodes[0]
	if ep.SessionID != sessID {
		t.Fatalf("expected episode session_id %q, got %q", sessID, ep.SessionID)
	}
	if ep.Title != "Narrative Episode" || ep.Summary != "Episode body from narrative" {
		t.Fatalf("unexpected episode values: %+v", ep)
	}
}

func TestConsolidateSessions_AutoExtractsDecisions(t *testing.T) {
	svc, _ := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{})
	ctx := context.Background()

	svc.SetIntelligenceProvider(consolidateTestIntelligence{
		consolidate: func(_ []core.EventContent, _ []core.MemorySummary) (*core.NarrativeResult, error) {
			return &core.NarrativeResult{
				Summary:      "decision summary",
				TightDesc:    "decision tight",
				KeyDecisions: []string{"Adopt IntelligenceProvider for session consolidation"},
			}, nil
		},
	})

	sessID := "sess_consolidate_decisions"
	var sourceEventID string
	for i := 0; i < 2; i++ {
		evt, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			SessionID:    sessID,
			ProjectID:    "proj_decisions",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("decision event %d", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			sourceEventID = evt.ID
		}
	}

	seeded, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeProject,
		ProjectID:        "proj_decisions",
		Body:             "seed memory linked to source event",
		TightDescription: "seed linked memory",
		SourceEventIDs:   []string{sourceEventID},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.ConsolidateSessions(ctx); err != nil {
		t.Fatal(err)
	}

	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{Type: core.MemoryTypeDecision, Scope: core.ScopeProject, ProjectID: "proj_decisions", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected 1 decision memory, got %d", len(mems))
	}
	if mems[0].Body != "Adopt IntelligenceProvider for session consolidation" {
		t.Fatalf("unexpected decision body: %q", mems[0].Body)
	}
	if mems[0].Metadata[MetaExtractionQuality] != QualityProvisional {
		t.Fatalf("expected extraction quality provisional, got %q", mems[0].Metadata[MetaExtractionQuality])
	}

	seededAfter, err := svc.repo.GetMemory(ctx, seeded.ID)
	if err != nil {
		t.Fatal(err)
	}
	if seededAfter.Metadata[MetaNarrativeIncluded] != "true" {
		t.Fatalf("expected seeded memory narrative_included=true, got %q", seededAfter.Metadata[MetaNarrativeIncluded])
	}
}

func TestConsolidateSessions_AutoExtractsOpenLoops(t *testing.T) {
	svc, _ := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{})
	ctx := context.Background()

	svc.SetIntelligenceProvider(consolidateTestIntelligence{
		consolidate: func(_ []core.EventContent, _ []core.MemorySummary) (*core.NarrativeResult, error) {
			return &core.NarrativeResult{
				Summary:    "open loop summary",
				TightDesc:  "open loop tight",
				Unresolved: []string{"How should we route model-specific narrative prompts?"},
			}, nil
		},
	})

	sessID := "sess_consolidate_open_loops"
	for i := 0; i < 2; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			SessionID:    sessID,
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("open loop event %d", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if _, err := svc.ConsolidateSessions(ctx); err != nil {
		t.Fatal(err)
	}

	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{Type: core.MemoryTypeOpenLoop, Scope: core.ScopeGlobal, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected 1 open_loop memory, got %d", len(mems))
	}
	if mems[0].Body != "How should we route model-specific narrative prompts?" {
		t.Fatalf("unexpected open_loop body: %q", mems[0].Body)
	}
	if mems[0].Metadata[MetaExtractionQuality] != QualityProvisional {
		t.Fatalf("expected extraction quality provisional, got %q", mems[0].Metadata[MetaExtractionQuality])
	}
}

func TestConsolidateSessions_FallsBackOnError(t *testing.T) {
	var summarizeCalls int32
	var consolidateCalls int32
	svc, _ := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{summarize: func(content string, maxLen int) (string, error) {
		atomic.AddInt32(&summarizeCalls, 1)
		if maxLen == sessionBodyMaxChars {
			return "fallback summary body", nil
		}
		if maxLen == 100 {
			return "fallback tight", nil
		}
		return content, nil
	}})
	ctx := context.Background()

	svc.SetIntelligenceProvider(consolidateTestIntelligence{
		callCountPtr: &consolidateCalls,
		consolidate: func(_ []core.EventContent, _ []core.MemorySummary) (*core.NarrativeResult, error) {
			return nil, fmt.Errorf("provider unavailable")
		},
	})

	sessID := "sess_consolidate_fallback"
	for i := 0; i < 2; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			SessionID:    sessID,
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("fallback event %d", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	if _, err := svc.ConsolidateSessions(ctx); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&consolidateCalls) != 1 {
		t.Fatalf("expected 1 consolidate narrative call, got %d", atomic.LoadInt32(&consolidateCalls))
	}
	if atomic.LoadInt32(&summarizeCalls) != 2 {
		t.Fatalf("expected fallback summarize path (2 calls), got %d", atomic.LoadInt32(&summarizeCalls))
	}

	summaries, err := svc.repo.ListSummaries(ctx, core.ListSummariesOptions{Kind: "session", SessionID: sessID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 session summary, got %d", len(summaries))
	}
	if summaries[0].Body != "fallback summary body" || summaries[0].TightDescription != "fallback tight" {
		t.Fatalf("expected fallback summary outputs, got body=%q tight=%q", summaries[0].Body, summaries[0].TightDescription)
	}
}

func TestConsolidateSessions_RunJob(t *testing.T) {
	svc, _ := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{})
	ctx := context.Background()

	svc.SetIntelligenceProvider(consolidateTestIntelligence{
		consolidate: func(_ []core.EventContent, _ []core.MemorySummary) (*core.NarrativeResult, error) {
			return &core.NarrativeResult{
				Summary:      "job consolidate summary",
				TightDesc:    "job consolidate tight",
				KeyDecisions: []string{"Use consolidate_sessions job pathway"},
			}, nil
		},
	})

	for i := 0; i < 3; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			SessionID:    "sess_consolidate_job",
			ProjectID:    "proj_job",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("job consolidate event %d", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	job, err := svc.RunJob(ctx, "consolidate_sessions")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" {
		t.Fatalf("expected completed job, got %+v", job)
	}
	if job.Result["action"] != "consolidate_sessions" {
		t.Fatalf("unexpected job result: %+v", job.Result)
	}

	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{Type: core.MemoryTypeDecision, Scope: core.ScopeProject, ProjectID: "proj_job", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected one decision memory from run job path, got %d", len(mems))
	}
}

func TestBuildTopicSummaries_GroupsByEntity(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	for i, body := range []string{
		"Alice and Bob mapped SQLite migration risks for AMM rollout",
		"Bob asked Alice to verify SQLite WAL settings for AMM",
		"AMM reliability review: Alice and Bob aligned on SQLite recovery",
	} {
		summary := &core.Summary{
			ID:               fmt.Sprintf("sum_topic_group_%d", i),
			Kind:             "leaf",
			Scope:            core.ScopeGlobal,
			Body:             body,
			TightDescription: fmt.Sprintf("leaf %d", i),
			PrivacyLevel:     core.PrivacyPrivate,
			CreatedAt:        time.Now().UTC().Add(time.Duration(i) * time.Second),
			UpdatedAt:        time.Now().UTC().Add(time.Duration(i) * time.Second),
		}
		if err := repo.InsertSummary(ctx, summary); err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.BuildTopicSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("expected 1 topic summary created, got %d", created)
	}

	topics, err := repo.ListSummaries(ctx, core.ListSummariesOptions{Kind: "topic", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(topics) != 1 {
		t.Fatalf("expected 1 topic summary, got %d", len(topics))
	}
	if len(topics[0].SourceSpan.SummaryIDs) != 3 {
		t.Fatalf("expected 3 child summary IDs in source span, got %d", len(topics[0].SourceSpan.SummaryIDs))
	}

	edges, err := repo.GetSummaryChildren(ctx, topics[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 3 {
		t.Fatalf("expected 3 topic->leaf edges, got %d", len(edges))
	}
}

func TestBuildTopicSummaries_Idempotent(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	for i, body := range []string{
		"Alice and Bob reviewed SQLite constraints for AMM",
		"Bob and Alice documented SQLite indexing for AMM",
		"AMM check-in: Alice Bob SQLite backup discussion",
	} {
		summary := &core.Summary{
			ID:               fmt.Sprintf("sum_topic_idempotent_%d", i),
			Kind:             "leaf",
			Scope:            core.ScopeGlobal,
			Body:             body,
			TightDescription: fmt.Sprintf("leaf idempotent %d", i),
			PrivacyLevel:     core.PrivacyPrivate,
			CreatedAt:        time.Now().UTC().Add(time.Duration(i) * time.Second),
			UpdatedAt:        time.Now().UTC().Add(time.Duration(i) * time.Second),
		}
		if err := repo.InsertSummary(ctx, summary); err != nil {
			t.Fatal(err)
		}
	}

	firstCreated, err := svc.BuildTopicSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if firstCreated != 1 {
		t.Fatalf("expected first run to create 1 topic summary, got %d", firstCreated)
	}

	secondCreated, err := svc.BuildTopicSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if secondCreated != 0 {
		t.Fatalf("expected second run to create 0 topic summaries, got %d", secondCreated)
	}

	topics, err := repo.ListSummaries(ctx, core.ListSummariesOptions{Kind: "topic", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(topics) != 1 {
		t.Fatalf("expected exactly 1 topic summary after rerun, got %d", len(topics))
	}
}

func TestBuildTopicSummaries_SkipsSmallGroups(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	for i, body := range []string{
		"Alice and Bob discussed SQLite tuning for AMM",
		"Bob asked Alice about SQLite cache settings in AMM",
	} {
		summary := &core.Summary{
			ID:               fmt.Sprintf("sum_topic_small_%d", i),
			Kind:             "leaf",
			Scope:            core.ScopeGlobal,
			Body:             body,
			TightDescription: fmt.Sprintf("leaf small %d", i),
			PrivacyLevel:     core.PrivacyPrivate,
			CreatedAt:        time.Now().UTC().Add(time.Duration(i) * time.Second),
			UpdatedAt:        time.Now().UTC().Add(time.Duration(i) * time.Second),
		}
		if err := repo.InsertSummary(ctx, summary); err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.BuildTopicSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("expected 0 topic summaries for small group, got %d", created)
	}

	topics, err := repo.ListSummaries(ctx, core.ListSummariesOptions{Kind: "topic", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(topics) != 0 {
		t.Fatalf("expected no topic summaries, got %d", len(topics))
	}
}

func TestBuildTopicSummaries_RunJob(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	for i, body := range []string{
		"Alice and Bob aligned SQLite safeguards for AMM",
		"Bob and Alice validated SQLite restore flow for AMM",
		"AMM postmortem captured by Alice and Bob on SQLite",
	} {
		summary := &core.Summary{
			ID:               fmt.Sprintf("sum_topic_job_%d", i),
			Kind:             "leaf",
			Scope:            core.ScopeGlobal,
			Body:             body,
			TightDescription: fmt.Sprintf("leaf job %d", i),
			PrivacyLevel:     core.PrivacyPrivate,
			CreatedAt:        time.Now().UTC().Add(time.Duration(i) * time.Second),
			UpdatedAt:        time.Now().UTC().Add(time.Duration(i) * time.Second),
		}
		if err := repo.InsertSummary(ctx, summary); err != nil {
			t.Fatal(err)
		}
	}

	job, err := svc.RunJob(ctx, "build_topic_summaries")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" {
		t.Fatalf("expected completed job, got %+v", job)
	}
	if job.Result["action"] != "build_topic_summaries" {
		t.Fatalf("unexpected job result: %+v", job.Result)
	}
	if job.Result["summaries_created"] != "1" {
		t.Fatalf("expected summaries_created=1, got %+v", job.Result)
	}
}

func TestBuildTopicSummaries_UsesBatchedIntelligence(t *testing.T) {
	var summarizeCalls int32
	var topicBatchCalls int32
	svc, repo := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{summarize: func(content string, maxLen int) (string, error) {
		atomic.AddInt32(&summarizeCalls, 1)
		return "unexpected topic fallback", nil
	}})
	ctx := context.Background()

	svc.SetIntelligenceProvider(consolidateTestIntelligence{
		topicBatchCallCountPtr: &topicBatchCalls,
		summarizeTopicBatches: func(topics []core.TopicChunk) ([]core.CompressionResult, error) {
			if len(topics) != 1 {
				t.Fatalf("expected 1 topic chunk, got %d", len(topics))
			}
			return []core.CompressionResult{{
				Index:            topics[0].Index,
				Body:             "batched topic body",
				TightDescription: "batched topic tight",
			}}, nil
		},
	})

	for i, body := range []string{
		"Alice and Bob mapped SQLite migration risks for AMM rollout",
		"Bob asked Alice to verify SQLite WAL settings for AMM",
		"AMM reliability review: Alice and Bob aligned on SQLite recovery",
	} {
		summary := &core.Summary{
			ID:               fmt.Sprintf("sum_topic_batch_%d", i),
			Kind:             "leaf",
			Scope:            core.ScopeGlobal,
			Body:             body,
			TightDescription: fmt.Sprintf("leaf batch %d", i),
			PrivacyLevel:     core.PrivacyPrivate,
			CreatedAt:        time.Now().UTC().Add(time.Duration(i) * time.Second),
			UpdatedAt:        time.Now().UTC().Add(time.Duration(i) * time.Second),
		}
		if err := repo.InsertSummary(ctx, summary); err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.BuildTopicSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("expected 1 topic summary created, got %d", created)
	}
	if atomic.LoadInt32(&topicBatchCalls) != 1 {
		t.Fatalf("expected one topic batch call, got %d", atomic.LoadInt32(&topicBatchCalls))
	}
	if atomic.LoadInt32(&summarizeCalls) != 0 {
		t.Fatalf("expected no fallback summarize calls, got %d", atomic.LoadInt32(&summarizeCalls))
	}

	topics, err := repo.ListSummaries(ctx, core.ListSummariesOptions{Kind: "topic", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(topics) != 1 {
		t.Fatalf("expected 1 topic summary, got %d", len(topics))
	}
	if topics[0].Body != "batched topic body" || topics[0].TightDescription != "batched topic tight" {
		t.Fatalf("expected batched topic outputs, got body=%q tight=%q", topics[0].Body, topics[0].TightDescription)
	}
}

func TestBuildTopicSummaries_FallsBackWhenBatchTopicFails(t *testing.T) {
	var bodyCalls int32
	var tightCalls int32
	var topicBatchCalls int32
	svc, repo := testServiceAndRepoWithSummarizer(t, consolidateTestSummarizer{summarize: func(content string, maxLen int) (string, error) {
		switch maxLen {
		case topicBodyMaxChars:
			atomic.AddInt32(&bodyCalls, 1)
			return "fallback topic body", nil
		case 100:
			atomic.AddInt32(&tightCalls, 1)
			return "fallback topic tight", nil
		default:
			return content, nil
		}
	}})
	ctx := context.Background()

	svc.SetIntelligenceProvider(consolidateTestIntelligence{
		topicBatchCallCountPtr: &topicBatchCalls,
		summarizeTopicBatches: func([]core.TopicChunk) ([]core.CompressionResult, error) {
			return nil, fmt.Errorf("topic batch failure")
		},
	})

	for i, body := range []string{
		"Alice and Bob reviewed SQLite constraints for AMM",
		"Bob and Alice documented SQLite indexing for AMM",
		"AMM check-in: Alice Bob SQLite backup discussion",
	} {
		summary := &core.Summary{
			ID:               fmt.Sprintf("sum_topic_fallback_%d", i),
			Kind:             "leaf",
			Scope:            core.ScopeGlobal,
			Body:             body,
			TightDescription: fmt.Sprintf("leaf fallback %d", i),
			PrivacyLevel:     core.PrivacyPrivate,
			CreatedAt:        time.Now().UTC().Add(time.Duration(i) * time.Second),
			UpdatedAt:        time.Now().UTC().Add(time.Duration(i) * time.Second),
		}
		if err := repo.InsertSummary(ctx, summary); err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.BuildTopicSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("expected 1 topic summary created, got %d", created)
	}
	if atomic.LoadInt32(&topicBatchCalls) != 1 {
		t.Fatalf("expected one failed topic batch call, got %d", atomic.LoadInt32(&topicBatchCalls))
	}
	if atomic.LoadInt32(&bodyCalls) != 1 || atomic.LoadInt32(&tightCalls) != 1 {
		t.Fatalf("expected fallback summarize calls body=1 tight=1, got body=%d tight=%d", atomic.LoadInt32(&bodyCalls), atomic.LoadInt32(&tightCalls))
	}

	topics, err := repo.ListSummaries(ctx, core.ListSummariesOptions{Kind: "topic", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(topics) != 1 {
		t.Fatalf("expected 1 topic summary, got %d", len(topics))
	}
	if topics[0].Body != "fallback topic body" || topics[0].TightDescription != "fallback topic tight" {
		t.Fatalf("expected fallback topic outputs, got body=%q tight=%q", topics[0].Body, topics[0].TightDescription)
	}
}

// ---------------------------------------------------------------------------
// Ingestion policy
// ---------------------------------------------------------------------------

func TestIngestionPolicy_Ignore(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	now := time.Now().UTC()
	err := repo.InsertIngestionPolicy(ctx, &core.IngestionPolicy{
		ID:          "pol_ign",
		PatternType: "source",
		Pattern:     "noisy_system",
		Mode:        "ignore",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatal(err)
	}

	evt := &core.Event{
		Kind:         "message",
		SourceSystem: "noisy_system",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "this should be ignored",
		OccurredAt:   now,
	}

	result, err := svc.IngestEvent(ctx, evt)
	if err != nil {
		t.Fatal(err)
	}
	// The event is returned but not persisted.
	if result == nil {
		t.Fatal("expected non-nil event returned")
	}

	// Verify it was NOT stored.
	events, err := svc.repo.ListEvents(ctx, core.ListEventsOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Content == "this should be ignored" {
			t.Error("event with ignore policy should not be persisted")
		}
	}
}

func TestIngestionPolicy_ReadOnly(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	now := time.Now().UTC()
	err := repo.InsertIngestionPolicy(ctx, &core.IngestionPolicy{
		ID:          "pol_ro",
		PatternType: "source",
		Pattern:     "readonly_system",
		Mode:        "read_only",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatal(err)
	}

	evt := &core.Event{
		Kind:         "message",
		SourceSystem: "readonly_system",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "this should be stored but not reflected",
		OccurredAt:   now,
	}

	ingest, createMem, err := svc.ShouldIngest(ctx, evt)
	if err != nil {
		t.Fatal(err)
	}
	if !ingest {
		t.Error("expected ingest=true for read_only policy")
	}
	if createMem {
		t.Error("expected createMemory=false for read_only policy")
	}

	// The event should still be ingested (stored in history).
	result, err := svc.IngestEvent(ctx, evt)
	if err != nil {
		t.Fatal(err)
	}
	if result.ID == "" {
		t.Error("expected event to be stored with an ID")
	}
}

func TestIngestionPolicy_Default(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	evt := &core.Event{
		Kind:         "message",
		SourceSystem: "normal_system",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "normal event",
		OccurredAt:   time.Now().UTC(),
	}

	ingest, createMem, err := svc.ShouldIngest(ctx, evt)
	if err != nil {
		t.Fatal(err)
	}
	if !ingest {
		t.Error("expected ingest=true with no policy (default full)")
	}
	if !createMem {
		t.Error("expected createMemory=true with no policy (default full)")
	}
}

func TestIngestionPolicy_ExplicitPolicyOverridesNoiseHeuristic(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	err := repo.InsertIngestionPolicy(ctx, &core.IngestionPolicy{
		ID:          "pol_full_noise_source",
		PatternType: "source",
		Pattern:     "noise-source",
		Mode:        "full",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatal(err)
	}

	evt := &core.Event{
		Kind:         "tool_result",
		SourceSystem: "noise-source",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "go test ./...\n=== RUN   TestX\n--- PASS: TestX (0.00s)",
		OccurredAt:   now,
	}

	ingest, createMem, err := svc.ShouldIngest(ctx, evt)
	if err != nil {
		t.Fatal(err)
	}
	if !ingest || !createMem {
		t.Fatalf("expected explicit full policy to win; got ingest=%t createMemory=%t", ingest, createMem)
	}

	stored, err := svc.IngestEvent(ctx, evt)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Metadata != nil {
		if stored.Metadata["ingestion_mode"] == "read_only" {
			t.Fatalf("expected heuristic not to downgrade explicit full policy event, metadata=%#v", stored.Metadata)
		}
		if stored.Metadata["ingestion_reason"] != "" || stored.Metadata["noise_kind"] != "" {
			t.Fatalf("expected no noise metadata when explicit policy matches, metadata=%#v", stored.Metadata)
		}
	}
}

func TestIngestionPolicy_NoiseHeuristicDowngradesConservativeCases(t *testing.T) {
	testCases := []struct {
		name      string
		event     *core.Event
		noiseKind string
	}{
		{
			name: "tool_result kind",
			event: &core.Event{
				Kind:         "tool_result",
				SourceSystem: "test",
				PrivacyLevel: core.PrivacyPrivate,
				Content:      "{}",
			},
			noiseKind: "tool_result",
		},
		{
			name: "amm tool call",
			event: &core.Event{
				Kind:         "tool_call",
				SourceSystem: "test",
				PrivacyLevel: core.PrivacyPrivate,
				Content:      `{"tool":"amm_recall","arguments":{"query":"preferences"}}`,
			},
			noiseKind: "amm_self_reference",
		},
		{
			name: "large json blob",
			event: &core.Event{
				Kind:         "message",
				SourceSystem: "test",
				PrivacyLevel: core.PrivacyPrivate,
				Content:      `{"records":[{"line":"` + strings.Repeat("a", 2400) + `"}]}`,
			},
			noiseKind: "json_blob",
		},
		{
			name: "build output dump",
			event: &core.Event{
				Kind:         "message",
				SourceSystem: "test",
				PrivacyLevel: core.PrivacyPrivate,
				Content:      "=== RUN   TestAlpha\n=== RUN   TestBeta\n--- PASS: TestAlpha (0.00s)\n--- FAIL: TestBeta (0.00s)\nerror: compile failed\nFAIL\tgithub.com/example/project\t0.123s",
			},
			noiseKind: "build_or_test_log",
		},
		{
			name: "grep style dump",
			event: &core.Event{
				Kind:         "message",
				SourceSystem: "test",
				PrivacyLevel: core.PrivacyPrivate,
				Content:      "internal/a.go:10:func A()\ninternal/b.go:11:func B()\ninternal/c.go:12:func C()\ninternal/d.go:13:func D()\ninternal/e.go:14:func E()\ninternal/f.go:15:func F()\ninternal/g.go:16:func G()\ninternal/h.go:17:func H()",
			},
			noiseKind: "listing_or_diff_dump",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := testServiceAndRepo(t)
			ctx := context.Background()

			evt := *tc.event
			evt.OccurredAt = time.Now().UTC()

			ingest, createMem, err := svc.ShouldIngest(ctx, &evt)
			if err != nil {
				t.Fatal(err)
			}
			if !ingest || createMem {
				t.Fatalf("expected noisy unmatched event to downgrade to read_only; got ingest=%t createMemory=%t", ingest, createMem)
			}
			if evt.Metadata["ingestion_mode"] != "read_only" {
				t.Fatalf("expected ingestion_mode=read_only, got metadata=%#v", evt.Metadata)
			}
			if evt.Metadata["ingestion_reason"] != "noise_filter" {
				t.Fatalf("expected ingestion_reason=noise_filter, got metadata=%#v", evt.Metadata)
			}
			if evt.Metadata["noise_kind"] != tc.noiseKind {
				t.Fatalf("expected noise_kind=%s, got metadata=%#v", tc.noiseKind, evt.Metadata)
			}
		})
	}
}

func TestIngestionPolicy_NoiseHeuristicKeepsNormalProseFull(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	evt := &core.Event{
		Kind:         "message_user",
		SourceSystem: "normal_system",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "I prefer concise responses and want to track decisions from this sprint.",
		OccurredAt:   time.Now().UTC(),
	}

	ingest, createMem, err := svc.ShouldIngest(ctx, evt)
	if err != nil {
		t.Fatal(err)
	}
	if !ingest || !createMem {
		t.Fatalf("expected normal prose to stay full; got ingest=%t createMemory=%t", ingest, createMem)
	}
	if evt.Metadata != nil && evt.Metadata["ingestion_mode"] == "read_only" {
		t.Fatalf("expected no noise downgrade metadata for normal prose, got %#v", evt.Metadata)
	}
}

func TestReflect_SkipsNoiseDowngradedEvents(t *testing.T) {
	summarizer := reflectTestSummarizer{extract: func(_ string) ([]core.MemoryCandidate, error) {
		return []core.MemoryCandidate{{
			Type:             core.MemoryTypeFact,
			Body:             "build output should not become memory",
			TightDescription: "skip downgraded noise",
			Confidence:       0.9,
		}}, nil
	}}
	svc, _ := testServiceAndRepoWithSummarizer(t, summarizer)
	ctx := context.Background()

	evt, err := svc.IngestEvent(ctx, &core.Event{
		Kind:         "tool_result",
		SourceSystem: "test",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "tool output that should be preserved in history only",
		OccurredAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if evt.Metadata["ingestion_mode"] != "read_only" {
		t.Fatalf("expected noisy event to be tagged read_only, got metadata=%#v", evt.Metadata)
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("expected reflect to skip downgraded noisy event, created=%d", created)
	}

	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 0 {
		t.Fatalf("expected no memories from downgraded noisy event, got %#v", mems)
	}
}

func TestReflect_ProcessesNoiseEventsWithLLMSummarizer(t *testing.T) {
	ctx := context.Background()
	var extractionCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		extractionCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"[]"}}]}`))
	}))
	t.Cleanup(server.Close)

	llm := NewLLMSummarizer(server.URL, "test-key", "test-model")
	svc, _ := testServiceAndRepoWithSummarizer(t, llm)
	if !svc.hasLLMSummarizer {
		t.Fatal("expected service to detect LLM summarizer")
	}

	evt, err := svc.IngestEvent(ctx, &core.Event{
		Kind:         "tool_result",
		SourceSystem: "test",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "tool output that should be filtered by summarizer",
		OccurredAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if evt.Metadata["ingestion_mode"] != "read_only" {
		t.Fatalf("expected noisy event to be tagged read_only, got metadata=%#v", evt.Metadata)
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("expected no memories from empty LLM extraction result, got %d", created)
	}
	if extractionCalls.Load() == 0 {
		t.Fatal("expected reflect to process read_only event with LLM summarizer")
	}
}

func TestReflect_SkipsNoiseEventsWithoutLLMSummarizer(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	evt, err := svc.IngestEvent(ctx, &core.Event{
		Kind:         "tool_result",
		SourceSystem: "test",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "tool output should stay history-only without llm summarizer",
		OccurredAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if evt.Metadata["ingestion_mode"] != "read_only" {
		t.Fatalf("expected noisy event to be tagged read_only, got metadata=%#v", evt.Metadata)
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 {
		t.Fatalf("expected reflect to skip read_only event without llm summarizer, created=%d", created)
	}

	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 0 {
		t.Fatalf("expected no memories from skipped read_only event, got %#v", mems)
	}
}

func TestReflect_SkipsTriagedNoiseEvents(t *testing.T) {
	summarizer := reflectTestSummarizer{extract: func(content string) ([]core.MemoryCandidate, error) {
		return []core.MemoryCandidate{{
			Type:             core.MemoryTypeFact,
			Body:             content,
			TightDescription: content,
			Confidence:       0.9,
		}}, nil
	}}
	svc, repo := testServiceAndRepoWithSummarizer(t, summarizer)
	svc.hasLLMSummarizer = true
	svc.SetIntelligenceProvider(consolidateTestIntelligence{
		triage: func(events []core.EventContent) (map[int]core.TriageDecision, error) {
			decisions := make(map[int]core.TriageDecision, len(events))
			for _, evt := range events {
				if strings.Contains(strings.ToLower(evt.Content), "heartbeat") {
					decisions[evt.Index] = core.TriageSkip
					continue
				}
				decisions[evt.Index] = core.TriageReflect
			}
			return decisions, nil
		},
	})
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	events := []core.Event{
		{ID: "evt_triage_noise", Kind: "message_user", SourceSystem: "test", PrivacyLevel: core.PrivacyPrivate, Content: "heartbeat status update", OccurredAt: now, IngestedAt: now},
		{ID: "evt_triage_signal", Kind: "message_user", SourceSystem: "test", PrivacyLevel: core.PrivacyPrivate, Content: "We decided to use Postgres for production", OccurredAt: now, IngestedAt: now},
	}
	for i := range events {
		if err := repo.InsertEvent(ctx, &events[i]); err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.Reflect(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 {
		t.Fatalf("expected only one reflected memory after triage skip, got %d", created)
	}

	mems, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Type: core.MemoryTypeFact, Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) != 1 {
		t.Fatalf("expected 1 active memory, got %d", len(mems))
	}
	if !strings.Contains(strings.ToLower(mems[0].Body), "decided") {
		t.Fatalf("expected non-noise memory body, got %q", mems[0].Body)
	}
}

func TestRemember_MergesNearExactDuplicate(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	first, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "postgres is primary database for production workloads",
		TightDescription: "postgres is primary db",
		Confidence:       0.6,
	})
	if err != nil {
		t.Fatal(err)
	}

	merged, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "postgres is primary database for production workloads services",
		TightDescription: "postgres remains primary db",
		Confidence:       0.95,
	})
	if err != nil {
		t.Fatal(err)
	}
	if merged == nil {
		t.Fatal("expected non-nil merged memory")
	}
	if merged.ID != first.ID {
		t.Fatalf("expected duplicate to merge into keeper %s, got %s", first.ID, merged.ID)
	}
	if merged.Confidence != 0.95 {
		t.Fatalf("expected keeper confidence to upgrade to 0.95, got %.2f", merged.Confidence)
	}

	active, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Type:   core.MemoryTypeFact,
		Status: core.MemoryStatusActive,
		Limit:  10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("expected exactly 1 active memory after merge, got %d", len(active))
	}
}

func TestRemember_AllowsDistinctMemories(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "postgres powers analytics workloads",
		TightDescription: "postgres for analytics",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "redis handles ephemeral cache state",
		TightDescription: "redis cache layer",
	})
	if err != nil {
		t.Fatal(err)
	}

	active, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Type:   core.MemoryTypeFact,
		Status: core.MemoryStatusActive,
		Limit:  10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 {
		t.Fatalf("expected 2 active distinct memories, got %d", len(active))
	}
}

func TestRemember_ExplicitSupersedesSkipsDedup(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	base, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "kubernetes target cluster is alpha",
		TightDescription: "k8s alpha",
	})
	if err != nil {
		t.Fatal(err)
	}

	newer, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "kubernetes target cluster is alpha now with hardened settings",
		TightDescription: "k8s alpha hardened",
		Supersedes:       base.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if newer.ID == base.ID {
		t.Fatalf("expected explicit supersession to insert new memory, got merged keeper %s", newer.ID)
	}

	updatedBase, err := svc.repo.GetMemory(ctx, base.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedBase.Status != core.MemoryStatusSuperseded {
		t.Fatalf("expected base memory to be superseded, got %s", updatedBase.Status)
	}
	if updatedBase.SupersededBy != newer.ID {
		t.Fatalf("expected base superseded_by=%s, got %s", newer.ID, updatedBase.SupersededBy)
	}
}

func TestRemember_MergesSourceEventIDs(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	first, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypePreference,
		Body:             "user prefers concise responses with direct action items",
		TightDescription: "prefers concise responses",
		SourceEventIDs:   []string{"evt-1", "evt-2"},
	})
	if err != nil {
		t.Fatal(err)
	}

	merged, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypePreference,
		Body:             "user prefers concise responses with direct action items always",
		TightDescription: "concise responses preference",
		SourceEventIDs:   []string{"evt-2", "evt-3"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if merged.ID != first.ID {
		t.Fatalf("expected source IDs merge to keep %s, got %s", first.ID, merged.ID)
	}

	got := make(map[string]bool, len(merged.SourceEventIDs))
	for _, id := range merged.SourceEventIDs {
		got[id] = true
	}
	for _, want := range []string{"evt-1", "evt-2", "evt-3"} {
		if !got[want] {
			t.Fatalf("expected merged source_event_ids to include %s, got %#v", want, merged.SourceEventIDs)
		}
	}
}

func TestEmbeddingDedup_CatchesParaphrase(t *testing.T) {
	provider := staticEmbeddingProvider{
		model:   "test-model",
		vectors: map[string][]float32{},
	}
	svc, repo := testServiceAndRepoWithEmbeddingProvider(t, provider)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	first := &core.Memory{
		ID:               "mem_embedding_first",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "PostgreSQL is the primary datastore for production transactions",
		TightDescription: "postgres primary datastore for production",
		Confidence:       0.7,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	second := &core.Memory{
		ID:               "mem_embedding_second",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "Production writes persist in Postgres as the system of record",
		TightDescription: "postgres system of record for production writes",
		Confidence:       0.8,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		CreatedAt:        now.Add(time.Second),
		UpdatedAt:        now.Add(time.Second),
	}
	if err := repo.InsertMemory(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertMemory(ctx, second); err != nil {
		t.Fatal(err)
	}

	if err := repo.UpsertEmbedding(ctx, &core.EmbeddingRecord{ObjectID: first.ID, ObjectKind: "memory", Model: provider.Model(), Vector: []float32{1, 0}, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertEmbedding(ctx, &core.EmbeddingRecord{ObjectID: second.ID, ObjectKind: "memory", Model: provider.Model(), Vector: []float32{0.98, 0.02}, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}

	candidate := &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "Use Postgres as the canonical database for production writes",
		TightDescription: "postgres canonical production database",
		Confidence:       0.95,
		Importance:       0.6,
	}
	provider.vectors[buildMemoryEmbeddingText(candidate)] = []float32{0.97, 0.03}
	svc.embeddingProvider = provider

	merged, err := svc.Remember(ctx, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if merged.ID != second.ID {
		t.Fatalf("expected embedding dedup to merge into %s, got %s", second.ID, merged.ID)
	}

	active, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Type: core.MemoryTypeFact, Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 {
		t.Fatalf("expected no new active memory on embedding dedup merge, got %d", len(active))
	}
}

func TestEmbeddingDedup_NoopWithoutProvider(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i, body := range []string{
		"PostgreSQL is the primary datastore for production transactions",
		"Production writes persist in Postgres as the system of record",
	} {
		mem := &core.Memory{
			ID:               fmt.Sprintf("mem_embedding_noprov_%d", i),
			Type:             core.MemoryTypeFact,
			Scope:            core.ScopeGlobal,
			Body:             body,
			TightDescription: body,
			Confidence:       0.8,
			Importance:       0.5,
			PrivacyLevel:     core.PrivacyPrivate,
			Status:           core.MemoryStatusActive,
			CreatedAt:        now.Add(time.Duration(i) * time.Second),
			UpdatedAt:        now.Add(time.Duration(i) * time.Second),
		}
		if err := repo.InsertMemory(ctx, mem); err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "Use Postgres as the canonical database for production writes",
		TightDescription: "postgres canonical production database",
		Confidence:       0.95,
		Importance:       0.6,
	})
	if err != nil {
		t.Fatal(err)
	}

	if strings.HasPrefix(created.ID, "mem_embedding_noprov_") {
		t.Fatalf("expected remember to create a new memory without embedding provider, got %s", created.ID)
	}

	active, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Type: core.MemoryTypeFact, Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 3 {
		t.Fatalf("expected new memory insertion without embedding dedup, got %d active", len(active))
	}
}

func TestEmbeddingDedup_FallsBackOnEmbedFailure(t *testing.T) {
	svc, repo := testServiceAndRepoWithEmbeddingProvider(t, failingEmbeddingProvider{})
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i, body := range []string{
		"PostgreSQL is the primary datastore for production transactions",
		"Production writes persist in Postgres as the system of record",
	} {
		mem := &core.Memory{
			ID:               fmt.Sprintf("mem_embedding_fail_%d", i),
			Type:             core.MemoryTypeFact,
			Scope:            core.ScopeGlobal,
			Body:             body,
			TightDescription: body,
			Confidence:       0.8,
			Importance:       0.5,
			PrivacyLevel:     core.PrivacyPrivate,
			Status:           core.MemoryStatusActive,
			CreatedAt:        now.Add(time.Duration(i) * time.Second),
			UpdatedAt:        now.Add(time.Duration(i) * time.Second),
		}
		if err := repo.InsertMemory(ctx, mem); err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "Use Postgres as the canonical database for production writes",
		TightDescription: "postgres canonical production database",
		Confidence:       0.95,
		Importance:       0.6,
	})
	if err != nil {
		t.Fatal(err)
	}

	if strings.HasPrefix(created.ID, "mem_embedding_fail_") {
		t.Fatalf("expected fallback insertion on embed failure, got %s", created.ID)
	}

	active, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Type: core.MemoryTypeFact, Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 3 {
		t.Fatalf("expected embed failure fallback to insert new memory, got %d active", len(active))
	}
}

// ---------------------------------------------------------------------------
// Supersession
// ---------------------------------------------------------------------------

func TestSupersession_Remember(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	memA, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "Go version is 1.21",
		TightDescription: "Go 1.21",
	})
	if err != nil {
		t.Fatal(err)
	}

	memB, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "Go version is 1.22",
		TightDescription: "Go 1.22",
		Supersedes:       memA.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if memB.Supersedes != memA.ID {
		t.Errorf("expected memB.Supersedes=%s, got %s", memA.ID, memB.Supersedes)
	}

	// Verify A is now superseded.
	updatedA, err := svc.repo.GetMemory(ctx, memA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedA.Status != core.MemoryStatusSuperseded {
		t.Errorf("expected A status=superseded, got %s", updatedA.Status)
	}
	if updatedA.SupersededBy != memB.ID {
		t.Errorf("expected A.SupersededBy=%s, got %s", memB.ID, updatedA.SupersededBy)
	}
}

func TestSupersession_UpdateMemoryHandlesSupersedes(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	memA, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "runtime is go1.21",
		TightDescription: "go1.21",
	})
	if err != nil {
		t.Fatal(err)
	}

	memB, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "runtime is go1.22",
		TightDescription: "go1.22",
	})
	if err != nil {
		t.Fatal(err)
	}

	memB.Supersedes = memA.ID
	if _, err := svc.UpdateMemory(ctx, memB); err != nil {
		t.Fatal(err)
	}

	updatedA, err := svc.repo.GetMemory(ctx, memA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedA.Status != core.MemoryStatusSuperseded {
		t.Fatalf("expected memory A status superseded, got %s", updatedA.Status)
	}
	if updatedA.SupersededBy != memB.ID {
		t.Fatalf("expected memory A superseded_by %s, got %s", memB.ID, updatedA.SupersededBy)
	}
}

func TestSupersession_RecallFilters(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	memA, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "deployment target is kubernetes cluster alpha",
		TightDescription: "deploy to k8s alpha",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "deployment target is kubernetes cluster beta",
		TightDescription: "deploy to k8s beta",
		Supersedes:       memA.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	archived, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "deployment target is kubernetes cluster archived",
		TightDescription: "deploy archived",
		Status:           core.MemoryStatusArchived,
		Confidence:       0.9,
	})
	if err != nil {
		t.Fatal(err)
	}

	retracted, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "deployment target is kubernetes cluster retracted",
		TightDescription: "deploy retracted",
		Status:           core.MemoryStatusRetracted,
		Confidence:       0.9,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.Recall(ctx, "deployment kubernetes", core.RecallOptions{
		Mode: core.RecallModeFacts,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, item := range result.Items {
		if item.ID == memA.ID {
			t.Error("superseded memory A should not appear in recall results")
		}
		if item.ID == archived.ID {
			t.Error("archived memory should not appear in recall results")
		}
		if item.ID == retracted.ID {
			t.Error("retracted memory should not appear in recall results")
		}
	}
}

// ---------------------------------------------------------------------------
// Repair / CheckIntegrity
// ---------------------------------------------------------------------------

func TestCheckIntegrity_Clean(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	// Add valid data.
	_, err := svc.IngestEvent(ctx, &core.Event{
		Kind:         "message",
		SourceSystem: "test",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "clean integrity test event",
		OccurredAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "clean integrity test memory",
		TightDescription: "integrity test",
	})
	if err != nil {
		t.Fatal(err)
	}

	report, err := svc.CheckIntegrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Issues != 0 {
		t.Errorf("expected 0 issues, got %d: %v", report.Issues, report.Details)
	}
}

func TestCheckIntegrity_BrokenSupersession(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	// Create a memory that supersedes a non-existent memory.
	now := time.Now().UTC()
	mem := &core.Memory{
		ID:               "mem_broken",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "broken supersession pointer",
		TightDescription: "broken",
		Confidence:       0.8,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		Supersedes:       "mem_nonexistent",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := svc.repo.InsertMemory(ctx, mem); err != nil {
		t.Fatal(err)
	}

	report, err := svc.CheckIntegrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Issues == 0 {
		t.Error("expected at least 1 issue for broken supersession")
	}

	// Verify the details mention supersession.
	foundDetail := false
	for _, d := range report.Details {
		if strings.Contains(d, "supersession") {
			foundDetail = true
			break
		}
	}
	if !foundDetail {
		t.Errorf("expected supersession issue in details, got %v", report.Details)
	}
}

// ---------------------------------------------------------------------------
// ExplainRecall
// ---------------------------------------------------------------------------

func TestExplainRecall(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "Terraform manages infrastructure as code",
		TightDescription: "Terraform IaC",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.ExplainRecall(ctx, "Terraform infrastructure", mem.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Verify required fields are present.
	for _, key := range []string{"query", "item_id", "item_kind", "signal_breakdown", "final_score"} {
		if _, ok := result[key]; !ok {
			t.Errorf("expected key %q in ExplainRecall result", key)
		}
	}

	breakdown, ok := result["signal_breakdown"].(SignalBreakdown)
	if !ok {
		t.Fatalf("expected signal_breakdown to be SignalBreakdown, got %T", result["signal_breakdown"])
	}
	if breakdown.FinalScore < 0 || breakdown.FinalScore > 1 {
		t.Errorf("expected final_score in [0,1], got %f", breakdown.FinalScore)
	}
	if result["item_kind"] != "memory" {
		t.Errorf("expected item_kind=memory, got %v", result["item_kind"])
	}
}

func TestRecallAmbient_SemanticReranksLexicalCandidates(t *testing.T) {
	query := "terraform infrastructure"
	provider := staticEmbeddingProvider{
		model: "test-model",
		vectors: map[string][]float32{
			query: {1, 0},
		},
	}

	svc, repo := testServiceAndRepoWithEmbeddingProvider(t, provider)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	lowSemantic := &core.Memory{
		ID:               "mem_sem_low",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "terraform infrastructure terraform infrastructure rollout notes",
		TightDescription: "Terraform infra lexical-heavy",
		Confidence:       0.9,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	highSemantic := &core.Memory{
		ID:               "mem_sem_high",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "terraform infrastructure deployment notes",
		TightDescription: "Terraform infra semantic-best",
		Confidence:       0.9,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.InsertMemory(ctx, lowSemantic); err != nil {
		t.Fatalf("insert low semantic memory: %v", err)
	}
	if err := repo.InsertMemory(ctx, highSemantic); err != nil {
		t.Fatalf("insert high semantic memory: %v", err)
	}

	if err := repo.UpsertEmbedding(ctx, &core.EmbeddingRecord{ObjectID: lowSemantic.ID, ObjectKind: "memory", Model: "test-model", Vector: []float32{0, 1}, CreatedAt: now}); err != nil {
		t.Fatalf("upsert low semantic embedding: %v", err)
	}
	if err := repo.UpsertEmbedding(ctx, &core.EmbeddingRecord{ObjectID: highSemantic.ID, ObjectKind: "memory", Model: "test-model", Vector: []float32{1, 0}, CreatedAt: now}); err != nil {
		t.Fatalf("upsert high semantic embedding: %v", err)
	}

	noSemanticSvc := New(repo, svc.dbPath, nil, nil)
	withoutSemantic, err := noSemanticSvc.Recall(ctx, query, core.RecallOptions{Mode: core.RecallModeFacts, Limit: 2})
	if err != nil {
		t.Fatalf("facts recall without semantic: %v", err)
	}
	if len(withoutSemantic.Items) < 2 {
		t.Fatalf("expected two lexical matches without semantic, got %d", len(withoutSemantic.Items))
	}
	if withoutSemantic.Items[0].ID != lowSemantic.ID {
		t.Fatalf("expected lexical ordering to start with %s, got %s", lowSemantic.ID, withoutSemantic.Items[0].ID)
	}

	withSemantic, err := svc.Recall(ctx, query, core.RecallOptions{Mode: core.RecallModeFacts, Limit: 2})
	if err != nil {
		t.Fatalf("facts recall with semantic: %v", err)
	}
	if len(withSemantic.Items) < 2 {
		t.Fatalf("expected two matches with semantic, got %d", len(withSemantic.Items))
	}
	if withSemantic.Items[0].ID != highSemantic.ID {
		t.Fatalf("expected semantic rerank to promote %s, got %s", highSemantic.ID, withSemantic.Items[0].ID)
	}
}

func TestRecallHybrid_FindsEmbeddingCandidatesNotInFTS(t *testing.T) {
	query := "what are the gotchas and constraints"
	provider := staticEmbeddingProvider{
		model: "test-model",
		vectors: map[string][]float32{
			query: {1, 0},
		},
	}

	svc, repo := testServiceAndRepoWithEmbeddingProvider(t, provider)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	mem := &core.Memory{
		ID:               "mem_hybrid_embedding_only",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "Pure Go driver modernc.org/sqlite used for database access",
		TightDescription: "SQLite driver choice",
		Confidence:       0.9,
		Importance:       0.7,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.InsertMemory(ctx, mem); err != nil {
		t.Fatalf("insert memory: %v", err)
	}
	if err := repo.UpsertEmbedding(ctx, &core.EmbeddingRecord{ObjectID: mem.ID, ObjectKind: "memory", Model: provider.Model(), Vector: []float32{1, 0}, CreatedAt: now}); err != nil {
		t.Fatalf("upsert embedding: %v", err)
	}

	result, err := svc.Recall(ctx, query, core.RecallOptions{Mode: core.RecallModeHybrid, Limit: 10})
	if err != nil {
		t.Fatalf("hybrid recall: %v", err)
	}

	found := false
	for _, item := range result.Items {
		if item.ID == mem.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected embedding-only memory %s in hybrid recall results, got %#v", mem.ID, result.Items)
	}
}

func TestRecallHybrid_MergesFTSAndEmbeddingCandidates(t *testing.T) {
	query := "sqlite build requirement checklist"
	provider := staticEmbeddingProvider{
		model: "test-model",
		vectors: map[string][]float32{
			query: {1, 0},
		},
	}

	svc, repo := testServiceAndRepoWithEmbeddingProvider(t, provider)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	ftsMem := &core.Memory{
		ID:               "mem_hybrid_fts",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "SQLite build requirement checklist and setup notes",
		TightDescription: "SQLite build checklist",
		Confidence:       0.9,
		Importance:       0.6,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	embOnlyMem := &core.Memory{
		ID:               "mem_hybrid_embedding",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "Pure Go driver modernc.org/sqlite used for database access",
		TightDescription: "SQLite driver choice",
		Confidence:       0.9,
		Importance:       0.7,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		CreatedAt:        now.Add(time.Second),
		UpdatedAt:        now.Add(time.Second),
	}
	if err := repo.InsertMemory(ctx, ftsMem); err != nil {
		t.Fatalf("insert fts memory: %v", err)
	}
	if err := repo.InsertMemory(ctx, embOnlyMem); err != nil {
		t.Fatalf("insert embedding-only memory: %v", err)
	}
	if err := repo.UpsertEmbedding(ctx, &core.EmbeddingRecord{ObjectID: embOnlyMem.ID, ObjectKind: "memory", Model: provider.Model(), Vector: []float32{1, 0}, CreatedAt: now}); err != nil {
		t.Fatalf("upsert embedding: %v", err)
	}

	result, err := svc.Recall(ctx, query, core.RecallOptions{Mode: core.RecallModeHybrid, Limit: 10})
	if err != nil {
		t.Fatalf("hybrid recall: %v", err)
	}

	counts := map[string]int{}
	for _, item := range result.Items {
		counts[item.ID]++
	}
	if counts[ftsMem.ID] != 1 {
		t.Fatalf("expected FTS memory %s exactly once, got count=%d results=%#v", ftsMem.ID, counts[ftsMem.ID], result.Items)
	}
	if counts[embOnlyMem.ID] != 1 {
		t.Fatalf("expected embedding memory %s exactly once, got count=%d results=%#v", embOnlyMem.ID, counts[embOnlyMem.ID], result.Items)
	}
}

func TestRecallHybrid_NoEmbeddingSearchWithoutProvider(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "SQLite build requirement checklist",
		TightDescription: "SQLite build checklist",
		Confidence:       0.9,
	})
	if err != nil {
		t.Fatalf("remember memory: %v", err)
	}

	result, err := svc.Recall(ctx, "sqlite build requirement checklist", core.RecallOptions{Mode: core.RecallModeHybrid, Limit: 10})
	if err != nil {
		t.Fatalf("hybrid recall without embedding provider: %v", err)
	}

	found := false
	for _, item := range result.Items {
		if item.ID == mem.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected lexical memory %s in hybrid recall without embeddings, got %#v", mem.ID, result.Items)
	}
}

func TestExplainRecall_IncludesSemanticSignalWhenEmbeddingsAvailable(t *testing.T) {
	query := "postgres durability"
	provider := staticEmbeddingProvider{
		model: "test-model",
		vectors: map[string][]float32{
			query: {1, 0},
		},
	}
	svc, repo := testServiceAndRepoWithEmbeddingProvider(t, provider)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "Postgres offers strong durability guarantees",
		TightDescription: "Postgres durability",
	})
	if err != nil {
		t.Fatalf("remember memory: %v", err)
	}
	now := time.Now().UTC()
	if err := repo.UpsertEmbedding(ctx, &core.EmbeddingRecord{ObjectID: mem.ID, ObjectKind: "memory", Model: "test-model", Vector: []float32{1, 0}, CreatedAt: now}); err != nil {
		t.Fatalf("upsert embedding: %v", err)
	}

	result, err := svc.ExplainRecall(ctx, query, mem.ID)
	if err != nil {
		t.Fatalf("explain recall: %v", err)
	}

	breakdown, ok := result["signal_breakdown"].(SignalBreakdown)
	if !ok {
		t.Fatalf("expected signal_breakdown type SignalBreakdown, got %T", result["signal_breakdown"])
	}
	if breakdown.Semantic <= 0 {
		t.Fatalf("expected positive semantic contribution, got %f", breakdown.Semantic)
	}
}

// ---------------------------------------------------------------------------
// Expand summary hierarchy
// ---------------------------------------------------------------------------

func TestExpandSummaryHierarchy(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	// Ingest events to get real event IDs.
	var eventIDs []string
	for i := 0; i < 3; i++ {
		evt, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("expand hierarchy event %d", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
		eventIDs = append(eventIDs, evt.ID)
	}

	// Create a summary with source_span pointing to the events.
	now := time.Now().UTC()
	sum := &core.Summary{
		ID:               "sum_expand_test",
		Kind:             "leaf",
		Scope:            core.ScopeGlobal,
		Title:            "Test Expand Summary",
		Body:             "body of the summary",
		TightDescription: "expand test",
		PrivacyLevel:     core.PrivacyPrivate,
		SourceSpan:       core.SourceSpan{EventIDs: eventIDs},
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.InsertSummary(ctx, sum); err != nil {
		t.Fatal(err)
	}

	// Also create summary_edges for the events.
	for i, eid := range eventIDs {
		edge := &core.SummaryEdge{
			ParentSummaryID: sum.ID,
			ChildKind:       "event",
			ChildID:         eid,
			EdgeOrder:       i,
		}
		if err := repo.InsertSummaryEdge(ctx, edge); err != nil {
			t.Fatal(err)
		}
	}

	// Call Expand.
	result, err := svc.Expand(ctx, sum.ID, "summary", core.ExpandOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if result.Summary == nil {
		t.Fatal("expected non-nil Summary in expand result")
	}
	if result.Summary.ID != sum.ID {
		t.Errorf("expected summary ID %s, got %s", sum.ID, result.Summary.ID)
	}

	// The events should be returned (via edges or source_span).
	if len(result.Events) != 3 {
		t.Errorf("expected 3 events from expand, got %d", len(result.Events))
	}
}

// ---------------------------------------------------------------------------
// ExtractClaims
// ---------------------------------------------------------------------------

func TestExtractClaims(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Subject:          "amm",
		Body:             "amm uses SQLite for storage",
		TightDescription: "amm uses SQLite",
	})
	if err != nil {
		t.Fatal(err)
	}

	created, err := svc.ExtractClaims(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created < 1 {
		t.Fatalf("expected at least 1 claim created, got %d", created)
	}

	// Verify a claim with predicate "uses" exists.
	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range mems {
		claims, err := svc.repo.ListClaimsByMemory(ctx, m.ID)
		if err != nil {
			continue
		}
		for _, c := range claims {
			if c.Predicate == "uses" {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("expected a claim with predicate 'uses' after ExtractClaims")
	}
}

func TestExtractClaims_SkipsExisting(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Subject:          "amm",
		Body:             "amm uses SQLite for storage",
		TightDescription: "amm uses SQLite",
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := svc.ExtractClaims(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first < 1 {
		t.Fatalf("expected first ExtractClaims to create >= 1, got %d", first)
	}

	second, err := svc.ExtractClaims(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second != 0 {
		t.Errorf("expected second ExtractClaims to create 0 (already extracted), got %d", second)
	}
}

func TestExtractClaims_DecisionBodyUsesChosenOptionOnly(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeDecision,
		Subject:          "database",
		Body:             "Decision: use SQLite for persistence\nWhy: simpler local setup and fewer moving parts",
		TightDescription: "Use SQLite for persistence",
		Confidence:       0.9,
	})
	if err != nil {
		t.Fatal(err)
	}

	created, err := svc.ExtractClaims(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created < 1 {
		t.Fatalf("expected at least 1 claim created, got %d", created)
	}

	claims, err := svc.repo.ListClaimsByMemory(ctx, mem.ID)
	if err != nil {
		t.Fatal(err)
	}

	for _, claim := range claims {
		if claim.Predicate != "decided" {
			continue
		}
		if claim.ObjectValue != "use SQLite for persistence" {
			t.Fatalf("expected decided claim object to be chosen option only, got %q", claim.ObjectValue)
		}
		if claim.Metadata["subject"] != "database" {
			t.Fatalf("expected subject metadata to use memory subject, got %q", claim.Metadata["subject"])
		}
		return
	}

	t.Fatal("expected a decided claim for decision-style body")
}

// ---------------------------------------------------------------------------
// FormEpisodes
// ---------------------------------------------------------------------------

func TestFormEpisodes(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	sessID := "ep-sess"
	var eventIDs []string
	for i := 0; i < 5; i++ {
		evt, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			SessionID:    sessID,
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("episode formation event %d about testing", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
		eventIDs = append(eventIDs, evt.ID)
	}

	created, err := svc.FormEpisodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created < 1 {
		t.Fatalf("expected at least 1 episode created, got %d", created)
	}

	// Verify the episode has the correct session_id and source_span.
	episodes, err := svc.repo.ListEpisodes(ctx, core.ListEpisodesOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ep := range episodes {
		if ep.SessionID == sessID {
			found = true
			// Verify all event IDs are in the source span.
			spanSet := make(map[string]bool, len(ep.SourceSpan.EventIDs))
			for _, eid := range ep.SourceSpan.EventIDs {
				spanSet[eid] = true
			}
			for _, eid := range eventIDs {
				if !spanSet[eid] {
					t.Errorf("expected event %s in episode source_span", eid)
				}
			}
			break
		}
	}
	if !found {
		t.Error("expected an episode with session_id 'ep-sess'")
	}
}

func TestFormEpisodes_SkipsExisting(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	sessID := "epsessdedup"
	for i := 0; i < 3; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			SessionID:    sessID,
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("dedup episode event %d with epsessdedup session", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	first, err := svc.FormEpisodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first < 1 {
		t.Fatalf("expected first FormEpisodes to create >= 1, got %d", first)
	}

	second, err := svc.FormEpisodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second != 0 {
		t.Errorf("expected second FormEpisodes to create 0 (already formed), got %d", second)
	}
}

// ---------------------------------------------------------------------------
// DecayStaleMemories
// ---------------------------------------------------------------------------

func TestDecayStaleMemories_Fresh(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "fresh memory that should not decay",
		TightDescription: "fresh memory",
	})
	if err != nil {
		t.Fatal(err)
	}

	decayed, err := svc.DecayStaleMemories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if decayed != 0 {
		t.Errorf("expected 0 decayed for a fresh memory, got %d", decayed)
	}
}

func TestDecayStaleMemories_Stale(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypePreference,
		Body:             "stale memory that should decay",
		TightDescription: "stale memory",
		Importance:       0.5,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Manually set timestamps to 60 days ago so the memory appears stale.
	// UpdateMemory does not update created_at, so we use raw SQL for that column.
	sixtyDaysAgo := time.Now().UTC().Add(-60 * 24 * time.Hour)
	sixtyDaysAgoStr := sixtyDaysAgo.Format(time.RFC3339)
	mem.UpdatedAt = sixtyDaysAgo
	obs := sixtyDaysAgo
	mem.ObservedAt = &obs
	if err := repo.UpdateMemory(ctx, mem); err != nil {
		t.Fatal(err)
	}
	// Also set created_at via raw SQL since UpdateMemory doesn't touch it.
	_, err = repo.ExecContext(ctx, "UPDATE memories SET created_at=? WHERE id=?", sixtyDaysAgoStr, mem.ID)
	if err != nil {
		t.Fatal(err)
	}

	decayed, err := svc.DecayStaleMemories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if decayed < 1 {
		t.Fatalf("expected at least 1 decayed for a stale memory, got %d", decayed)
	}

	// Verify the memory was actually modified (importance reduced or archived).
	updated, err := repo.GetMemory(ctx, mem.ID)
	if err != nil {
		t.Fatal(err)
	}
	importanceReduced := updated.Importance < 0.5
	archived := updated.Status == core.MemoryStatusArchived
	if !importanceReduced && !archived {
		t.Errorf("expected stale memory to have reduced importance or be archived; importance=%f status=%s",
			updated.Importance, updated.Status)
	}
}

// ---------------------------------------------------------------------------
// DetectContradictions
// ---------------------------------------------------------------------------

func TestDetectContradictions(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	// Remember two conflicting memories.
	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Subject:          "amm",
		Body:             "amm uses SQLite for persistence",
		TightDescription: "amm uses SQLite",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Subject:          "amm",
		Body:             "amm uses Postgres for persistence",
		TightDescription: "amm uses Postgres",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Extract claims so the contradiction detector has something to compare.
	claimsCreated, err := svc.ExtractClaims(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if claimsCreated < 2 {
		t.Fatalf("expected at least 2 claims extracted, got %d", claimsCreated)
	}

	found, err := svc.DetectContradictions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if found < 1 {
		t.Fatalf("expected at least 1 contradiction detected, got %d", found)
	}

	// Verify a contradiction memory was created.
	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Type:   core.MemoryTypeContradiction,
		Status: core.MemoryStatusActive,
		Limit:  100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) < 1 {
		t.Error("expected at least one contradiction memory to be created")
	}
}

func TestDetectContradictions_SupersedesOlderConflictingMemory(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	older := &core.Memory{
		ID:               "mem_contradiction_older",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Subject:          "amm",
		Body:             "amm uses sqlite",
		TightDescription: "amm sqlite",
		Confidence:       0.8,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		CreatedAt:        now.Add(-2 * time.Hour),
		UpdatedAt:        now.Add(-2 * time.Hour),
	}
	newer := &core.Memory{
		ID:               "mem_contradiction_newer",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Subject:          "amm",
		Body:             "amm uses postgres",
		TightDescription: "amm postgres",
		Confidence:       0.9,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.InsertMemory(ctx, older); err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertMemory(ctx, newer); err != nil {
		t.Fatal(err)
	}

	if err := repo.InsertClaim(ctx, &core.Claim{ID: "clm_contradiction_old", MemoryID: older.ID, Predicate: "uses", ObjectValue: "sqlite", Confidence: 0.8}); err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertClaim(ctx, &core.Claim{ID: "clm_contradiction_new", MemoryID: newer.ID, Predicate: "uses", ObjectValue: "postgres", Confidence: 0.9}); err != nil {
		t.Fatal(err)
	}

	found, err := svc.DetectContradictions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if found < 1 {
		t.Fatalf("expected contradiction to be found, got %d", found)
	}

	updatedOlder, err := repo.GetMemory(ctx, older.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedOlder.Status != core.MemoryStatusSuperseded {
		t.Fatalf("expected older memory status superseded, got %s", updatedOlder.Status)
	}
	if updatedOlder.SupersededBy != newer.ID {
		t.Fatalf("expected older memory superseded_by=%s, got %s", newer.ID, updatedOlder.SupersededBy)
	}
}

// ---------------------------------------------------------------------------
// MergeDuplicates
// ---------------------------------------------------------------------------

func TestMergeDuplicates(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	// Insert two nearly identical memories directly (bypassing Remember dedup).
	now := time.Now().UTC()
	if err := repo.InsertMemory(ctx, &core.Memory{
		ID:               generateID("mem_"),
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "The deployment pipeline uses GitHub Actions for CI and CD",
		TightDescription: "deployment pipeline uses GitHub Actions CI CD",
		Confidence:       0.8,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.InsertMemory(ctx, &core.Memory{
		ID:               generateID("mem_"),
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "The deployment pipeline uses GitHub Actions for CI and CD workflows",
		TightDescription: "deployment pipeline uses GitHub Actions CI CD workflows",
		Confidence:       0.9,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		CreatedAt:        now.Add(time.Second),
		UpdatedAt:        now.Add(time.Second),
	}); err != nil {
		t.Fatal(err)
	}

	merged, err := svc.MergeDuplicates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if merged < 1 {
		t.Fatalf("expected at least 1 merge, got %d", merged)
	}

	// Verify one memory is now superseded.
	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Status: core.MemoryStatusSuperseded,
		Limit:  100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) < 1 {
		t.Error("expected at least one superseded memory after MergeDuplicates")
	}
}

func TestMergeDuplicates_ManyNearIdenticalMemories(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 24; i++ {
		mem := &core.Memory{
			Type:         core.MemoryTypeFact,
			Scope:        core.ScopeGlobal,
			PrivacyLevel: core.PrivacyPrivate,
			Status:       core.MemoryStatusActive,
			CreatedAt:    now.Add(time.Duration(i) * time.Second),
			UpdatedAt:    now.Add(time.Duration(i) * time.Second),
			Importance:   0.5,
			Confidence:   0.8 + (float64(i) * 0.001),
			Body: fmt.Sprintf(
				"The deployment pipeline uses GitHub Actions for CI and CD with build test package security scan and release promotion stages variant %d",
				i,
			),
			TightDescription: fmt.Sprintf(
				"deployment pipeline uses GitHub Actions CI CD build test package security scan release promotion variant %d",
				i,
			),
		}
		if err := repo.InsertMemory(ctx, mem); err != nil {
			t.Fatal(err)
		}
	}

	merged, err := svc.MergeDuplicates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if merged != 23 {
		t.Fatalf("expected 23 merges for 24 near-identical memories, got %d", merged)
	}

	active, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Status: core.MemoryStatusActive,
		Limit:  100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("expected exactly 1 active memory after dedup, got %d", len(active))
	}

	superseded, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Status: core.MemoryStatusSuperseded,
		Limit:  100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(superseded) != 23 {
		t.Fatalf("expected 23 superseded memories after dedup, got %d", len(superseded))
	}
}

func TestMergeDuplicates_ConvergesAcrossIterations(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 4; i++ {
		mem := &core.Memory{
			Type:         core.MemoryTypeFact,
			Scope:        core.ScopeGlobal,
			PrivacyLevel: core.PrivacyPrivate,
			Status:       core.MemoryStatusActive,
			CreatedAt:    now.Add(time.Duration(i) * time.Second),
			UpdatedAt:    now.Add(time.Duration(i) * time.Second),
			Importance:   0.5,
			Body: fmt.Sprintf(
				"The deployment pipeline uses GitHub Actions for CI and CD with build test package security scan and release promotion stages variant %d",
				i,
			),
			TightDescription: fmt.Sprintf(
				"deployment pipeline uses GitHub Actions CI CD build test package security scan release promotion variant %d",
				i,
			),
			Confidence: 0.9,
		}
		if err := repo.InsertMemory(ctx, mem); err != nil {
			t.Fatal(err)
		}
	}

	merged, err := svc.MergeDuplicates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if merged != 3 {
		t.Fatalf("expected 3 total merges across iterations, got %d", merged)
	}

	active, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Status: core.MemoryStatusActive,
		Limit:  10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("expected convergence to 1 active memory, got %d", len(active))
	}
}

func TestMergeDuplicates_MergesSourceEventIDsIntoKeeper(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()

	first := &core.Memory{
		ID:               "mem_source_event_first",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
		Importance:       0.5,
		Body:             "The service stores durable memories in SQLite and serves recall over FTS5",
		TightDescription: "service stores durable memories in sqlite with fts5 recall",
		Confidence:       0.7,
		SourceEventIDs:   []string{"evt-a", "evt-shared"},
	}
	if err := repo.InsertMemory(ctx, first); err != nil {
		t.Fatal(err)
	}

	keeper := &core.Memory{
		ID:               "mem_source_event_keeper",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		CreatedAt:        now.Add(time.Second),
		UpdatedAt:        now.Add(time.Second),
		Importance:       0.5,
		Body:             "The service stores durable memories in SQLite and serves recall over FTS5 indexes",
		TightDescription: "service stores durable memories in sqlite with fts5 indexes",
		Confidence:       0.95,
		SourceEventIDs:   []string{"evt-b", "evt-shared"},
	}
	if err := repo.InsertMemory(ctx, keeper); err != nil {
		t.Fatal(err)
	}

	merged, err := svc.MergeDuplicates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if merged != 1 {
		t.Fatalf("expected exactly 1 merge, got %d", merged)
	}

	updatedKeeper, err := svc.repo.GetMemory(ctx, keeper.ID)
	if err != nil {
		t.Fatal(err)
	}

	ids := make(map[string]bool, len(updatedKeeper.SourceEventIDs))
	for _, id := range updatedKeeper.SourceEventIDs {
		ids[id] = true
	}
	if !ids["evt-a"] || !ids["evt-b"] || !ids["evt-shared"] {
		t.Fatalf("expected keeper source event IDs to include evt-a, evt-b, evt-shared; got %#v", updatedKeeper.SourceEventIDs)
	}
}

// ---------------------------------------------------------------------------
// jaccardSimilarity
// ---------------------------------------------------------------------------

func TestJaccardSimilarity(t *testing.T) {
	// Identical text should yield 1.0.
	sim := jaccardSimilarity("hello world", "hello world")
	if math.Abs(sim-1.0) > 1e-9 {
		t.Errorf("expected similarity 1.0 for identical text, got %f", sim)
	}

	// Completely different text should yield 0.0.
	sim = jaccardSimilarity("alpha beta gamma", "delta epsilon zeta")
	if sim != 0.0 {
		t.Errorf("expected similarity 0.0 for completely different text, got %f", sim)
	}

	// Partial overlap should be between 0 and 1.
	sim = jaccardSimilarity("the quick brown fox", "the slow brown dog")
	if sim <= 0.0 || sim >= 1.0 {
		t.Errorf("expected similarity between 0 and 1 for partial overlap, got %f", sim)
	}

	// Verify the known value: intersection={the, brown}=2, union={the,quick,brown,fox,slow,dog}=6 => 2/6 ≈ 0.333
	expected := 2.0 / 6.0
	if math.Abs(sim-expected) > 1e-9 {
		t.Errorf("expected similarity %f for partial overlap, got %f", expected, sim)
	}

	// Both empty should yield 1.0.
	sim = jaccardSimilarity("", "")
	if math.Abs(sim-1.0) > 1e-9 {
		t.Errorf("expected similarity 1.0 for two empty strings, got %f", sim)
	}
}
