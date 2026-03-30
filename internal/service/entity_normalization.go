package service

import (
	"strings"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func normalizeEntityTerm(term string) string {
	return strings.ToLower(strings.TrimSpace(term))
}

func entityMatchesTerm(entity *core.Entity, term string) bool {
	if entity == nil {
		return false
	}
	needle := normalizeEntityTerm(term)
	if needle == "" {
		return false
	}
	if normalizeEntityTerm(entity.CanonicalName) == needle {
		return true
	}
	for _, alias := range entity.Aliases {
		if normalizeEntityTerm(alias) == needle {
			return true
		}
	}
	return false
}

func candidateMatchesContent(candidate core.EntityCandidate, normalizedContent string) bool {
	if strings.TrimSpace(normalizedContent) == "" {
		return false
	}
	canonical := normalizeEntityTerm(candidate.CanonicalName)
	if canonical != "" && strings.Contains(normalizedContent, canonical) {
		return true
	}
	for _, alias := range candidate.Aliases {
		term := normalizeEntityTerm(alias)
		if term == "" {
			continue
		}
		if strings.Contains(normalizedContent, term) {
			return true
		}
	}
	return false
}
