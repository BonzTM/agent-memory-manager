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
	const maxMergesPerIteration = 500
	const maxIterations = 10

	totalMerged := 0
	for iteration := 1; iteration <= maxIterations; iteration++ {
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

		merged := 0
		mergedIDs := make(map[string]bool)
		stopIteration := false

		for _, group := range groups {
			activeByType := make([]*core.Memory, 0, len(group))
			for i := range group {
				activeByType = append(activeByType, &group[i])
			}
			for i := range group {
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

				candidates, err := s.repo.SearchMemories(ctx, query, core.ListMemoriesOptions{Limit: 10})
				if err != nil {
					continue
				}
				if len(candidates) <= 1 {
					candidates = group
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

					if err := s.repo.UpdateMemory(ctx, superseded); err != nil {
						return false
					}
					if err := s.repo.UpdateMemory(ctx, keeper); err != nil {
						return false
					}

					mergedIDs[superseded.ID] = true
					merged++
					return true
				}

				jaccardMerged := false

				for j := range candidates {
					candB := &candidates[j]

					sim := jaccardSimilarity(memA.Body, candB.Body)
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
				embCandidates := s.findDuplicatesByStoredEmbedding(ctx, *memA, activeByType)
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

		slog.Debug("merge_duplicates iteration complete", "iteration", iteration, "merged", merged)
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
	wordsA := wordSet(textA)
	wordsB := wordSet(textB)

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
