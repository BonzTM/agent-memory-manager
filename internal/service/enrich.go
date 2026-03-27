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
	batchSize := s.reflectLLMBatchSize
	if batchSize <= 0 {
		batchSize = defaultReflectLLMBatchSize
	}

	for i := 0; i < len(memories); i += batchSize {
		end := i + batchSize
		if end > len(memories) {
			end = len(memories)
		}
		batch := memories[i:end]

		analysisEntities := make([]core.EntityCandidate, 0)
		usedAnalysis := false
		if s.intelligence != nil && s.hasLLMSummarizer {
			analysisInputs := make([]core.EventContent, 0, len(batch))
			for idx := range batch {
				analysisInputs = append(analysisInputs, core.EventContent{
					Index:     idx + 1,
					Content:   batch[idx].Body,
					ProjectID: batch[idx].ProjectID,
				})
			}

			analysis, analysisErr := s.intelligence.AnalyzeEvents(ctx, analysisInputs)
			if analysisErr == nil {
				usedAnalysis = true
				if analysis != nil {
					analysisEntities = append(analysisEntities, analysis.Entities...)
				}
			}
		}

		for j := range batch {
			mem := &batch[j]
			if hasProcessingStep(mem, MetaEntitiesExtracted) {
				continue
			}

			method := MethodHeuristic
			if usedAnalysis {
				candidateEntities := selectAnalysisEntitiesForContent(analysisEntities, mem.Body)
				if len(candidateEntities) > 0 {
					if err := s.linkEntitiesFromAnalysis(ctx, mem.ID, candidateEntities); err != nil {
						return enriched, fmt.Errorf("link analysis entities for memory %s: %w", mem.ID, err)
					}
					method = MethodLLM
				} else {
					if err := s.linkEntitiesToMemory(ctx, mem.ID, mem.Body); err != nil {
						return enriched, fmt.Errorf("link entities for memory %s: %w", mem.ID, err)
					}
				}
			} else {
				if err := s.linkEntitiesToMemory(ctx, mem.ID, mem.Body); err != nil {
					return enriched, fmt.Errorf("link entities for memory %s: %w", mem.ID, err)
				}
			}

			markEntitiesExtracted(mem, method)
			mem.UpdatedAt = time.Now().UTC()
			if err := s.repo.UpdateMemory(ctx, mem); err != nil {
				return enriched, fmt.Errorf("update enriched memory %s: %w", mem.ID, err)
			}

			enriched++
		}
	}

	return enriched, nil
}
