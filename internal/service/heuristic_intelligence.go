package service

import (
	"context"
	"strings"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

type HeuristicIntelligenceProvider struct {
	*HeuristicSummarizer
}

type summarizerIntelligenceAdapter struct {
	core.Summarizer
}

func NewHeuristicIntelligenceProvider() *HeuristicIntelligenceProvider {
	return &HeuristicIntelligenceProvider{HeuristicSummarizer: &HeuristicSummarizer{}}
}

func NewSummarizerIntelligenceAdapter(summarizer core.Summarizer) core.IntelligenceProvider {
	if summarizer == nil {
		return NewHeuristicIntelligenceProvider()
	}
	if llm, ok := summarizer.(*LLMSummarizer); ok {
		return NewLLMIntelligenceProvider(llm, nil)
	}
	if _, ok := summarizer.(*HeuristicSummarizer); ok {
		return NewHeuristicIntelligenceProvider()
	}
	return &summarizerIntelligenceAdapter{Summarizer: summarizer}
}

func (s *summarizerIntelligenceAdapter) AnalyzeEvents(ctx context.Context, events []core.EventContent) (*core.AnalysisResult, error) {
	contents := make([]string, 0, len(events))
	for _, evt := range events {
		contents = append(contents, evt.Content)
	}

	memories, err := s.ExtractMemoryCandidateBatch(ctx, contents)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	entities := make([]core.EntityCandidate, 0)
	for _, content := range contents {
		for _, name := range ExtractEntities(content) {
			key := strings.ToLower(strings.TrimSpace(name))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			entities = append(entities, core.EntityCandidate{CanonicalName: name, Type: "topic"})
		}
	}

	return &core.AnalysisResult{
		Memories:      memories,
		Entities:      entities,
		Relationships: []core.RelationshipCandidate{},
		EventQuality:  map[int]string{},
	}, nil
}

func (s *summarizerIntelligenceAdapter) IsLLMBacked() bool {
	return false
}

func (s *summarizerIntelligenceAdapter) ModelName() string {
	return ""
}

func (s *summarizerIntelligenceAdapter) TriageEvents(_ context.Context, events []core.EventContent) (map[int]core.TriageDecision, error) {
	return heuristicTriageEvents(events), nil
}

func (s *summarizerIntelligenceAdapter) ReviewMemories(context.Context, []core.MemoryReview) (*core.ReviewResult, error) {
	return &core.ReviewResult{}, nil
}

func (s *summarizerIntelligenceAdapter) CompressEventBatches(ctx context.Context, chunks []core.EventChunk) ([]core.CompressionResult, error) {
	results := make([]core.CompressionResult, 0, len(chunks))
	for _, chunk := range chunks {
		joined := strings.Join(chunk.Contents, "\n")
		body, err := s.Summarize(ctx, joined, leafBodyMaxChars)
		if err != nil {
			return nil, err
		}
		tight, err := s.Summarize(ctx, body, 100)
		if err != nil {
			return nil, err
		}
		results = append(results, core.CompressionResult{
			Index:            chunk.Index,
			Body:             body,
			TightDescription: tight,
		})
	}
	return results, nil
}

func (s *summarizerIntelligenceAdapter) SummarizeTopicBatches(ctx context.Context, topics []core.TopicChunk) ([]core.CompressionResult, error) {
	results := make([]core.CompressionResult, 0, len(topics))
	for _, topic := range topics {
		joined := strings.Join(topic.Contents, "\n\n")
		body, err := s.Summarize(ctx, joined, topicBodyMaxChars)
		if err != nil {
			return nil, err
		}
		tight, err := s.Summarize(ctx, joined, 100)
		if err != nil {
			return nil, err
		}
		results = append(results, core.CompressionResult{
			Index:            topic.Index,
			Body:             body,
			TightDescription: tight,
		})
	}
	return results, nil
}

func (s *summarizerIntelligenceAdapter) ConsolidateNarrative(ctx context.Context, events []core.EventContent, existingMemories []core.MemorySummary) (*core.NarrativeResult, error) {
	_ = existingMemories

	var bodyBuilder strings.Builder
	for i, evt := range events {
		if i > 0 {
			bodyBuilder.WriteByte('\n')
		}
		bodyBuilder.WriteString(evt.Content)
	}

	body, err := s.Summarize(ctx, bodyBuilder.String(), sessionBodyMaxChars)
	if err != nil {
		return nil, err
	}
	tight, err := s.Summarize(ctx, body, 100)
	if err != nil {
		return nil, err
	}

	return &core.NarrativeResult{
		Summary:       body,
		TightDesc:     tight,
		Episode:       nil,
		KeyDecisions:  []string{},
		Unresolved:    []string{},
		ResolvedLoops: []string{},
	}, nil
}

func (h *HeuristicIntelligenceProvider) AnalyzeEvents(ctx context.Context, events []core.EventContent) (*core.AnalysisResult, error) {
	contents := make([]string, 0, len(events))
	for _, evt := range events {
		contents = append(contents, evt.Content)
	}

	memories, err := h.ExtractMemoryCandidateBatch(ctx, contents)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	entities := make([]core.EntityCandidate, 0)
	for _, content := range contents {
		for _, name := range ExtractEntities(content) {
			key := strings.ToLower(strings.TrimSpace(name))
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			entities = append(entities, core.EntityCandidate{
				CanonicalName: name,
				Type:          "topic",
			})
		}
	}

	return &core.AnalysisResult{
		Memories:      memories,
		Entities:      entities,
		Relationships: []core.RelationshipCandidate{},
		EventQuality:  map[int]string{},
	}, nil
}

func (h *HeuristicIntelligenceProvider) IsLLMBacked() bool {
	return false
}

func (h *HeuristicIntelligenceProvider) ModelName() string {
	return ""
}

func (h *HeuristicIntelligenceProvider) TriageEvents(_ context.Context, events []core.EventContent) (map[int]core.TriageDecision, error) {
	return heuristicTriageEvents(events), nil
}

func (h *HeuristicIntelligenceProvider) ReviewMemories(context.Context, []core.MemoryReview) (*core.ReviewResult, error) {
	return &core.ReviewResult{}, nil
}

func (h *HeuristicIntelligenceProvider) CompressEventBatches(ctx context.Context, chunks []core.EventChunk) ([]core.CompressionResult, error) {
	results := make([]core.CompressionResult, 0, len(chunks))
	for _, chunk := range chunks {
		joined := strings.Join(chunk.Contents, "\n")
		body, err := h.Summarize(ctx, joined, leafBodyMaxChars)
		if err != nil {
			return nil, err
		}
		tight, err := h.Summarize(ctx, body, 100)
		if err != nil {
			return nil, err
		}
		results = append(results, core.CompressionResult{
			Index:            chunk.Index,
			Body:             body,
			TightDescription: tight,
		})
	}
	return results, nil
}

func (h *HeuristicIntelligenceProvider) SummarizeTopicBatches(ctx context.Context, topics []core.TopicChunk) ([]core.CompressionResult, error) {
	results := make([]core.CompressionResult, 0, len(topics))
	for _, topic := range topics {
		joined := strings.Join(topic.Contents, "\n\n")
		body, err := h.Summarize(ctx, joined, topicBodyMaxChars)
		if err != nil {
			return nil, err
		}
		tight, err := h.Summarize(ctx, joined, 100)
		if err != nil {
			return nil, err
		}
		results = append(results, core.CompressionResult{
			Index:            topic.Index,
			Body:             body,
			TightDescription: tight,
		})
	}
	return results, nil
}

func (h *HeuristicIntelligenceProvider) ConsolidateNarrative(ctx context.Context, events []core.EventContent, existingMemories []core.MemorySummary) (*core.NarrativeResult, error) {
	_ = existingMemories

	var bodyBuilder strings.Builder
	for i, evt := range events {
		if i > 0 {
			bodyBuilder.WriteByte('\n')
		}
		bodyBuilder.WriteString(evt.Content)
	}

	body, err := h.Summarize(ctx, bodyBuilder.String(), sessionBodyMaxChars)
	if err != nil {
		return nil, err
	}

	tight, err := h.Summarize(ctx, body, 100)
	if err != nil {
		return nil, err
	}

	return &core.NarrativeResult{
		Summary:       body,
		TightDesc:     tight,
		Episode:       nil,
		KeyDecisions:  []string{},
		Unresolved:    []string{},
		ResolvedLoops: []string{},
	}, nil
}

func heuristicTriageEvents(events []core.EventContent) map[int]core.TriageDecision {
	decisions := make(map[int]core.TriageDecision, len(events))
	for i, evt := range events {
		index := evt.Index
		if index <= 0 {
			index = i + 1
		}
		decisions[index] = heuristicTriageContent(evt.Content)
	}
	return decisions
}

func heuristicTriageContent(content string) core.TriageDecision {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return core.TriageSkip
	}
	if len([]rune(trimmed)) < 20 {
		return core.TriageSkip
	}

	lower := strings.ToLower(trimmed)
	if isHeuristicNoiseContent(lower) {
		return core.TriageSkip
	}
	if isHeuristicHighPriorityContent(lower) {
		return core.TriageHighPriority
	}

	return core.TriageReflect
}

func isHeuristicNoiseContent(lower string) bool {
	for _, needle := range []string{
		"heartbeat",
		"status ping",
		"status check",
		"health check",
		"still alive",
		"empty tool output",
		"tool output: (empty)",
		"tool output: none",
		"tool output: no output",
		"no output from tool",
		"no output",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func isHeuristicHighPriorityContent(lower string) bool {
	for _, needle := range []string{"decided", "prefer", "must not", "important", "always", "never"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}
