package service

import (
	"strings"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func importanceForCandidate(candidate core.MemoryCandidate) float64 {
	if candidate.Importance != nil {
		return clampUnit(*candidate.Importance)
	}

	switch candidate.Type {
	case core.MemoryTypeDecision, core.MemoryTypeConstraint, core.MemoryTypeProcedure, core.MemoryTypeIncident:
		return 0.85
	case core.MemoryTypePreference, core.MemoryTypeOpenLoop, core.MemoryTypeIdentity:
		return 0.75
	case core.MemoryTypeFact, core.MemoryTypeRelationship, core.MemoryTypeAssumption:
		return 0.65
	default:
		return 0.5
	}
}

func shouldUpgradeDuplicateContent(existing *core.Memory, candidate core.Memory, extractionMethod string) bool {
	if existing == nil {
		return false
	}
	if extractionMethod == "llm" && (existing.Metadata == nil || existing.Metadata["extraction_method"] != "llm") {
		return true
	}
	return candidate.Confidence >= existing.Confidence
}

func selectDuplicateKeeper(duplicates []*core.Memory) *core.Memory {
	if len(duplicates) == 0 {
		return nil
	}
	keeper := duplicates[0]
	for _, candidate := range duplicates[1:] {
		if candidate == nil {
			continue
		}
		if candidate.Confidence > keeper.Confidence {
			keeper = candidate
			continue
		}
		if candidate.Confidence == keeper.Confidence && candidate.CreatedAt.After(keeper.CreatedAt) {
			keeper = candidate
		}
	}
	return keeper
}

func findDuplicateActiveMemories(activeMemories []*core.Memory, candidate core.Memory) []*core.Memory {
	duplicates := make([]*core.Memory, 0, 4)
	for _, existing := range activeMemories {
		if existing == nil || existing.Status != core.MemoryStatusActive {
			continue
		}
		if memoriesLikelyDuplicate(*existing, candidate) {
			duplicates = append(duplicates, existing)
		}
	}
	return duplicates
}

func clampUnit(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func normalizeMemoryText(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(text)), " ")
}

func mergeUniqueStrings(existing []string, additional []string) []string {
	if len(additional) == 0 {
		return existing
	}
	seen := make(map[string]bool, len(existing)+len(additional))
	merged := make([]string, 0, len(existing)+len(additional))
	for _, value := range existing {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		merged = append(merged, value)
	}
	for _, value := range additional {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		merged = append(merged, value)
	}
	return merged
}

func memoriesLikelyDuplicate(a, b core.Memory) bool {
	if a.Type != b.Type || a.Scope != b.Scope || a.ProjectID != b.ProjectID {
		return false
	}

	subjA := normalizeMemoryText(a.Subject)
	subjB := normalizeMemoryText(b.Subject)
	if subjA != "" && subjB != "" && subjA != subjB {
		return false
	}

	tightA := normalizeMemoryText(a.TightDescription)
	tightB := normalizeMemoryText(b.TightDescription)
	if tightA != "" && tightA == tightB {
		return true
	}

	bodyA := normalizeMemoryText(a.Body)
	bodyB := normalizeMemoryText(b.Body)
	if bodyA != "" && bodyA == bodyB {
		return true
	}

	if tightA != "" && tightB != "" && jaccardSimilarity(tightA, tightB) >= 0.8 {
		return true
	}

	return bodyA != "" && bodyB != "" && jaccardSimilarity(bodyA, bodyB) >= 0.72
}
