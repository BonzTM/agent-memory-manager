//go:build fts5

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/joshd-04/agent-memory-manager/internal/core"
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

	event := "We decided to use PostgreSQL for this service."
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
