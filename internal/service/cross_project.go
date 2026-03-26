package service

import (
	"context"
	"fmt"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	defaultCrossProjectSimilarityThreshold = 0.7
	crossProjectImportanceFloor            = 0.7
	crossProjectConfidenceFloor            = 0.7
)

func (s *AMMService) CrossProjectTransfer(ctx context.Context) (int, error) {
	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Scope:  core.ScopeProject,
		Status: core.MemoryStatusActive,
		Limit:  10000,
	})
	if err != nil {
		return 0, fmt.Errorf("list project memories for transfer: %w", err)
	}

	candidates := make([]*core.Memory, 0, len(memories))
	for i := range memories {
		mem := &memories[i]
		if mem.ProjectID == "" {
			continue
		}
		if mem.Importance < crossProjectImportanceFloor || mem.Confidence < crossProjectConfidenceFloor {
			continue
		}
		if normalizeMemoryText(mem.Body) == "" {
			continue
		}
		candidates = append(candidates, mem)
	}

	if len(candidates) < 2 {
		return 0, nil
	}

	neighbors := make([][]int, len(candidates))
	similarityThreshold := s.crossProjectSimilarityThreshold
	if similarityThreshold <= 0 {
		similarityThreshold = defaultCrossProjectSimilarityThreshold
	}
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			memA := candidates[i]
			memB := candidates[j]
			if memA.Type != memB.Type {
				continue
			}
			if memA.ProjectID == memB.ProjectID {
				continue
			}
			sim := jaccardSimilarity(normalizeMemoryText(memA.Body), normalizeMemoryText(memB.Body))
			if sim < similarityThreshold {
				continue
			}
			neighbors[i] = append(neighbors[i], j)
			neighbors[j] = append(neighbors[j], i)
		}
	}

	now := time.Now().UTC()
	promoted := 0
	visited := make([]bool, len(candidates))

	for i := range candidates {
		if visited[i] {
			continue
		}
		if len(neighbors[i]) == 0 {
			visited[i] = true
			continue
		}

		component := collectCrossProjectComponent(i, neighbors, visited)
		if len(component) < 2 {
			continue
		}

		projects := make(map[string]bool)
		for _, idx := range component {
			projects[candidates[idx].ProjectID] = true
		}
		if len(projects) < 2 {
			continue
		}

		best := candidates[component[0]]
		for _, idx := range component[1:] {
			best = chooseCrossProjectBest(best, candidates[idx])
		}

		globalMemory := &core.Memory{
			ID:               generateID("mem_"),
			Type:             best.Type,
			Scope:            core.ScopeGlobal,
			Subject:          best.Subject,
			Body:             best.Body,
			TightDescription: best.TightDescription,
			Confidence:       best.Confidence,
			Importance:       best.Importance,
			PrivacyLevel:     best.PrivacyLevel,
			Status:           core.MemoryStatusActive,
			CreatedAt:        now,
			UpdatedAt:        now,
			Metadata:         cloneProcessingMetadata(best.Metadata),
		}

		for _, idx := range component {
			mem := candidates[idx]
			globalMemory.SourceEventIDs = mergeUniqueStrings(globalMemory.SourceEventIDs, mem.SourceEventIDs)
		}

		setProcessingMeta(globalMemory, MetaExtractionQuality, QualityVerified)
		if extractionMethod := getProcessingMeta(best, MetaExtractionMethod); extractionMethod != "" {
			setProcessingMeta(globalMemory, MetaExtractionMethod, extractionMethod)
		}

		if err := s.repo.InsertMemory(ctx, globalMemory); err != nil {
			return promoted, fmt.Errorf("insert promoted global memory: %w", err)
		}

		for _, idx := range component {
			mem := candidates[idx]
			mem.Status = core.MemoryStatusSuperseded
			mem.SupersededBy = globalMemory.ID
			mem.SupersededAt = &now
			mem.UpdatedAt = now
			if err := s.repo.UpdateMemory(ctx, mem); err != nil {
				return promoted, fmt.Errorf("supersede project memory %s: %w", mem.ID, err)
			}
		}

		promoted++
	}

	return promoted, nil
}

func collectCrossProjectComponent(start int, neighbors [][]int, visited []bool) []int {
	stack := []int{start}
	visited[start] = true
	component := make([]int, 0, 4)

	for len(stack) > 0 {
		idx := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		component = append(component, idx)
		for _, next := range neighbors[idx] {
			if visited[next] {
				continue
			}
			visited[next] = true
			stack = append(stack, next)
		}
	}

	return component
}

func chooseCrossProjectBest(a, b *core.Memory) *core.Memory {
	if b == nil {
		return a
	}
	if a == nil {
		return b
	}
	if b.Confidence > a.Confidence {
		return b
	}
	if b.Confidence < a.Confidence {
		return a
	}
	if b.Importance > a.Importance {
		return b
	}
	if b.Importance < a.Importance {
		return a
	}
	if b.CreatedAt.After(a.CreatedAt) {
		return b
	}
	return a
}

func cloneProcessingMetadata(metadata map[string]string) map[string]string {
	if metadata == nil {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(metadata))
	for key, value := range metadata {
		cloned[key] = value
	}
	return cloned
}
