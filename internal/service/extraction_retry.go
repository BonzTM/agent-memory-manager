package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

type analysisMethodProvider interface {
	AnalyzeEventsWithMethod(ctx context.Context, events []core.EventContent) (*core.AnalysisResult, string, error)
}

type batchExtractionMethodProvider interface {
	ExtractMemoryCandidateBatchWithMethod(ctx context.Context, eventContents []string) ([]core.MemoryCandidate, string, error)
}

type narrativeMethodProvider interface {
	ConsolidateNarrativeWithMethod(ctx context.Context, events []core.EventContent, existingMemories []core.MemorySummary) (*core.NarrativeResult, string, error)
}

func analyzeEventsWithMethod(ctx context.Context, provider core.IntelligenceProvider, events []core.EventContent) (*core.AnalysisResult, string, error) {
	if provider == nil {
		return &core.AnalysisResult{}, MethodHeuristic, nil
	}
	if reporter, ok := provider.(analysisMethodProvider); ok {
		return reporter.AnalyzeEventsWithMethod(ctx, events)
	}
	result, err := provider.AnalyzeEvents(ctx, events)
	method := MethodHeuristic
	if provider.IsLLMBacked() {
		method = MethodLLM
	}
	return result, method, err
}

func extractBatchWithMethod(ctx context.Context, provider core.IntelligenceProvider, contents []string) ([]core.MemoryCandidate, string, error) {
	if provider == nil {
		return nil, MethodHeuristic, nil
	}
	if reporter, ok := provider.(batchExtractionMethodProvider); ok {
		return reporter.ExtractMemoryCandidateBatchWithMethod(ctx, contents)
	}
	result, err := provider.ExtractMemoryCandidateBatch(ctx, contents)
	method := MethodHeuristic
	if provider.IsLLMBacked() {
		method = MethodLLM
	}
	return result, method, err
}

func consolidateNarrativeWithMethod(ctx context.Context, provider core.IntelligenceProvider, events []core.EventContent, existingMemories []core.MemorySummary) (*core.NarrativeResult, string, error) {
	if provider == nil {
		return &core.NarrativeResult{}, MethodHeuristic, nil
	}
	if reporter, ok := provider.(narrativeMethodProvider); ok {
		return reporter.ConsolidateNarrativeWithMethod(ctx, events, existingMemories)
	}
	result, err := provider.ConsolidateNarrative(ctx, events, existingMemories)
	method := MethodHeuristic
	if provider.IsLLMBacked() {
		method = MethodLLM
	}
	return result, method, err
}

func (s *AMMService) shouldRetrySourceEvents(ctx context.Context, eventIDs []string) (bool, error) {
	if len(eventIDs) == 0 {
		return false, nil
	}
	memories, err := s.repo.ListMemoriesBySourceEventIDs(ctx, eventIDs)
	if err != nil {
		return false, fmt.Errorf("list memories by source events: %w", err)
	}
	for i := range memories {
		if shouldRetryHeuristicMemory(&memories[i]) {
			return true, nil
		}
	}
	return false, nil
}

func currentSummaryFallbackCount(summary *core.Summary) int {
	if summary == nil {
		return 0
	}
	return fallbackCountFromMetadata(summary.Metadata)
}

func summaryNeedsLLMRetry(summary *core.Summary) bool {
	if summary == nil {
		return false
	}
	method := strings.TrimSpace(summary.Metadata[MetaExtractionMethod])
	return method == MethodHeuristic && currentSummaryFallbackCount(summary) > 0 && currentSummaryFallbackCount(summary) < maxHeuristicFallbackRetries
}
