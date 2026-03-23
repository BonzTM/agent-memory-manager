package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/joshd-04/agent-memory-manager/internal/core"
)

// MergeDuplicates finds same-type memories with high FTS text overlap and merges them via supersession.
// Returns the number of merges performed.
func (s *AMMService) MergeDuplicates(ctx context.Context) (int, error) {
	// List active memories.
	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Status: core.MemoryStatusActive,
		Limit:  1000,
	})
	if err != nil {
		return 0, fmt.Errorf("list memories for dedup: %w", err)
	}

	// Group by type.
	groups := make(map[core.MemoryType][]core.Memory)
	for _, mem := range memories {
		groups[mem.Type] = append(groups[mem.Type], mem)
	}

	merged := 0
	mergedIDs := make(map[string]bool)
	const maxMerges = 50

	for _, group := range groups {
		for i := range group {
			if merged >= maxMerges {
				return merged, nil
			}

			memA := &group[i]
			if mergedIDs[memA.ID] {
				continue
			}

			// Use the first 50 chars of TightDescription as an FTS query.
			query := memA.TightDescription
			if len(query) > 50 {
				query = query[:50]
			}
			if strings.TrimSpace(query) == "" {
				continue
			}

			candidates, err := s.repo.SearchMemories(ctx, query, 10)
			if err != nil {
				continue
			}

			for j := range candidates {
				candB := &candidates[j]

				// Skip self, already merged, different type, different scope.
				if candB.ID == memA.ID {
					continue
				}
				if mergedIDs[candB.ID] {
					continue
				}
				if candB.Type != memA.Type {
					continue
				}
				if candB.Scope != memA.Scope {
					continue
				}
				if candB.Status != core.MemoryStatusActive {
					continue
				}

				sim := jaccardSimilarity(memA.Body, candB.Body)
				if sim <= 0.7 {
					continue
				}

				// Determine keeper and superseded.
				keeper, superseded := memA, candB
				if candB.Confidence > memA.Confidence {
					keeper, superseded = candB, memA
				} else if candB.Confidence == memA.Confidence && candB.CreatedAt.After(memA.CreatedAt) {
					keeper, superseded = candB, memA
				}

				// Supersede the loser.
				now := time.Now().UTC()
				superseded.Status = core.MemoryStatusSuperseded
				superseded.SupersededBy = keeper.ID
				superseded.SupersededAt = &now
				superseded.UpdatedAt = now

				if keeper.Supersedes == "" {
					keeper.Supersedes = superseded.ID
					keeper.UpdatedAt = now
				}

				if err := s.repo.UpdateMemory(ctx, superseded); err != nil {
					continue
				}
				if err := s.repo.UpdateMemory(ctx, keeper); err != nil {
					continue
				}

				mergedIDs[superseded.ID] = true
				merged++

				if merged >= maxMerges {
					return merged, nil
				}

				// Only merge one candidate per source memory per iteration.
				break
			}
		}
	}

	return merged, nil
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
