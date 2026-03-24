package service

import (
	"context"
	"strings"

	"github.com/joshd-04/agent-memory-manager/internal/core"
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

type HeuristicSummarizer struct{}

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

func (h *HeuristicSummarizer) ExtractMemoryCandidate(ctx context.Context, eventContent string) ([]core.MemoryCandidate, error) {
	_ = ctx
	contentLower := strings.ToLower(eventContent)

	var matchedType core.MemoryType
	for _, cue := range phraseCues {
		for _, phrase := range cue.phrases {
			if strings.Contains(contentLower, phrase) {
				matchedType = cue.memType
				break
			}
		}
		if matchedType != "" {
			break
		}
	}

	if matchedType == "" {
		return nil, nil
	}

	body, err := h.Summarize(ctx, eventContent, 500)
	if err != nil {
		return nil, err
	}

	candidate := core.MemoryCandidate{
		Type:             matchedType,
		Body:             body,
		TightDescription: extractTightDescription(eventContent, 100),
		Confidence:       0.6,
	}

	return []core.MemoryCandidate{candidate}, nil
}
