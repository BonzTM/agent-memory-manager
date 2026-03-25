package service

import (
	"strings"

	"github.com/bonztm/agent-memory-manager/internal/core"
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
