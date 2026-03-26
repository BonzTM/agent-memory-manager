package service

import (
	"context"
	"log/slog"
	"strings"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const embeddingDedupThreshold = 0.85

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

// findDuplicatesByEmbedding checks for semantic duplicates using embedding cosine similarity.
// Returns nil if embedding provider is not configured or embedding fails.
// Only compares against active memories of the same type, scope, and projectID.
func (s *AMMService) findDuplicatesByEmbedding(ctx context.Context, candidate core.Memory, activeMemories []*core.Memory) []*core.Memory {
	if s.embeddingProvider == nil {
		return nil
	}

	candidateText := buildMemoryEmbeddingText(&candidate)
	vectors, err := s.embeddingProvider.Embed(ctx, []string{candidateText})
	if err != nil {
		slog.Debug("embedding dedup skipped: candidate embedding failed", "error", err, "memoryType", candidate.Type, "scope", candidate.Scope, "projectID", candidate.ProjectID)
		return nil
	}
	if len(vectors) != 1 {
		slog.Debug("embedding dedup skipped: unexpected candidate embedding count", "expected", 1, "actual", len(vectors), "memoryType", candidate.Type, "scope", candidate.Scope, "projectID", candidate.ProjectID)
		return nil
	}

	candidateVector := vectors[0]
	duplicates := make([]*core.Memory, 0, 4)
	model := s.embeddingProvider.Model()
	for _, existing := range activeMemories {
		if existing == nil || existing.Status != core.MemoryStatusActive {
			continue
		}
		if existing.Type != candidate.Type || existing.Scope != candidate.Scope || existing.ProjectID != candidate.ProjectID {
			continue
		}

		record, err := s.repo.GetEmbedding(ctx, existing.ID, "memory", model)
		if err != nil || record == nil || len(record.Vector) == 0 {
			continue
		}

		cosine, ok := cosineSimilarity(candidateVector, record.Vector)
		if !ok {
			continue
		}
		if cosine >= embeddingDedupThreshold {
			duplicates = append(duplicates, existing)
		}
	}

	return duplicates
}

func (s *AMMService) findDuplicatesByStoredEmbedding(ctx context.Context, candidate core.Memory, activeMemories []*core.Memory) []*core.Memory {
	if s.embeddingProvider == nil {
		return nil
	}

	model := s.embeddingProvider.Model()
	candidateRecord, err := s.repo.GetEmbedding(ctx, candidate.ID, "memory", model)
	if err != nil || candidateRecord == nil || len(candidateRecord.Vector) == 0 {
		return nil
	}

	duplicates := make([]*core.Memory, 0, 4)
	for _, existing := range activeMemories {
		if existing == nil || existing.Status != core.MemoryStatusActive {
			continue
		}
		if existing.ID == candidate.ID {
			continue
		}
		if existing.Type != candidate.Type || existing.Scope != candidate.Scope || existing.ProjectID != candidate.ProjectID {
			continue
		}

		record, err := s.repo.GetEmbedding(ctx, existing.ID, "memory", model)
		if err != nil || record == nil || len(record.Vector) == 0 {
			continue
		}

		cosine, ok := cosineSimilarity(candidateRecord.Vector, record.Vector)
		if !ok {
			continue
		}
		if cosine >= embeddingDedupThreshold {
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
