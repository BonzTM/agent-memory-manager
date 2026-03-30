package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

// MergeDuplicates finds highly overlapping active memories and merges them via
// supersession.
func (s *AMMService) MergeDuplicates(ctx context.Context) (int, error) {
	slog.Debug("MergeDuplicates called")

	const maxMergesPerIteration = 500
	const maxIterations = 10

	totalMerged := 0
	for iteration := 1; iteration <= maxIterations; iteration++ {
		if err := ctx.Err(); err != nil {
			return totalMerged, fmt.Errorf("merge_duplicates cancelled: %w", err)
		}

		iterStart := time.Now()
		memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
			Status: core.MemoryStatusActive,
			Limit:  10000,
		})
		if err != nil {
			return totalMerged, fmt.Errorf("list memories for dedup: %w", err)
		}

		groups := make(map[core.MemoryType][]core.Memory)
		for _, mem := range memories {
			groups[mem.Type] = append(groups[mem.Type], mem)
		}

		// Pre-compute word sets for all memories to avoid redundant allocations
		// during pairwise Jaccard comparisons.
		wordSets := make(map[string]map[string]bool, len(memories))
		for i := range memories {
			wordSets[memories[i].ID] = wordSet(memories[i].Body)
		}

		merged := 0
		mergedIDs := make(map[string]bool)
		stopIteration := false

		for _, group := range groups {
			activeByType := make([]*core.Memory, 0, len(group))
			for i := range group {
				activeByType = append(activeByType, &group[i])
			}

			// Build subject-keyed index for cheap fallback candidate lookup
			// when FTS returns too few results.
			bySubject := make(map[string][]*core.Memory)
			for i := range group {
				key := normalizeMemoryText(group[i].Subject)
				bySubject[key] = append(bySubject[key], &group[i])
			}

			// Batch-load embeddings for this type group so the embedding
			// dedup path avoids per-memory DB queries.
			var embeddingCache map[string][]float32
			if s.embeddingProvider != nil {
				embeddingCache = make(map[string][]float32, len(group))
				ids := make([]string, len(group))
				for i := range group {
					ids[i] = group[i].ID
				}
				model := s.embeddingProvider.Model()
				if batch, err := s.repo.GetEmbeddingsBatch(ctx, ids, "memory", model); err == nil {
					for id, rec := range batch {
						if len(rec.Vector) > 0 {
							embeddingCache[id] = rec.Vector
						}
					}
				}
			}

			for i := range group {
				if err := ctx.Err(); err != nil {
					return totalMerged + merged, fmt.Errorf("merge_duplicates cancelled: %w", err)
				}
				if merged >= maxMergesPerIteration {
					stopIteration = true
					break
				}

				memA := &group[i]
				if mergedIDs[memA.ID] {
					continue
				}

				query := memA.TightDescription
				if strings.TrimSpace(query) == "" {
					continue
				}

				candidates, err := s.repo.SearchMemories(ctx, query, core.ListMemoriesOptions{
						Status: core.MemoryStatusActive,
						Limit:  10,
					})
				if err != nil {
					continue
				}
				// If FTS returned too few results, fall back to same-subject
				// peers. For memories with no subject, use a bounded slice
				// of the type group to avoid O(N²) over thousands of memories.
				if len(candidates) <= 1 {
					subjectKey := normalizeMemoryText(memA.Subject)
					var peers []*core.Memory
					if subjectKey != "" {
						peers = bySubject[subjectKey]
					} else {
						peers = activeByType
					}
					const maxFallbackCandidates = 50
					cap := len(peers)
					if cap > maxFallbackCandidates {
						cap = maxFallbackCandidates
					}
					candidates = make([]core.Memory, 0, cap)
					for _, p := range peers {
						if len(candidates) >= maxFallbackCandidates {
							break
						}
						candidates = append(candidates, *p)
					}
				}

				mergePair := func(memA, candB *core.Memory) bool {
					if candB.ID == memA.ID {
						return false
					}
					if mergedIDs[candB.ID] {
						return false
					}
					if candB.Type != memA.Type {
						return false
					}
					if candB.Scope != memA.Scope {
						return false
					}
					if candB.ProjectID != memA.ProjectID {
						return false
					}
					if candB.Status != core.MemoryStatusActive {
						return false
					}

					keeper, superseded := memA, candB
					if candB.Confidence > memA.Confidence {
						keeper, superseded = candB, memA
					} else if candB.Confidence == memA.Confidence && candB.CreatedAt.After(memA.CreatedAt) {
						keeper, superseded = candB, memA
					}

					now := time.Now().UTC()
					superseded.Status = core.MemoryStatusSuperseded
					superseded.SupersededBy = keeper.ID
					superseded.SupersededAt = &now
					superseded.UpdatedAt = now
					keeper.SourceEventIDs = mergeUniqueStrings(keeper.SourceEventIDs, superseded.SourceEventIDs)
					keeper.UpdatedAt = now

					if keeper.Supersedes == "" {
						keeper.Supersedes = superseded.ID
					}

					if err := s.repo.UpdateMemoriesBatch(ctx, []*core.Memory{superseded, keeper}); err != nil {
						return false
					}

					mergedIDs[superseded.ID] = true
					merged++
					return true
				}

				jaccardMerged := false

				wsA := wordSets[memA.ID]
				for j := range candidates {
					candB := &candidates[j]

					wsB := wordSets[candB.ID]
					if wsB == nil {
						// Candidate may have come from FTS and not be in the
						// pre-computed set (different iteration or new).
						wsB = wordSet(candB.Body)
					}
					sim := jaccardSimilarityFromSets(wsA, wsB)
					if sim <= 0.7 {
						continue
					}

					if !mergePair(memA, candB) {
						continue
					}
					jaccardMerged = true

					if merged >= maxMergesPerIteration {
						stopIteration = true
						break
					}

					break
				}

				if !jaccardMerged && s.embeddingProvider != nil {
					embCandidates := s.findDuplicatesByStoredEmbeddingWithCache(ctx, *memA, activeByType, embeddingCache)
					for _, candB := range embCandidates {
						if !mergePair(memA, candB) {
							continue
						}
						if merged >= maxMergesPerIteration {
							stopIteration = true
						}
						break
					}
				}
				if stopIteration {
					break
				}
			}
			if stopIteration {
				break
			}
		}

		slog.Info("merge_duplicates iteration complete",
			"iteration", iteration,
			"merged", merged,
			"active_memories", len(memories),
			"duration_ms", time.Since(iterStart).Milliseconds(),
		)
		totalMerged += merged
		if merged == 0 {
			break
		}
	}
	return totalMerged, nil
}

// jaccardSimilarity computes the Jaccard similarity between the word sets of two texts.
// Words are split on whitespace and lowercased.
func jaccardSimilarity(textA, textB string) float64 {
	return jaccardSimilarityFromSets(wordSet(textA), wordSet(textB))
}

// jaccardSimilarityFromSets computes Jaccard similarity from pre-computed word sets.
func jaccardSimilarityFromSets(wordsA, wordsB map[string]bool) float64 {
	if len(wordsA) == 0 && len(wordsB) == 0 {
		return 1.0
	}

	// Compute intersection and union sizes.
	intersection := 0
	for w := range wordsA {
		if wordsB[w] {
			intersection++
		}
	}

	union := len(wordsA)
	for w := range wordsB {
		if !wordsA[w] {
			union++
		}
	}

	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// wordSet splits text on whitespace and returns a set of lowercased words.
func wordSet(text string) map[string]bool {
	fields := strings.Fields(strings.ToLower(text))
	set := make(map[string]bool, len(fields))
	for _, f := range fields {
		set[f] = true
	}
	return set
}
