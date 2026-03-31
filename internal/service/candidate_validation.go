package service

import (
	"strings"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	defaultMinConfidenceForCreation = 0.5
	defaultMinImportanceForCreation = 0.3
)

func prepareMemoryCandidate(candidate core.MemoryCandidate) (core.MemoryCandidate, bool) {
	candidate.Subject = strings.TrimSpace(candidate.Subject)
	candidate.Body = strings.TrimSpace(candidate.Body)
	candidate.TightDescription = strings.TrimSpace(candidate.TightDescription)

	if candidate.Body == "" || candidate.TightDescription == "" {
		return candidate, false
	}
	if !isSupportedMemoryType(candidate.Type) {
		return candidate, false
	}
	return candidate, true
}

// passesIntakeQualityGates checks whether a candidate meets the minimum
// confidence and importance thresholds for memory creation.
func passesIntakeQualityGates(candidate core.MemoryCandidate, minConfidence, minImportance float64) bool {
	if candidate.Confidence < minConfidence {
		return false
	}
	importance := importanceForCandidate(candidate)
	if importance < minImportance {
		return false
	}
	return true
}

// candidateSourcedFromLowQualityEvents returns true if EventQuality is
// available and every source event for this candidate is classified as
// "ephemeral" or "noise". When EventQuality is nil or the candidate has no
// source event references, the check is skipped (returns false).
func candidateSourcedFromLowQualityEvents(candidate core.MemoryCandidate, eventQuality map[int]string) bool {
	if len(eventQuality) == 0 || len(candidate.SourceEventNums) == 0 {
		return false
	}
	for _, idx := range candidate.SourceEventNums {
		quality, ok := eventQuality[idx]
		if !ok {
			return false // unknown quality — don't filter
		}
		if quality != "ephemeral" && quality != "noise" {
			return false
		}
	}
	return true
}

func isSupportedMemoryType(t core.MemoryType) bool {
	switch t {
	case core.MemoryTypeIdentity,
		core.MemoryTypePreference,
		core.MemoryTypeFact,
		core.MemoryTypeDecision,
		core.MemoryTypeEpisode,
		core.MemoryTypeTodo,
		core.MemoryTypeRelationship,
		core.MemoryTypeProcedure,
		core.MemoryTypeConstraint,
		core.MemoryTypeIncident,
		core.MemoryTypeArtifact,
		core.MemoryTypeSummary,
		core.MemoryTypeActiveContext,
		core.MemoryTypeOpenLoop,
		core.MemoryTypeAssumption,
		core.MemoryTypeContradiction:
		return true
	default:
		return false
	}
}
