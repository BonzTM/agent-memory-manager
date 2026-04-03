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
	openLoopArchiveWithoutAccessAge = 30 * 24 * time.Hour

	lifecyclePromoteDelta = 0.15
	lifecyclePromoteCap   = 1.0
	lifecycleDecayDelta   = 0.15
	lifecycleDecayFloor   = 0.05
)

type lifecycleMutationAction int

type scopedOpenLoopResolutionKey struct {
	scope     core.Scope
	projectID string
	sessionID string
	key       string
}

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
	slog.Debug("LifecycleReview called")
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
	decisionSubjects := make(map[scopedOpenLoopResolutionKey]time.Time)
	for i := range memories {
		mem := &memories[i]
		if mem.Status != core.MemoryStatusActive || mem.Type != core.MemoryTypeDecision {
			continue
		}
		resolvedAt := mem.CreatedAt
		if mem.UpdatedAt.After(resolvedAt) {
			resolvedAt = mem.UpdatedAt
		}
		for _, key := range openLoopResolutionKeys(mem.Subject, mem.TightDescription) {
			scopedKey := makeScopedOpenLoopResolutionKey(mem, key)
			if existing, ok := decisionSubjects[scopedKey]; !ok || resolvedAt.After(existing) {
				decisionSubjects[scopedKey] = resolvedAt
			}
		}
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
		deterministicArchives := make([]string, 0, len(batch))
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
		for _, mem := range batch {
			stat := accessByMemory[mem.ID]
			if shouldArchiveStaleOpenLoop(mem, stat, now) || shouldArchiveResolvedOpenLoop(mem, decisionSubjects) {
				resolveAction(mem.ID, lifecycleActionArchive)
				deterministicArchives = append(deterministicArchives, mem.ID)
			}
		}
		result.Archive = mergeUniqueStrings(result.Archive, deterministicArchives)

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
			if _, err := s.persistContradiction(
				ctx,
				contradiction.MemoryA,
				contradiction.MemoryB,
				contradiction.Explanation,
				[]string{"contradiction", "lifecycle-review"},
			); err != nil {
				return affected, fmt.Errorf("persist lifecycle review contradiction: %w", err)
			}

			for _, id := range []string{contradiction.MemoryA, contradiction.MemoryB} {
				mem := batchByID[id]
				if mem == nil {
					continue
				}
				latest, err := s.repo.GetMemory(ctx, id)
				if err != nil || latest == nil {
					continue
				}
				*mem = *latest
				mutatedMemoryIDs[id] = true
			}
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

func shouldArchiveStaleOpenLoop(mem *core.Memory, stat core.MemoryAccessStat, now time.Time) bool {
	if mem == nil || mem.Type != core.MemoryTypeOpenLoop || mem.Status != core.MemoryStatusActive {
		return false
	}
	if stat.AccessCount > 0 || mem.CreatedAt.IsZero() {
		return false
	}
	return now.Sub(mem.CreatedAt) >= openLoopArchiveWithoutAccessAge
}

func shouldArchiveResolvedOpenLoop(mem *core.Memory, decisionSubjects map[scopedOpenLoopResolutionKey]time.Time) bool {
	if mem == nil || mem.Type != core.MemoryTypeOpenLoop || mem.Status != core.MemoryStatusActive {
		return false
	}
	recordedAt := mem.CreatedAt
	if recordedAt.IsZero() {
		recordedAt = mem.UpdatedAt
	}
	for _, key := range openLoopResolutionKeys(mem.Subject, mem.TightDescription) {
		resolvedAt, ok := decisionSubjects[makeScopedOpenLoopResolutionKey(mem, key)]
		if !ok {
			continue
		}
		if recordedAt.IsZero() || !resolvedAt.Before(recordedAt) {
			return true
		}
	}
	return false
}

func makeScopedOpenLoopResolutionKey(mem *core.Memory, key string) scopedOpenLoopResolutionKey {
	if mem == nil {
		return scopedOpenLoopResolutionKey{key: key}
	}
	return scopedOpenLoopResolutionKey{
		scope:     mem.Scope,
		projectID: mem.ProjectID,
		sessionID: mem.SessionID,
		key:       key,
	}
}

// minOpenLoopResolutionKeyLen is the minimum normalized text length for
// an open loop resolution key. Short subjects like "database" or "config"
// match too broadly and cause false-positive archival.
const minOpenLoopResolutionKeyLen = 12

func openLoopResolutionKeys(subject, tightDescription string) []string {
	keys := make([]string, 0, 2)
	for _, value := range []string{subject, tightDescription} {
		normalized := normalizeMemoryText(value)
		if normalized == "" || len(normalized) < minOpenLoopResolutionKeyLen {
			continue
		}
		alreadyIncluded := false
		for _, existing := range keys {
			if existing == normalized {
				alreadyIncluded = true
				break
			}
		}
		if alreadyIncluded {
			continue
		}
		keys = append(keys, normalized)
	}
	return keys
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
