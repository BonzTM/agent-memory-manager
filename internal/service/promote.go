package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	sessionTraceArchiveMaxAge = 7 * 24 * time.Hour
)

func (s *AMMService) ArchiveLowSalienceSessionTraces(ctx context.Context) (int, error) {
	slog.Debug("ArchiveLowSalienceSessionTraces called")
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

		if now.Sub(lastTouchTime(mem)) <= sessionTraceArchiveMaxAge {
			continue
		}

		mem.Status = core.MemoryStatusArchived
		mem.UpdatedAt = now

		if err := s.repo.UpdateMemory(ctx, mem); err != nil {
			return archived, fmt.Errorf("archive session trace %s: %w", mem.ID, err)
		}
		archived++
	}

	return archived, nil
}
