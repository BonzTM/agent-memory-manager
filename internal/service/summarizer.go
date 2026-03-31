package service

import (
	"context"
	"strings"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

type phraseCue struct {
	memType core.MemoryType
	phrases []string
}

var phraseCues = []phraseCue{
	{
		memType: core.MemoryTypePreference,
		phrases: []string{"prefer", "always use", "don't like", "i like", "i want", "default to", "rather than"},
	},
	{
		memType: core.MemoryTypeDecision,
		phrases: []string{"decided", "we agreed", "going with", "chosen", "settled on", "will use", "switching to"},
	},
	{
		memType: core.MemoryTypeFact,
		phrases: []string{"is a", "works by", "uses", "requires", "depends on", "supports", "runs on"},
	},
	{
		memType: core.MemoryTypeOpenLoop,
		phrases: []string{"todo", "need to", "should look into", "haven't figured out", "remains", "still need", "tbd", "unresolved"},
	},
	{
		memType: core.MemoryTypeConstraint,
		phrases: []string{"must not", "never", "always must", "required to", "cannot", "forbidden"},
	},
}

// HeuristicSummarizer provides a local rule-based fallback for summarization
// and memory extraction.
type HeuristicSummarizer struct{}

// Summarize returns text truncated to maxLen characters.
func (h *HeuristicSummarizer) Summarize(ctx context.Context, text string, maxLen int) (string, error) {
	_ = ctx
	if maxLen <= 0 {
		return "", nil
	}
	if len(text) <= maxLen {
		return text, nil
	}
	return text[:maxLen], nil
}

// ExtractMemoryCandidate applies phrase cues to derive memory candidates from a
// single event. Requires at least 2 phrase cue matches to reduce false positives
// from overly common phrases like "uses" or "is a".
func (h *HeuristicSummarizer) ExtractMemoryCandidate(ctx context.Context, eventContent string) ([]core.MemoryCandidate, error) {
	_ = ctx
	contentLower := strings.ToLower(eventContent)

	// Count total cue matches across all groups and track the first matched type.
	totalMatches := 0
	var firstMatchedType core.MemoryType
	for _, cue := range phraseCues {
		for _, phrase := range cue.phrases {
			if strings.Contains(contentLower, phrase) {
				totalMatches++
				if firstMatchedType == "" {
					firstMatchedType = cue.memType
				}
				break // count at most one match per cue group
			}
		}
	}

	if totalMatches < 2 {
		return nil, nil
	}

	body, err := h.Summarize(ctx, eventContent, 500)
	if err != nil {
		return nil, err
	}

	candidate := core.MemoryCandidate{
		Type:             firstMatchedType,
		Body:             body,
		TightDescription: extractTightDescription(eventContent, 100),
		Confidence:       0.45,
	}

	return []core.MemoryCandidate{candidate}, nil
}

// ExtractMemoryCandidateBatch extracts candidates from multiple events and tags
// each candidate with its source event number.
func (h *HeuristicSummarizer) ExtractMemoryCandidateBatch(ctx context.Context, eventContents []string) ([]core.MemoryCandidate, error) {
	var all []core.MemoryCandidate
	for i, content := range eventContents {
		candidates, err := h.ExtractMemoryCandidate(ctx, content)
		if err != nil {
			return all, err
		}
		for j := range candidates {
			candidates[j].SourceEventNums = []int{i + 1}
		}
		all = append(all, candidates...)
	}
	return all, nil
}
