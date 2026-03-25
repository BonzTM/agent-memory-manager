package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	// decayHalfLifeDays is the half-life in days for the freshness exponential decay.
	decayHalfLifeDays = 14.0

	// decayFreshnessThreshold is the freshness value below which memories are considered stale.
	decayFreshnessThreshold = 0.1

	// decayImportanceReduction is the factor by which importance is reduced for stale memories.
	decayImportanceReduction = 0.8

	// decayArchiveThreshold is the importance below which stale memories get archived.
	decayArchiveThreshold = 0.1
)

// DecayStaleMemories downranks or archives stale active memories and returns
// the number updated.
func (s *AMMService) DecayStaleMemories(ctx context.Context) (int, error) {
	// List all active memories.
	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Status: core.MemoryStatusActive,
		Limit:  10000,
	})
	if err != nil {
		return 0, fmt.Errorf("list memories for decay: %w", err)
	}

	if len(memories) == 0 {
		return 0, nil
	}

	now := time.Now().UTC()
	decayed := 0

	for i := range memories {
		mem := &memories[i]

		// Skip identity and constraint memories — these should never decay.
		if mem.Type == core.MemoryTypeIdentity || mem.Type == core.MemoryTypeConstraint {
			continue
		}

		// Compute freshness.
		lastTouch := lastTouchTime(mem)
		daysSince := now.Sub(lastTouch).Hours() / 24.0
		freshness := math.Exp(-0.693 * daysSince / decayHalfLifeDays)

		if freshness >= decayFreshnessThreshold {
			continue
		}

		// Memory is stale — apply decay.
		updated := false

		if mem.Importance > decayArchiveThreshold {
			// Reduce importance by 20%.
			mem.Importance *= decayImportanceReduction
			updated = true
		} else if mem.Type != core.MemoryTypeDecision && mem.Type != core.MemoryTypeFact {
			// Archive low-importance non-decision, non-fact memories.
			mem.Status = core.MemoryStatusArchived
			updated = true
		}

		if updated {
			mem.UpdatedAt = now
			if err := s.repo.UpdateMemory(ctx, mem); err != nil {
				return decayed, fmt.Errorf("update decayed memory %s: %w", mem.ID, err)
			}
			decayed++
		}
	}

	return decayed, nil
}

// lastTouchTime returns the most recent timestamp from a memory's time fields.
func lastTouchTime(mem *core.Memory) time.Time {
	latest := mem.CreatedAt

	if mem.UpdatedAt.After(latest) {
		latest = mem.UpdatedAt
	}
	if mem.ObservedAt != nil && mem.ObservedAt.After(latest) {
		latest = *mem.ObservedAt
	}
	if mem.LastConfirmedAt != nil && mem.LastConfirmedAt.After(latest) {
		latest = *mem.LastConfirmedAt
	}

	return latest
}
