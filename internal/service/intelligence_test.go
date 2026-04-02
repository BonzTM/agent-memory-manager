package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestHeuristicIntelligence_ImplementsSummarizer(t *testing.T) {
	var _ core.Summarizer = (*HeuristicIntelligenceProvider)(nil)
}

func TestHeuristicIntelligence_AnalyzeEvents(t *testing.T) {
	h := NewHeuristicIntelligenceProvider()

	result, err := h.AnalyzeEvents(context.Background(), []core.EventContent{
		{Index: 1, Content: "We decided to use SQLite because it requires no server."},
		{Index: 2, Content: "OpenAI API integration is next."},
	})
	if err != nil {
		t.Fatalf("AnalyzeEvents error: %v", err)
	}
	if len(result.Memories) == 0 {
		t.Fatal("expected memory candidates from delegated batch extraction")
	}
	if len(result.Entities) == 0 {
		t.Fatal("expected entities from heuristic entity extraction")
	}
	if len(result.Relationships) != 0 {
		t.Fatalf("expected no relationships, got %d", len(result.Relationships))
	}
	if len(result.EventQuality) != 0 {
		t.Fatalf("expected empty event quality, got %d", len(result.EventQuality))
	}
}

func TestHeuristicIntelligence_CompressEventBatches(t *testing.T) {
	h := NewHeuristicIntelligenceProvider()
	results, err := h.CompressEventBatches(context.Background(), []core.EventChunk{
		{Index: 1, Contents: []string{"event one", "event two"}},
		{Index: 2, Contents: []string{"event three"}},
	})
	if err != nil {
		t.Fatalf("CompressEventBatches error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Index != 1 || results[1].Index != 2 {
		t.Fatalf("expected indexed results, got %#v", results)
	}
}

func TestHeuristicIntelligence_SummarizeTopicBatches(t *testing.T) {
	h := NewHeuristicIntelligenceProvider()
	results, err := h.SummarizeTopicBatches(context.Background(), []core.TopicChunk{
		{Index: 4, Title: "Topic A", Contents: []string{"leaf one", "leaf two"}},
	})
	if err != nil {
		t.Fatalf("SummarizeTopicBatches error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Index != 4 {
		t.Fatalf("expected result index 4, got %#v", results[0])
	}
}

func TestTriage_SkipsClearNoise(t *testing.T) {
	h := NewHeuristicIntelligenceProvider()

	decisions, err := h.TriageEvents(context.Background(), []core.EventContent{{Index: 1, Content: "   "}, {Index: 2, Content: "heartbeat"}})
	if err != nil {
		t.Fatalf("TriageEvents error: %v", err)
	}
	if decisions[1] != core.TriageSkip {
		t.Fatalf("expected whitespace event to be skip, got %q", decisions[1])
	}
	if decisions[2] != core.TriageSkip {
		t.Fatalf("expected heartbeat event to be skip, got %q", decisions[2])
	}
}

func TestTriage_PassesClearSignal(t *testing.T) {
	h := NewHeuristicIntelligenceProvider()

	decisions, err := h.TriageEvents(context.Background(), []core.EventContent{{Index: 1, Content: "We decided to use Postgres for production storage."}})
	if err != nil {
		t.Fatalf("TriageEvents error: %v", err)
	}
	if decisions[1] != core.TriageHighPriority {
		t.Fatalf("expected high_priority triage, got %q", decisions[1])
	}
}

func TestTriage_DefaultsToReflect(t *testing.T) {
	h := NewHeuristicIntelligenceProvider()

	decisions, err := h.TriageEvents(context.Background(), []core.EventContent{{Index: 1, Content: "Ran tests and reviewed implementation options for tomorrow."}})
	if err != nil {
		t.Fatalf("TriageEvents error: %v", err)
	}
	if decisions[1] != core.TriageReflect {
		t.Fatalf("expected reflect triage, got %q", decisions[1])
	}
}

func TestTriage_LLMClassifiesBorderline(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockChatResponse(`{"1":"reflect","2":"skip"}`)))
	}))
	defer srv.Close()

	provider := NewLLMIntelligenceProvider(NewLLMSummarizer(srv.URL, "test-key", "test-model", 0), nil)
	decisions, err := provider.TriageEvents(context.Background(), []core.EventContent{{Index: 1, Content: "This may or may not matter later."}, {Index: 2, Content: "tool output: no output"}})
	if err != nil {
		t.Fatalf("TriageEvents error: %v", err)
	}
	if calls.Load() == 0 {
		t.Fatal("expected LLM triage call")
	}
	if decisions[1] != core.TriageReflect {
		t.Fatalf("expected event 1 reflect, got %q", decisions[1])
	}
	if decisions[2] != core.TriageSkip {
		t.Fatalf("expected event 2 skip, got %q", decisions[2])
	}
}

func TestTriage_FallsBackOnLLMFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	provider := NewLLMIntelligenceProvider(NewLLMSummarizer(srv.URL, "test-key", "test-model", 0), nil)
	decisions, err := provider.TriageEvents(context.Background(), []core.EventContent{{Index: 1, Content: "We decided to use Postgres for production storage."}, {Index: 2, Content: "heartbeat"}})
	if err != nil {
		t.Fatalf("expected fallback instead of error: %v", err)
	}
	if decisions[1] != core.TriageHighPriority {
		t.Fatalf("expected fallback high_priority for event 1, got %q", decisions[1])
	}
	if decisions[2] != core.TriageSkip {
		t.Fatalf("expected fallback skip for event 2, got %q", decisions[2])
	}
}

func TestLLMIntelligence_AnalyzeEvents_ReturnsStructuredResult(t *testing.T) {
	payload := map[string]any{
		"memories": []map[string]any{
			{
				"type":              "decision",
				"subject":           "database",
				"body":              "Decision: use SQLite",
				"tight_description": "Using SQLite",
				"confidence":        0.92,
				"source_events":     []int{1},
			},
		},
		"entities": []map[string]any{
			{"canonical_name": "SQLite", "type": "technology", "aliases": []string{"sqlite3"}},
		},
		"relationships": []map[string]any{
			{"from_entity": "AMM", "to_entity": "SQLite", "type": "depends-on", "description": "storage layer"},
		},
		"event_quality": map[string]string{"1": "durable"},
	}
	b, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockChatResponse(string(b))))
	}))
	defer srv.Close()

	provider := NewLLMIntelligenceProvider(NewLLMSummarizer(srv.URL, "test-key", "test-model", 0), nil)
	result, err := provider.AnalyzeEvents(context.Background(), []core.EventContent{{Index: 1, Content: "We decided to use SQLite."}})
	if err != nil {
		t.Fatalf("AnalyzeEvents error: %v", err)
	}
	if len(result.Memories) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(result.Memories))
	}
	if len(result.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(result.Entities))
	}
	if len(result.Relationships) != 1 {
		t.Fatalf("expected 1 relationship, got %d", len(result.Relationships))
	}
	if result.EventQuality[1] != "durable" {
		t.Fatalf("expected event quality durable, got %q", result.EventQuality[1])
	}
}

func TestLLMIntelligence_FallsBackToHeuristic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	provider := NewLLMIntelligenceProvider(NewLLMSummarizer(srv.URL, "test-key", "test-model", 0), nil)
	result, err := provider.AnalyzeEvents(context.Background(), []core.EventContent{{Index: 1, Content: "We decided to use Go because it requires minimal dependencies."}})
	if err != nil {
		t.Fatalf("expected fallback instead of error: %v", err)
	}
	if len(result.Memories) == 0 {
		t.Fatal("expected heuristic fallback memories")
	}
}

func TestLLMIntelligence_ReviewMemories(t *testing.T) {
	payload := map[string]any{
		"promote": []string{"mem_1"},
		"decay":   []string{"mem_2"},
		"archive": []string{},
		"merge": []map[string]string{
			{"keep_id": "mem_1", "merge_id": "mem_3", "reason": "duplicate"},
		},
		"contradictions": []map[string]string{
			{"memory_a": "mem_1", "memory_b": "mem_4", "explanation": "conflict"},
		},
	}
	b, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockChatResponse(string(b))))
	}))
	defer srv.Close()

	provider := NewLLMIntelligenceProvider(NewLLMSummarizer(srv.URL, "test-key", "test-model", 0), nil)
	result, err := provider.ReviewMemories(context.Background(), []core.MemoryReview{{ID: "mem_1", Body: "A"}, {ID: "mem_2", Body: "B"}})
	if err != nil {
		t.Fatalf("ReviewMemories error: %v", err)
	}
	if len(result.Promote) != 1 || result.Promote[0] != "mem_1" {
		t.Fatalf("unexpected promote result: %#v", result.Promote)
	}
	if len(result.Merge) != 1 {
		t.Fatalf("expected merge suggestion, got %#v", result.Merge)
	}
}

func TestLLMIntelligence_ConsolidateNarrative(t *testing.T) {
	payload := map[string]any{
		"summary":           "Session covered implementation details.",
		"tight_description": "Implemented intelligence provider",
		"episode": map[string]any{
			"title": "Phase 2A",
			"body":  "Implemented interface and providers",
		},
		"key_decisions": []string{"Use optional review model routing"},
		"unresolved":    []string{"Wire into phase 2B"},
	}
	b, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockChatResponse(string(b))))
	}))
	defer srv.Close()

	provider := NewLLMIntelligenceProvider(NewLLMSummarizer(srv.URL, "test-key", "test-model", 0), nil)
	result, err := provider.ConsolidateNarrative(
		context.Background(),
		[]core.EventContent{{Index: 1, Content: "Implemented interface"}},
		[]core.MemorySummary{{Type: "decision", TightDescription: "Use interface"}},
	)
	if err != nil {
		t.Fatalf("ConsolidateNarrative error: %v", err)
	}
	if result.Summary == "" || result.TightDesc == "" {
		t.Fatalf("expected summary and tight description, got %#v", result)
	}
	if result.Episode == nil || result.Episode.Title != "Phase 2A" {
		t.Fatalf("expected parsed episode, got %#v", result.Episode)
	}
}

func TestLLMIntelligence_CompressEventBatches_ReturnsMappedResults(t *testing.T) {
	payload := []map[string]any{
		{"index": 2, "body": "chunk two body", "tight_description": "chunk two tight"},
		{"index": 1, "body": "chunk one body", "tight_description": "chunk one tight"},
	}
	b, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockChatResponse(string(b))))
	}))
	defer srv.Close()

	provider := NewLLMIntelligenceProvider(NewLLMSummarizer(srv.URL, "test-key", "test-model", 0), nil)
	results, err := provider.CompressEventBatches(context.Background(), []core.EventChunk{
		{Index: 1, Contents: []string{"event 1", "event 2"}},
		{Index: 2, Contents: []string{"event 3"}},
	})
	if err != nil {
		t.Fatalf("CompressEventBatches error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Index != 1 || results[0].Body != "chunk one body" || results[0].TightDescription != "chunk one tight" {
		t.Fatalf("unexpected result[0]: %#v", results[0])
	}
	if results[1].Index != 2 || results[1].Body != "chunk two body" || results[1].TightDescription != "chunk two tight" {
		t.Fatalf("unexpected result[1]: %#v", results[1])
	}
}

func TestLLMIntelligence_SummarizeTopicBatches_ReturnsMappedResults(t *testing.T) {
	payload := []map[string]any{
		{"index": 7, "body": "topic seven body", "tight_description": "topic seven tight"},
		{"index": 8, "body": "topic eight body", "tight_description": "topic eight tight"},
	}
	b, _ := json.Marshal(payload)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockChatResponse(string(b))))
	}))
	defer srv.Close()

	provider := NewLLMIntelligenceProvider(NewLLMSummarizer(srv.URL, "test-key", "test-model", 0), nil)
	results, err := provider.SummarizeTopicBatches(context.Background(), []core.TopicChunk{
		{Index: 7, Title: "Topic A", Contents: []string{"leaf 1", "leaf 2"}},
		{Index: 8, Title: "Topic B", Contents: []string{"leaf 3"}},
	})
	if err != nil {
		t.Fatalf("SummarizeTopicBatches error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Index != 7 || results[0].Body != "topic seven body" || results[0].TightDescription != "topic seven tight" {
		t.Fatalf("unexpected result[0]: %#v", results[0])
	}
	if results[1].Index != 8 || results[1].Body != "topic eight body" || results[1].TightDescription != "topic eight tight" {
		t.Fatalf("unexpected result[1]: %#v", results[1])
	}
}

func TestAnalyzeEventsPrompt_IncludesConsolidatedSections(t *testing.T) {
	prompt := buildAnalyzeEventsPrompt([]core.EventContent{{
		Index:     1,
		ProjectID: "proj-a",
		SessionID: "sess-a",
		Content:   "We decided to use SQLite and AMM.",
	}})

	for _, want := range []string{"entities", "relationships", "event_quality", "canonical_name", "aliases"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected AnalyzeEvents prompt to contain %q", want)
		}
	}
}

func TestAnalyzeEventsPrompt_IncludesExistingRules(t *testing.T) {
	prompt := buildAnalyzeEventsPrompt([]core.EventContent{{
		Index:   1,
		Content: "We decided to use SQLite.",
	}})

	for _, want := range []string{"FILTERING", "tight_description", "source_events"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("expected AnalyzeEvents prompt to preserve extraction rule %q", want)
		}
	}
}
