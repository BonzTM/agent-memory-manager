package service

import (
	"context"
	"fmt"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

// EnrichMemories finds memories that have not had entity extraction and
// enriches them by linking entities and recording processing ledger metadata.
//
// This pass is additive-only: it never changes authored memory content fields.
func (s *AMMService) EnrichMemories(ctx context.Context) (int, error) {
	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Status: core.MemoryStatusActive,
		Limit:  10000,
	})
	if err != nil {
		return 0, fmt.Errorf("list memories for enrichment: %w", err)
	}

	enriched := 0
	for i := range memories {
		mem := &memories[i]
		if hasProcessingStep(mem, MetaEntitiesExtracted) {
			continue
		}

		if err := s.linkEntitiesToMemory(ctx, mem.ID, mem.Body); err != nil {
			return enriched, fmt.Errorf("link entities for memory %s: %w", mem.ID, err)
		}

		markEntitiesExtracted(mem, s.extractionMethod())
		mem.UpdatedAt = time.Now().UTC()
		if err := s.repo.UpdateMemory(ctx, mem); err != nil {
			return enriched, fmt.Errorf("update enriched memory %s: %w", mem.ID, err)
		}

		enriched++
	}

	return enriched, nil
}
