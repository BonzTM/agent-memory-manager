package service

import (
	"context"
	"strings"
	"testing"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestHeuristicSummarizer_ImplementsInterface(t *testing.T) {
	var _ core.Summarizer = (*HeuristicSummarizer)(nil)
}

func TestHeuristicSummarizer_SummarizeTruncates(t *testing.T) {
	h := &HeuristicSummarizer{}
	ctx := context.Background()

	longText := strings.Repeat("abcdef", 40)
	maxLen := 80

	summary, err := h.Summarize(ctx, longText, maxLen)
	if err != nil {
		t.Fatalf("Summarize returned error: %v", err)
	}
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
	if len(summary) > maxLen {
		t.Fatalf("expected summary length <= %d, got %d", maxLen, len(summary))
	}
	if summary != longText[:maxLen] {
		t.Fatalf("expected summary to match truncated source")
	}
}

func TestHeuristicSummarizer_ExtractMemoryCandidate(t *testing.T) {
	h := &HeuristicSummarizer{}
	ctx := context.Background()

	// Two cue matches across groups: "decided" (decision) + "requires" (fact)
	event := "We decided to use PostgreSQL because it requires less maintenance."
	candidates, err := h.ExtractMemoryCandidate(ctx, event)
	if err != nil {
		t.Fatalf("ExtractMemoryCandidate returned error: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected at least one candidate")
	}

	got := candidates[0]
	if got.Type != core.MemoryTypeDecision {
		t.Fatalf("expected candidate type %q, got %q", core.MemoryTypeDecision, got.Type)
	}
	if got.Body == "" {
		t.Fatal("expected candidate body to be populated")
	}
	if got.TightDescription == "" {
		t.Fatal("expected candidate tight description to be populated")
	}
	if got.Confidence <= 0 {
		t.Fatalf("expected positive confidence, got %f", got.Confidence)
	}
}

func TestHeuristicSummarizer_SingleCueNoExtraction(t *testing.T) {
	h := &HeuristicSummarizer{}
	ctx := context.Background()

	// Only one cue match: "uses" alone should NOT extract (requires 2+)
	event := "The service uses a database connection pool."
	candidates, err := h.ExtractMemoryCandidate(ctx, event)
	if err != nil {
		t.Fatalf("ExtractMemoryCandidate returned error: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("expected no candidates for single cue match, got %d", len(candidates))
	}
}

func TestHeuristicSummarizer_TwoCuesCrossGroupExtracts(t *testing.T) {
	h := &HeuristicSummarizer{}
	ctx := context.Background()

	// Two cues from different groups: "prefer" (preference) + "decided" (decision)
	event := "I prefer Go and we decided to use it for the backend."
	candidates, err := h.ExtractMemoryCandidate(ctx, event)
	if err != nil {
		t.Fatalf("ExtractMemoryCandidate returned error: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected candidate for two cue matches across groups")
	}
}

func TestHeuristicSummarizer_ConfidenceIs045(t *testing.T) {
	h := &HeuristicSummarizer{}
	ctx := context.Background()

	event := "We decided to always use structured logging going with slog."
	candidates, err := h.ExtractMemoryCandidate(ctx, event)
	if err != nil {
		t.Fatalf("ExtractMemoryCandidate returned error: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected at least one candidate")
	}
	if candidates[0].Confidence != 0.45 {
		t.Fatalf("expected heuristic confidence 0.45, got %f", candidates[0].Confidence)
	}
}
