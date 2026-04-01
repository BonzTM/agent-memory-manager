package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func mockChatResponse(content string) string {
	resp := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"message": map[string]string{
					"role":    "assistant",
					"content": content,
				},
			},
		},
	}
	b, _ := json.Marshal(resp)
	return string(b)
}

func testFloat64Ptr(v float64) *float64 {
	return &v
}

func TestLLMSummarizer_Summarize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockChatResponse("This is a concise summary of the text.")))
	}))
	defer srv.Close()

	s := NewLLMSummarizer(srv.URL, "test-key", "test-model")
	ctx := context.Background()

	result, err := s.Summarize(ctx, "A very long piece of text that needs summarization into something shorter and more useful.", 200)
	if err != nil {
		t.Fatalf("Summarize error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty summary")
	}
	if result != "This is a concise summary of the text." {
		t.Fatalf("unexpected summary: %q", result)
	}
}

func TestLLMSummarizer_ExtractMemoryCandidate(t *testing.T) {
	candidates := []core.MemoryCandidate{
		{
			Type:             core.MemoryTypePreference,
			Subject:          "code style",
			Body:             "Josh prefers concise commit messages with imperative mood",
			TightDescription: "Prefers concise imperative commit messages",
			Confidence:       0.9,
		},
		{
			Type:             core.MemoryTypeDecision,
			Subject:          "database",
			Body:             "Decision: use SQLite for the persistence layer\nWhy: simpler local setup and fewer moving parts",
			TightDescription: "Using SQLite for persistence",
			Confidence:       0.85,
		},
	}
	payload, _ := json.Marshal(candidates)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockChatResponse(string(payload))))
	}))
	defer srv.Close()

	s := NewLLMSummarizer(srv.URL, "test-key", "test-model")
	ctx := context.Background()

	result, err := s.ExtractMemoryCandidate(ctx, "Josh said he prefers concise commit messages. We also decided to use SQLite.")
	if err != nil {
		t.Fatalf("ExtractMemoryCandidate error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(result))
	}
	if result[0].Type != core.MemoryTypePreference {
		t.Fatalf("expected first candidate type preference, got %q", result[0].Type)
	}
	if result[0].Subject != "code style" {
		t.Fatalf("expected first candidate subject 'code style', got %q", result[0].Subject)
	}
	if result[1].Type != core.MemoryTypeDecision {
		t.Fatalf("expected second candidate type decision, got %q", result[1].Type)
	}
	if !strings.Contains(result[1].Body, "Decision:") {
		t.Fatalf("expected decision body to preserve explicit decision framing, got %q", result[1].Body)
	}
}

func TestLLMSummarizer_ExtractPromptIncludesDecisionGuidance(t *testing.T) {
	var receivedPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		msgs := body["messages"].([]interface{})
		msg := msgs[0].(map[string]interface{})
		receivedPrompt = msg["content"].(string)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockChatResponse("[]")))
	}))
	defer srv.Close()

	s := NewLLMSummarizer(srv.URL, "test-key", "test-model")
	_, err := s.ExtractMemoryCandidate(context.Background(), "We decided to use SQLite because local setup is simpler")
	if err != nil {
		t.Fatalf("extract prompt error: %v", err)
	}

	for _, want := range []string{
		// Structural sections
		`FILTERING`,
		`BODY QUALITY`,
		`TYPE REFERENCE`,
		// Selectivity framing
		`Return [] for most inputs`,
		`Most events contain nothing worth remembering`,
		// Field quality
		`MUST go beyond tight_description`,
		`Calibrate: 0.95 = explicitly stated by user`,
		// Type guidance
		`decision: a settled architectural or design choice`,
		`open_loop: an unresolved question or blocked work`,
		`constraint: a hard requirement or boundary`,
		`procedure: a non-obvious multi-step workflow`,
		`incident: a notable failure or surprise`,
		`assumption: something believed but not verified`,
	} {
		if !strings.Contains(receivedPrompt, want) {
			t.Fatalf("expected prompt to contain %q, got %q", want, receivedPrompt)
		}
	}
}

func TestLLMSummarizer_ExtractFallsBackOnBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockChatResponse("This is not valid JSON at all")))
	}))
	defer srv.Close()

	s := NewLLMSummarizer(srv.URL, "test-key", "test-model")
	ctx := context.Background()

	result, err := s.ExtractMemoryCandidate(ctx, "I prefer tabs over spaces because Go requires gofmt formatting")
	if err != nil {
		t.Fatalf("expected fallback instead of error: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected heuristic fallback to produce at least one candidate")
	}
}

func TestLLMSummarizer_ExtractFallsBackOnHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer srv.Close()

	s := NewLLMSummarizer(srv.URL, "test-key", "test-model")
	ctx := context.Background()

	result, err := s.ExtractMemoryCandidate(ctx, "We decided to use Go because it requires minimal dependencies")
	if err != nil {
		t.Fatalf("expected fallback instead of error: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected heuristic fallback to produce at least one candidate")
	}
}

func TestLLMSummarizer_SummarizeFallsBackOnHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	s := NewLLMSummarizer(srv.URL, "test-key", "test-model")
	ctx := context.Background()

	result, err := s.Summarize(ctx, "Some text to summarize that is quite long", 200)
	if err != nil {
		t.Fatalf("expected fallback instead of error: %v", err)
	}
	if result != "Some text to summarize that is quite long" {
		t.Fatalf("expected heuristic truncation fallback, got %q", result)
	}
}

func TestLLMSummarizer_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer srv.Close()

	s := NewLLMSummarizer(srv.URL, "test-key", "test-model")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result, err := s.Summarize(ctx, "Some text", 200)
	if err != nil {
		t.Fatalf("expected fallback on timeout, got error: %v", err)
	}
	if result != "Some text" {
		t.Fatalf("expected fallback to return original text, got %q", result)
	}
}

func TestLLMSummarizer_ExtractEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockChatResponse("[]")))
	}))
	defer srv.Close()

	s := NewLLMSummarizer(srv.URL, "test-key", "test-model")
	ctx := context.Background()

	result, err := s.ExtractMemoryCandidate(ctx, "The weather is nice today")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected no candidates from empty LLM response, got %d", len(result))
	}
}

func TestLLMSummarizer_ImplementsInterface(t *testing.T) {
	var _ core.Summarizer = (*LLMSummarizer)(nil)
}

func TestLLMSummarizer_RequestFormat(t *testing.T) {
	var receivedBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("expected /v1/chat/completions, got %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Fatalf("expected Bearer test-key, got %s", auth)
		}
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockChatResponse("summary")))
	}))
	defer srv.Close()

	s := NewLLMSummarizer(srv.URL, "test-key", "gpt-4o-mini")
	ctx := context.Background()
	s.Summarize(ctx, "test", 100)

	if receivedBody["model"] != "gpt-4o-mini" {
		t.Fatalf("expected model gpt-4o-mini, got %v", receivedBody["model"])
	}
	msgs, ok := receivedBody["messages"].([]interface{})
	if !ok || len(msgs) == 0 {
		t.Fatalf("expected messages array, got %v", receivedBody["messages"])
	}
}

func TestLLMSummarizer_ExtractBatchHappyPath(t *testing.T) {
	candidates := []core.MemoryCandidate{
		{Type: core.MemoryTypePreference, Subject: "Go", Body: "Prefers Go for backends", TightDescription: "Prefers Go for backends", Confidence: 0.9, Importance: testFloat64Ptr(0.7), SourceEventNums: []int{1}},
		{Type: core.MemoryTypeDecision, Subject: "SQLite", Body: "Decision: use SQLite for persistence\nWhy: simpler local setup", TightDescription: "Using SQLite for persistence", Confidence: 0.85, Importance: testFloat64Ptr(0.9), SourceEventNums: []int{2}},
	}
	payload, _ := json.Marshal(candidates)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockChatResponse(string(payload))))
	}))
	defer srv.Close()

	s := NewLLMSummarizer(srv.URL, "test-key", "test-model")
	ctx := context.Background()

	result, err := s.ExtractMemoryCandidateBatch(ctx, []string{
		"I prefer Go for backend services",
		"We decided to use SQLite",
	})
	if err != nil {
		t.Fatalf("batch extract error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(result))
	}
	if result[0].Subject != "Go" {
		t.Fatalf("expected subject 'Go', got %q", result[0].Subject)
	}
	if result[0].Importance == nil || *result[0].Importance != 0.7 {
		t.Fatalf("expected importance 0.7, got %#v", result[0].Importance)
	}
	if len(result[0].SourceEventNums) != 1 || result[0].SourceEventNums[0] != 1 {
		t.Fatalf("expected first candidate source event [1], got %#v", result[0].SourceEventNums)
	}
	if len(result[1].SourceEventNums) != 1 || result[1].SourceEventNums[0] != 2 {
		t.Fatalf("expected second candidate source event [2], got %#v", result[1].SourceEventNums)
	}
	if !strings.Contains(result[1].Body, "Why:") {
		t.Fatalf("expected batch decision body to preserve rationale line, got %q", result[1].Body)
	}
}

func TestLLMSummarizer_ExtractBatchPromptIncludesDecisionGuidance(t *testing.T) {
	var receivedPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		msgs := body["messages"].([]interface{})
		msg := msgs[0].(map[string]interface{})
		receivedPrompt = msg["content"].(string)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockChatResponse("[]")))
	}))
	defer srv.Close()

	s := NewLLMSummarizer(srv.URL, "test-key", "test-model")
	_, err := s.ExtractMemoryCandidateBatch(context.Background(), []string{
		"We should maybe use Postgres",
		"We decided to use SQLite because local setup is simpler",
	})
	if err != nil {
		t.Fatalf("extract batch prompt error: %v", err)
	}

	for _, want := range []string{
		`Deduplicate across events`,
		`decision: a settled architectural or design choice`,
		`source_events: array of event numbers (1-indexed) this memory was derived from`,
		`open_loop: an unresolved question`,
		`FILTERING`,
		`Return [] for most inputs`,
	} {
		if !strings.Contains(receivedPrompt, want) {
			t.Fatalf("expected batch prompt to contain %q, got %q", want, receivedPrompt)
		}
	}
}

func TestLLMSummarizer_ExtractBatchFallsBackOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := NewLLMSummarizer(srv.URL, "test-key", "test-model")
	ctx := context.Background()

	result, err := s.ExtractMemoryCandidateBatch(ctx, []string{
		"We decided to use Go because it requires minimal dependencies",
	})
	if err != nil {
		t.Fatalf("expected fallback, got error: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected heuristic fallback to produce candidates")
	}
}

func TestLLMSummarizer_ExtractBatchEmpty(t *testing.T) {
	s := NewLLMSummarizer("http://unused", "key", "model")
	result, err := s.ExtractMemoryCandidateBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty result for nil input, got %d", len(result))
	}
}

func TestLLMSummarizer_ExtractBatchTruncatesLongEvents(t *testing.T) {
	var receivedPrompt string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		msgs := body["messages"].([]interface{})
		msg := msgs[0].(map[string]interface{})
		receivedPrompt = msg["content"].(string)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(mockChatResponse("[]")))
	}))
	defer srv.Close()

	s := NewLLMSummarizer(srv.URL, "test-key", "test-model")
	longContent := make([]byte, 3000)
	for i := range longContent {
		longContent[i] = 'a'
	}
	s.ExtractMemoryCandidateBatch(context.Background(), []string{string(longContent)})

	if len(receivedPrompt) > 7000 {
		t.Fatalf("expected event content to be truncated within prompt, got total prompt length %d", len(receivedPrompt))
	}
	if len(receivedPrompt) < 1000 {
		t.Fatalf("prompt suspiciously short (%d), truncation may have been too aggressive", len(receivedPrompt))
	}
	if strings.Contains(receivedPrompt, strings.Repeat("a", maxEventContentLen+1)) {
		t.Fatal("expected prompt to exclude content beyond maxEventContentLen")
	}
}

func TestLLMSummarizer_ExtractPromptIncludesDurabilityCheck(t *testing.T) {
	prompt := buildMemoryExtractionPrompt([]string{"test event content"}, false)
	want := "will this still matter in 30 days"
	if !strings.Contains(prompt, want) {
		t.Fatalf("expected extraction prompt to contain durability check %q", want)
	}
}
