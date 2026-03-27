package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	defaultLifecycleReviewBatchSize = 50
	lifecycleReviewInterval         = 7 * 24 * time.Hour

	lifecyclePromoteDelta = 0.15
	lifecyclePromoteCap   = 1.0
	lifecycleDecayDelta   = 0.15
	lifecycleDecayFloor   = 0.05
)

type lifecycleMutationAction int

const (
	lifecycleActionPromote lifecycleMutationAction = iota + 1
	lifecycleActionDecay
	lifecycleActionMerge
	lifecycleActionArchive
)

func (s *AMMService) SetLifecycleReviewBatchSize(batchSize int) {
	if batchSize <= 0 {
		s.lifecycleReviewBatchSize = defaultLifecycleReviewBatchSize
		return
	}
	s.lifecycleReviewBatchSize = batchSize
}

func (s *AMMService) LifecycleReview(ctx context.Context) (int, error) {
	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Status: core.MemoryStatusActive,
		Limit:  10000,
	})
	if err != nil {
		return 0, fmt.Errorf("list memories for lifecycle review: %w", err)
	}
	if len(memories) == 0 {
		return 0, nil
	}

	now := time.Now().UTC()
	batchSize := s.lifecycleReviewBatchSize
	if batchSize <= 0 {
		batchSize = defaultLifecycleReviewBatchSize
	}

	reviewCandidates := make([]*core.Memory, 0, len(memories))
	for i := range memories {
		mem := &memories[i]
		reviewedAtRaw := strings.TrimSpace(getProcessingMeta(mem, MetaLifecycleReviewedAt))
		if reviewedAtRaw == "" {
			reviewCandidates = append(reviewCandidates, mem)
			continue
		}
		reviewedAt, parseErr := time.Parse(time.RFC3339, reviewedAtRaw)
		if parseErr != nil || now.Sub(reviewedAt) >= lifecycleReviewInterval {
			reviewCandidates = append(reviewCandidates, mem)
		}
	}
	if len(reviewCandidates) == 0 {
		return 0, nil
	}

	accessStats, err := s.repo.ListMemoryAccessStats(ctx, now.Add(-30*24*time.Hour))
	if err != nil {
		return 0, fmt.Errorf("list memory access stats for lifecycle review: %w", err)
	}
	accessByMemory := make(map[string]core.MemoryAccessStat, len(accessStats))
	for i := range accessStats {
		accessByMemory[accessStats[i].MemoryID] = accessStats[i]
	}

	modelName := s.lifecycleReviewModelName()
	affected := 0

	for start := 0; start < len(reviewCandidates); start += batchSize {
		end := start + batchSize
		if end > len(reviewCandidates) {
			end = len(reviewCandidates)
		}

		batch := reviewCandidates[start:end]
		reviews := make([]core.MemoryReview, 0, len(batch))
		for _, mem := range batch {
			stat := accessByMemory[mem.ID]
			reviews = append(reviews, core.MemoryReview{
				ID:               mem.ID,
				Type:             string(mem.Type),
				Subject:          mem.Subject,
				Body:             mem.Body,
				TightDescription: mem.TightDescription,
				Confidence:       mem.Confidence,
				Importance:       mem.Importance,
				CreatedAt:        mem.CreatedAt.Format(time.RFC3339),
				LastAccessedAt:   stat.LastAccessedAt,
				AccessCount:      stat.AccessCount,
			})
		}

		result, err := s.intelligence.ReviewMemories(ctx, reviews)
		if err != nil {
			return affected, fmt.Errorf("review memory batch: %w", err)
		}
		if result == nil {
			result = &core.ReviewResult{}
		}

		batchByID := make(map[string]*core.Memory, len(batch))
		for _, mem := range batch {
			batchByID[mem.ID] = mem
		}

		resolvedActions := make(map[string]lifecycleMutationAction, len(batchByID))
		mutatedMemoryIDs := make(map[string]bool, len(batchByID))
		resolveAction := func(memoryID string, action lifecycleMutationAction) {
			if memoryID == "" {
				return
			}
			if _, ok := batchByID[memoryID]; !ok {
				return
			}
			if existing, ok := resolvedActions[memoryID]; ok && existing >= action {
				return
			}
			resolvedActions[memoryID] = action
		}

		for _, id := range result.Promote {
			resolveAction(id, lifecycleActionPromote)
		}
		for _, id := range result.Decay {
			resolveAction(id, lifecycleActionDecay)
		}
		for _, id := range result.Archive {
			resolveAction(id, lifecycleActionArchive)
		}
		for _, suggestion := range result.Merge {
			resolveAction(suggestion.KeepID, lifecycleActionMerge)
			resolveAction(suggestion.MergeID, lifecycleActionMerge)
		}

		for _, id := range result.Promote {
			if resolvedActions[id] != lifecycleActionPromote {
				continue
			}
			mem := batchByID[id]
			if mem == nil {
				continue
			}
			newImportance := minFloat(mem.Importance+lifecyclePromoteDelta, lifecyclePromoteCap)
			if newImportance != mem.Importance {
				mem.Importance = newImportance
				mutatedMemoryIDs[mem.ID] = true
				affected++
			}
		}

		for _, id := range result.Decay {
			if resolvedActions[id] != lifecycleActionDecay {
				continue
			}
			mem := batchByID[id]
			if mem == nil {
				continue
			}
			newImportance := maxFloat(mem.Importance-lifecycleDecayDelta, lifecycleDecayFloor)
			if newImportance != mem.Importance {
				mem.Importance = newImportance
				mutatedMemoryIDs[mem.ID] = true
				affected++
			}
		}

		for _, id := range result.Archive {
			if resolvedActions[id] != lifecycleActionArchive {
				continue
			}
			mem := batchByID[id]
			if mem == nil {
				continue
			}
			if mem.Status != core.MemoryStatusArchived {
				mem.Status = core.MemoryStatusArchived
				mutatedMemoryIDs[mem.ID] = true
				affected++
			}
		}

		for _, suggestion := range result.Merge {
			if resolvedActions[suggestion.KeepID] != lifecycleActionMerge || resolvedActions[suggestion.MergeID] != lifecycleActionMerge {
				continue
			}
			keep := batchByID[suggestion.KeepID]
			merge := batchByID[suggestion.MergeID]
			if keep == nil || merge == nil || keep.ID == merge.ID {
				continue
			}
			if merge.Status != core.MemoryStatusSuperseded || merge.SupersededBy != keep.ID {
				merge.Status = core.MemoryStatusSuperseded
				merge.SupersededBy = keep.ID
				supNow := now
				merge.SupersededAt = &supNow
				mutatedMemoryIDs[merge.ID] = true
				affected++
			}
		}

		for _, contradiction := range result.Contradictions {
			slog.Info("lifecycle review contradiction detected",
				"memory_a", contradiction.MemoryA,
				"memory_b", contradiction.MemoryB,
				"explanation", contradiction.Explanation,
			)
		}

		for _, mem := range batch {
			originalUpdatedAt := mem.UpdatedAt
			markLifecycleReviewed(mem, modelName)
			if mutatedMemoryIDs[mem.ID] {
				mem.UpdatedAt = time.Now().UTC()
			} else {
				mem.UpdatedAt = originalUpdatedAt
			}
			if err := s.repo.UpdateMemory(ctx, mem); err != nil {
				return affected, fmt.Errorf("update lifecycle-reviewed memory %s: %w", mem.ID, err)
			}
		}
	}

	return affected, nil
}

func (s *AMMService) lifecycleReviewModelName() string {
	llm, ok := s.intelligence.(*LLMIntelligenceProvider)
	if !ok || llm == nil || llm.LLMSummarizer == nil {
		return ""
	}
	return llm.model
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
