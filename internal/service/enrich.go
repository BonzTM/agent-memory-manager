package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

// EnrichMemories finds memories that have not had entity extraction and
// enriches them by linking entities and recording processing ledger metadata.
//
// This pass is additive-only: it never changes authored memory content fields.
func (s *AMMService) EnrichMemories(ctx context.Context) (int, error) {
	slog.Debug("EnrichMemories called")
	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Status: core.MemoryStatusActive,
		Limit:  10000,
	})
	if err != nil {
		return 0, fmt.Errorf("list memories for enrichment: %w", err)
	}

	pending := make([]core.Memory, 0, len(memories))
	for _, mem := range memories {
		if !hasProcessingStep(&mem, MetaEntitiesExtracted) {
			pending = append(pending, mem)
		}
	}

	if len(pending) == 0 {
		return 0, nil
	}

	enriched := 0
	batchSize := s.reflectLLMBatchSize
	if batchSize <= 0 {
		batchSize = defaultReflectLLMBatchSize
	}

	for i := 0; i < len(pending); i += batchSize {
		end := i + batchSize
		if end > len(pending) {
			end = len(pending)
		}
		batch := pending[i:end]

		analysisEntities := make([]core.EntityCandidate, 0)
		analysisRelationships := make([]core.RelationshipCandidate, 0)
		usedAnalysis := false
		if s.intelligence != nil && s.intelligence.IsLLMBacked() {
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
					analysisRelationships = append(analysisRelationships, analysis.Relationships...)
				}
			}
		}

		for j := range batch {
			mem := &batch[j]

			method := MethodHeuristic
			if usedAnalysis {
				candidateEntities := selectAnalysisEntitiesForContent(analysisEntities, mem.Body)
				candidateRelationships := selectAnalysisRelationshipsForContent(analysisRelationships, analysisEntities, mem.Body)
				if len(candidateEntities) > 0 {
					if err := s.linkEntitiesFromAnalysis(ctx, mem.ID, candidateEntities); err != nil {
						return enriched, fmt.Errorf("link analysis entities for memory %s: %w", mem.ID, err)
					}
				} else {
					if err := s.linkEntitiesToMemory(ctx, mem.ID, mem.Body); err != nil {
						return enriched, fmt.Errorf("link entities for memory %s: %w", mem.ID, err)
					}
				}
				if len(candidateRelationships) > 0 {
					if err := s.createRelationshipsFromAnalysis(ctx, candidateRelationships); err != nil {
						return enriched, fmt.Errorf("create analysis relationships for memory %s: %w", mem.ID, err)
					}
				}
				if len(candidateEntities) > 0 || len(candidateRelationships) > 0 {
					method = MethodLLM
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
