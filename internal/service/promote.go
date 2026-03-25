package service

import (
	"context"
	"fmt"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	promoteHighValueThreshold   = 0.8
	promoteRecentWindow         = 30 * 24 * time.Hour
	promoteImportanceMultiplier = 1.1

	sessionTraceArchiveMaxAge = 7 * 24 * time.Hour
	sessionTraceLowImportance = 0.3
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

	now := time.Now().UTC()
	promoted := 0

	for i := range memories {
		mem := &memories[i]

		if mem.Type == core.MemoryTypeIdentity || mem.Type == core.MemoryTypeConstraint {
			continue
		}
		if mem.Importance < promoteHighValueThreshold || mem.Confidence < promoteHighValueThreshold {
			continue
		}

		recentTouch := mem.UpdatedAt
		if mem.LastConfirmedAt != nil && mem.LastConfirmedAt.After(recentTouch) {
			recentTouch = *mem.LastConfirmedAt
		}
		if now.Sub(recentTouch) > promoteRecentWindow {
			continue
		}

		mem.Importance *= promoteImportanceMultiplier
		if mem.Importance > 1.0 {
			mem.Importance = 1.0
		}
		mem.LastConfirmedAt = &now
		mem.UpdatedAt = now

		if err := s.repo.UpdateMemory(ctx, mem); err != nil {
			return promoted, fmt.Errorf("update promoted memory %s: %w", mem.ID, err)
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
