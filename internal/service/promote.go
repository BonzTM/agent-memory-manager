package service

import (
	"context"
	"fmt"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	sessionTraceArchiveMaxAge = 7 * 24 * time.Hour
	sessionTraceLowImportance = 0.3

	highRecallLookback     = 30 * 24 * time.Hour
	highRecallThreshold    = 5
	highRecallPromoteFloor = 0.7
	highRecallPromoteDelta = 0.1
	highRecallPromoteCap   = 0.85
)

func (s *AMMService) PromoteHighValueMemories(ctx context.Context) (int, error) {
	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Status: core.MemoryStatusActive,
		Limit:  10000,
	})
	if err != nil {
		return 0, fmt.Errorf("list memories for promotion: %w", err)
	}
	if len(memories) == 0 {
		return 0, nil
	}

	stats, err := s.repo.ListMemoryAccessStats(ctx, time.Now().UTC().Add(-highRecallLookback))
	if err != nil {
		return 0, fmt.Errorf("list memory access stats for promotion: %w", err)
	}
	if len(stats) == 0 {
		return 0, nil
	}

	countByMemory := make(map[string]int, len(stats))
	for i := range stats {
		countByMemory[stats[i].MemoryID] = stats[i].AccessCount
	}

	now := time.Now().UTC()
	promoted := 0
	for i := range memories {
		mem := &memories[i]
		if countByMemory[mem.ID] < highRecallThreshold {
			continue
		}
		if mem.Importance >= highRecallPromoteFloor {
			continue
		}

		newImportance := mem.Importance + highRecallPromoteDelta
		if newImportance > highRecallPromoteCap {
			newImportance = highRecallPromoteCap
		}
		if newImportance <= mem.Importance {
			continue
		}

		mem.Importance = newImportance
		mem.UpdatedAt = now
		if err := s.repo.UpdateMemory(ctx, mem); err != nil {
			return promoted, fmt.Errorf("promote high-value memory %s: %w", mem.ID, err)
		}
		promoted++
	}

	return promoted, nil
}

func (s *AMMService) ArchiveLowSalienceSessionTraces(ctx context.Context) (int, error) {
	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Status: core.MemoryStatusActive,
		Scope:  core.ScopeSession,
		Limit:  10000,
	})
	if err != nil {
		return 0, fmt.Errorf("list session memories for archival: %w", err)
	}

	if len(memories) == 0 {
		return 0, nil
	}

	now := time.Now().UTC()
	archived := 0

	for i := range memories {
		mem := &memories[i]

		if mem.Importance >= sessionTraceLowImportance {
			continue
		}
		if now.Sub(lastTouchTime(mem)) <= sessionTraceArchiveMaxAge {
			continue
		}

		mem.Status = core.MemoryStatusArchived
		mem.UpdatedAt = now

		if err := s.repo.UpdateMemory(ctx, mem); err != nil {
			return archived, fmt.Errorf("archive low-salience session trace %s: %w", mem.ID, err)
		}
		archived++
	}

	return archived, nil
}
