package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const defaultBatchSize = 20

// Reprocess re-extracts memory candidates from stored events, creating new
// memories and superseding duplicate older ones. When reprocessAll is false, it
// skips events already covered by LLM-extracted memories.
func (s *AMMService) Reprocess(ctx context.Context, reprocessAll bool) (int, int, error) {
	events, err := s.repo.ListEvents(ctx, core.ListEventsOptions{
		Limit: 50000,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("list events for reprocess: %w", err)
	}
	if len(events) == 0 {
		return 0, 0, nil
	}

	existingMemories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Limit: 50000,
	})
	if err != nil {
		return 0, 0, fmt.Errorf("list memories for reprocess: %w", err)
	}

	eventToMemories := make(map[string][]*core.Memory)
	activeMemories := make([]*core.Memory, 0, len(existingMemories))
	for i := range existingMemories {
		mem := &existingMemories[i]
		if mem.Status == core.MemoryStatusActive {
			activeMemories = append(activeMemories, mem)
		}
		for _, eid := range mem.SourceEventIDs {
			eventToMemories[eid] = append(eventToMemories[eid], mem)
		}
	}

	var toProcess []core.Event
	for _, evt := range events {
		if mode, ok := evt.Metadata["ingestion_mode"]; ok {
			if mode == "ignore" {
				continue
			}
			if mode == "read_only" && !s.hasLLMSummarizer {
				continue
			}
		}

		if !reprocessAll {
			mems := eventToMemories[evt.ID]
			allLLM := len(mems) > 0
			for _, m := range mems {
				if !hasLLMProcessingStep(m, MetaExtractionMethod) {
					allLLM = false
					break
				}
			}
			if allLLM {
				continue
			}
		}
		toProcess = append(toProcess, evt)
	}

	if len(toProcess) == 0 {
		return 0, 0, nil
	}

	batchSize := s.reprocessBatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}
	created := 0
	superseded := 0

	for i := 0; i < len(toProcess); i += batchSize {
		end := i + batchSize
		if end > len(toProcess) {
			end = len(toProcess)
		}
		batch := toProcess[i:end]
		if len(batch) == 0 {
			continue
		}

		filtered := batch
		if s.hasLLMSummarizer && s.intelligence != nil {
			triaged, triageErr := s.filterReflectEventsByTriage(ctx, batch)
			if triageErr == nil {
				filtered = triaged
			}
		}
		if len(filtered) == 0 {
			continue
		}

		contents := make([]string, len(filtered))
		for j, evt := range filtered {
			contents[j] = evt.Content
		}

		candidates := make([]core.MemoryCandidate, 0)
		analysisEntities := make([]core.EntityCandidate, 0)
		if s.intelligence != nil && s.hasLLMSummarizer {
			analysisInputs := make([]core.EventContent, 0, len(filtered))
			for idx, evt := range filtered {
				analysisInputs = append(analysisInputs, core.EventContent{
					Index:     idx + 1,
					Content:   evt.Content,
					ProjectID: evt.ProjectID,
					SessionID: evt.SessionID,
				})
			}
			analysis, err := s.intelligence.AnalyzeEvents(ctx, analysisInputs)
			if err != nil {
				return created, superseded, fmt.Errorf("batch analyze (batch %d): %w", i/batchSize, err)
			}
			if analysis != nil {
				candidates = append(candidates, analysis.Memories...)
				analysisEntities = append(analysisEntities, analysis.Entities...)
			}
		} else {
			extracted, err := s.summarizer.ExtractMemoryCandidateBatch(ctx, contents)
			if err != nil {
				return created, superseded, fmt.Errorf("batch extract (batch %d): %w", i/batchSize, err)
			}
			candidates = append(candidates, extracted...)
		}

		for _, candidate := range candidates {
			candidate, ok := prepareMemoryCandidate(candidate)
			if !ok {
				continue
			}

			candidateEvents, ok := resolveCandidateEvents(filtered, candidate.SourceEventNums)
			if !ok {
				continue
			}
			scope, projectID := inferScopeFromEvents(candidateEvents)
			sourceEventIDs := eventIDsFromEvents(candidateEvents)
			sourceContent := joinEventContent(candidateEvents)
			importance := importanceForCandidate(candidate)

			candidateMemory := core.Memory{
				Type:             candidate.Type,
				Scope:            scope,
				ProjectID:        projectID,
				Subject:          candidate.Subject,
				Body:             candidate.Body,
				TightDescription: candidate.TightDescription,
				Confidence:       candidate.Confidence,
				Importance:       importance,
				Status:           core.MemoryStatusActive,
				SourceEventIDs:   sourceEventIDs,
			}

			duplicates := findDuplicateActiveMemories(activeMemories, candidateMemory)
			if len(duplicates) > 0 {
				now := time.Now().UTC()
				duplicate := selectDuplicateKeeper(duplicates)
				duplicate.SourceEventIDs = mergeUniqueStrings(duplicate.SourceEventIDs, sourceEventIDs)
				for _, sibling := range duplicates {
					if sibling == nil || sibling.ID == duplicate.ID {
						continue
					}
					duplicate.SourceEventIDs = mergeUniqueStrings(duplicate.SourceEventIDs, sibling.SourceEventIDs)
				}
				if candidateMemory.Confidence > duplicate.Confidence {
					duplicate.Confidence = candidateMemory.Confidence
				}
				if candidateMemory.Importance > duplicate.Importance {
					duplicate.Importance = candidateMemory.Importance
				}
				if shouldUpgradeDuplicateContent(duplicate, candidateMemory, s.extractionMethod()) {
					duplicate.Subject = candidateMemory.Subject
					duplicate.Body = candidateMemory.Body
					duplicate.TightDescription = candidateMemory.TightDescription
				}
				method := s.extractionMethod()
				markUpgraded := shouldMarkAsUpgraded(duplicate, method)
				if getProcessingMeta(duplicate, MetaExtractionMethod) == "" || method == MethodLLM {
					markExtracted(duplicate, method, s.extractionModelName())
					if markUpgraded {
						setProcessingMeta(duplicate, MetaExtractionQuality, QualityUpgraded)
					}
				}
				duplicate.UpdatedAt = now
				if err := s.repo.UpdateMemory(ctx, duplicate); err != nil {
					return created, superseded, fmt.Errorf("update duplicate memory %s: %w", duplicate.ID, err)
				}
				for _, sibling := range duplicates {
					if sibling == nil || sibling.ID == duplicate.ID || sibling.Status == core.MemoryStatusSuperseded {
						continue
					}
					supNow := time.Now().UTC()
					sibling.Status = core.MemoryStatusSuperseded
					sibling.SupersededBy = duplicate.ID
					sibling.SupersededAt = &supNow
					sibling.UpdatedAt = supNow
					if err := s.repo.UpdateMemory(ctx, sibling); err != nil {
						return created, superseded, fmt.Errorf("supersede duplicate sibling %s: %w", sibling.ID, err)
					}
					superseded++
				}
				for _, eid := range duplicate.SourceEventIDs {
					eventToMemories[eid] = appendMemoryRefIfMissing(eventToMemories[eid], duplicate)
				}

				if len(analysisEntities) > 0 {
					candidateEntities := selectAnalysisEntitiesForContent(analysisEntities, sourceContent)
					if len(candidateEntities) > 0 {
						if err := s.linkEntitiesFromAnalysis(ctx, duplicate.ID, candidateEntities); err != nil {
							return created, superseded, fmt.Errorf("link reprocessed analysis entities: %w", err)
						}
					}
				}
				if err := s.linkEntitiesToMemory(ctx, duplicate.ID, sourceContent); err != nil {
					return created, superseded, fmt.Errorf("link reprocessed entities: %w", err)
				}
				continue
			}

			now := time.Now().UTC()
			method := s.extractionMethod()
			mem := &core.Memory{
				ID:               generateID("mem_"),
				Type:             candidate.Type,
				Scope:            scope,
				ProjectID:        projectID,
				Subject:          candidate.Subject,
				Body:             candidate.Body,
				TightDescription: candidate.TightDescription,
				Confidence:       candidate.Confidence,
				Importance:       importance,
				PrivacyLevel:     core.PrivacyPrivate,
				Status:           core.MemoryStatusActive,
				SourceEventIDs:   sourceEventIDs,
				CreatedAt:        now,
				UpdatedAt:        now,
			}
			markExtracted(mem, method, s.extractionModelName())
			if shouldMarkInsertedAsUpgraded(eventToMemories, sourceEventIDs, method) {
				setProcessingMeta(mem, MetaExtractionQuality, QualityUpgraded)
			}

			if err := s.repo.InsertMemory(ctx, mem); err != nil {
				return created, superseded, fmt.Errorf("insert reprocessed memory: %w", err)
			}
			if len(analysisEntities) > 0 {
				candidateEntities := selectAnalysisEntitiesForContent(analysisEntities, sourceContent)
				if len(candidateEntities) > 0 {
					if err := s.linkEntitiesFromAnalysis(ctx, mem.ID, candidateEntities); err != nil {
						return created, superseded, fmt.Errorf("link reprocessed analysis entities: %w", err)
					}
				}
			}
			if err := s.linkEntitiesToMemory(ctx, mem.ID, sourceContent); err != nil {
				return created, superseded, fmt.Errorf("link reprocessed entities: %w", err)
			}
			created++
			activeMemories = append(activeMemories, mem)
			for _, eid := range sourceEventIDs {
				eventToMemories[eid] = appendMemoryRefIfMissing(eventToMemories[eid], mem)
			}

			for _, eid := range sourceEventIDs {
				for _, old := range eventToMemories[eid] {
					if old.Status == core.MemoryStatusSuperseded {
						continue
					}
					if old.ID == mem.ID {
						continue
					}
					if hasLLMProcessingStep(old, MetaExtractionMethod) && !reprocessAll {
						continue
					}
					if !memoriesLikelyDuplicate(*old, *mem) {
						continue
					}
					supNow := time.Now().UTC()
					old.Status = core.MemoryStatusSuperseded
					old.SupersededBy = mem.ID
					old.SupersededAt = &supNow
				if err := s.repo.UpdateMemory(ctx, old); err != nil {
					return created, superseded, fmt.Errorf("supersede memory %s: %w", old.ID, err)
				}
				superseded++
				}
			}
		}
	}

	return created, superseded, nil
}

func (s *AMMService) extractionMethod() string {
	if _, ok := s.summarizer.(*LLMSummarizer); ok {
		return MethodLLM
	}
	return MethodHeuristic
}

func (s *AMMService) extractionModelName() string {
	llm, ok := s.summarizer.(*LLMSummarizer)
	if !ok || llm == nil {
		return ""
	}
	return llm.model
}

func shouldMarkAsUpgraded(mem *core.Memory, method string) bool {
	if method != MethodLLM || mem == nil {
		return false
	}
	return needsLLMUpgrade(mem, MetaExtractionMethod) || getProcessingMeta(mem, MetaExtractionQuality) == QualityProvisional
}

func shouldMarkInsertedAsUpgraded(eventToMemories map[string][]*core.Memory, sourceEventIDs []string, method string) bool {
	if method != MethodLLM {
		return false
	}
	for _, eid := range sourceEventIDs {
		for _, mem := range eventToMemories[eid] {
			if mem == nil {
				continue
			}
			if needsLLMUpgrade(mem, MetaExtractionMethod) || getProcessingMeta(mem, MetaExtractionQuality) == QualityProvisional {
				return true
			}
		}
	}
	return false
}

func resolveCandidateEvents(batch []core.Event, sourceEventNums []int) ([]core.Event, bool) {
	if len(batch) == 0 {
		return nil, false
	}
	if len(sourceEventNums) == 0 {
		if len(batch) == 1 {
			return batch, true
		}
		return nil, false
	}

	seen := make(map[int]bool, len(sourceEventNums))
	resolved := make([]core.Event, 0, len(sourceEventNums))
	for _, num := range sourceEventNums {
		idx := num - 1
		if idx < 0 || idx >= len(batch) {
			return nil, false
		}
		if seen[idx] {
			continue
		}
		seen[idx] = true
		resolved = append(resolved, batch[idx])
	}
	if len(resolved) == 0 {
		return nil, false
	}
	return resolved, true
}

func eventIDsFromEvents(events []core.Event) []string {
	ids := make([]string, 0, len(events))
	for _, evt := range events {
		if evt.ID == "" {
			continue
		}
		ids = append(ids, evt.ID)
	}
	return ids
}

func appendMemoryRefIfMissing(existing []*core.Memory, mem *core.Memory) []*core.Memory {
	for _, current := range existing {
		if current != nil && mem != nil && current.ID == mem.ID {
			return existing
		}
	}
	return append(existing, mem)
}

func selectAnalysisEntitiesForContent(entities []core.EntityCandidate, sourceContent string) []core.EntityCandidate {
	if len(entities) == 0 {
		return nil
	}
	content := strings.ToLower(strings.TrimSpace(sourceContent))
	if content == "" {
		return nil
	}
	selected := make([]core.EntityCandidate, 0, len(entities))
	for _, entity := range entities {
		if analysisEntityMatchesContent(entity, content) {
			selected = append(selected, entity)
		}
	}
	return selected
}

func analysisEntityMatchesContent(entity core.EntityCandidate, content string) bool {
	if strings.TrimSpace(content) == "" {
		return false
	}
	candidates := make([]string, 0, len(entity.Aliases)+1)
	candidates = append(candidates, entity.CanonicalName)
	candidates = append(candidates, entity.Aliases...)
	for _, candidate := range candidates {
		term := strings.ToLower(strings.TrimSpace(candidate))
		if term == "" {
			continue
		}
		if strings.Contains(content, term) {
			return true
		}
	}
	return false
}
