package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const metaReflectFallbackCount = "reflect_fallback_count"

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
	state, err := s.sourceEventRetryState(ctx, eventIDs)
	if err != nil {
		return false, err
	}
	return state.shouldRetry, nil
}

type sourceEventRetryState struct {
	hasActiveMemory bool
	shouldRetry     bool
}

func (s *AMMService) sourceEventRetryState(ctx context.Context, eventIDs []string) (sourceEventRetryState, error) {
	if len(eventIDs) == 0 {
		return sourceEventRetryState{}, nil
	}
	memories, err := s.repo.ListMemoriesBySourceEventIDs(ctx, eventIDs)
	if err != nil {
		return sourceEventRetryState{}, fmt.Errorf("list memories by source events: %w", err)
	}
	state := sourceEventRetryState{hasActiveMemory: len(memories) > 0}
	for i := range memories {
		if shouldRetryHeuristicMemory(&memories[i]) {
			state.shouldRetry = true
			return state, nil
		}
	}
	return state, nil
}

func eventFallbackCount(evt *core.Event) int {
	if evt == nil {
		return 0
	}
	raw := strings.TrimSpace(evt.Metadata[metaReflectFallbackCount])
	if raw == "" {
		return 0
	}
	count, err := strconv.Atoi(raw)
	if err != nil || count < 0 {
		return 0
	}
	return count
}

func setEventFallbackCount(evt *core.Event, count int) {
	if evt == nil {
		return
	}
	if count <= 0 {
		if evt.Metadata != nil {
			delete(evt.Metadata, metaReflectFallbackCount)
		}
		return
	}
	if evt.Metadata == nil {
		evt.Metadata = make(map[string]string)
	}
	evt.Metadata[metaReflectFallbackCount] = strconv.Itoa(count)
}

func (s *AMMService) recordReflectFallbackAttempt(ctx context.Context, events []core.Event) ([]core.Event, error) {
	retryEvents := make([]core.Event, 0, len(events))
	for i := range events {
		count := eventFallbackCount(&events[i]) + 1
		setEventFallbackCount(&events[i], count)
		if err := s.repo.UpdateEvent(ctx, &events[i]); err != nil {
			return nil, fmt.Errorf("record reflect fallback count for event %s: %w", events[i].ID, err)
		}
		if count < maxHeuristicFallbackRetries {
			retryEvents = append(retryEvents, events[i])
		}
	}
	return retryEvents, nil
}

func (s *AMMService) clearReflectFallbackAttempts(ctx context.Context, events []core.Event) error {
	for i := range events {
		if eventFallbackCount(&events[i]) == 0 {
			continue
		}
		setEventFallbackCount(&events[i], 0)
		if err := s.repo.UpdateEvent(ctx, &events[i]); err != nil {
			return fmt.Errorf("clear reflect fallback count for event %s: %w", events[i].ID, err)
		}
	}
	return nil
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
	count := currentSummaryFallbackCount(summary)
	return method == MethodHeuristic && count > 0 && count < maxHeuristicFallbackRetries
}
