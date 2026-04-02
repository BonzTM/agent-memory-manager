package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const defaultBatchSize = 20

// Reprocess re-extracts memory candidates from stored events, creating new
// memories and superseding duplicate older ones. When reprocessAll is false, it
// skips events already covered by LLM-extracted memories.
func (s *AMMService) Reprocess(ctx context.Context, reprocessAll bool) (int, int, error) {
	slog.Debug("Reprocess called", "reprocess_all", reprocessAll)
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
	retractedMemories := make([]*core.Memory, 0)
	for i := range existingMemories {
		mem := &existingMemories[i]
		if mem.Status == core.MemoryStatusActive {
			activeMemories = append(activeMemories, mem)
		}
		if mem.Status == core.MemoryStatusRetracted {
			retractedMemories = append(retractedMemories, mem)
		}
		for _, eid := range mem.SourceEventIDs {
			eventToMemories[eid] = append(eventToMemories[eid], mem)
		}
	}

	// For session events, reprocess means: delete session summaries and clear
	// reflected_at so ConsolidateSessions re-processes the session on the next
	// maintenance run. Session events don't go through per-event extraction.
	sessionIDs := make(map[string]bool)
	var toProcess []core.Event
	isLLMBacked := s.intelligence != nil && s.intelligence.IsLLMBacked()
	for _, evt := range events {
		if mode, ok := evt.Metadata["ingestion_mode"]; ok {
			if mode == "ignore" {
				continue
			}
			if mode == "read_only" && !isLLMBacked {
				continue
			}
		}

		// Session events are handled by ConsolidateSessions, not per-event extraction.
		if evt.SessionID != "" {
			sessionIDs[evt.SessionID] = true
			continue
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

	// Clear reflected_at on session events so ConsolidateSessions re-processes them.
	for _, evt := range events {
		if evt.SessionID == "" || !sessionIDs[evt.SessionID] {
			continue
		}
		if evt.ReflectedAt != nil {
			evt.ReflectedAt = nil
			if err := s.repo.UpdateEvent(ctx, &evt); err != nil {
				slog.Warn("reprocess: failed to clear reflected_at", "event_id", evt.ID, "error", err)
			}
		}
	}

	// Delete stale session summaries and episodes so ConsolidateSessions
	// rebuilds cleanly instead of appending incremental "(continued)"
	// summaries or duplicating episodes.
	for sessionID := range sessionIDs {
		summaries, err := s.repo.ListSummaries(ctx, core.ListSummariesOptions{
			Kind:      "session",
			SessionID: sessionID,
			Limit:     100,
		})
		if err != nil {
			slog.Warn("reprocess: failed to list session summaries", "session_id", sessionID, "error", err)
		} else {
			for _, sum := range summaries {
				if err := s.repo.DeleteSummary(ctx, sum.ID); err != nil {
					slog.Warn("reprocess: failed to delete session summary", "summary_id", sum.ID, "error", err)
				}
			}
		}

		episodes, err := s.repo.ListEpisodes(ctx, core.ListEpisodesOptions{
			SessionID: sessionID,
			Limit:     100,
		})
		if err != nil {
			slog.Warn("reprocess: failed to list session episodes", "session_id", sessionID, "error", err)
		} else {
			for _, ep := range episodes {
				if err := s.repo.DeleteEpisode(ctx, ep.ID); err != nil {
					slog.Warn("reprocess: failed to delete session episode", "episode_id", ep.ID, "error", err)
				}
			}
		}
	}

	// Rebuild session data immediately by running consolidation with no
	// idle timeout so reprocess doesn't leave sessions in a half-torn-down state.
	if len(sessionIDs) > 0 {
		savedTimeout := s.sessionIdleTimeout
		s.sessionIdleTimeout = 0
		if _, err := s.ConsolidateSessions(ctx); err != nil {
			slog.Warn("reprocess: consolidate_sessions after session reset failed", "error", err)
		}
		s.sessionIdleTimeout = savedTimeout
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
		if s.intelligence != nil && s.intelligence.IsLLMBacked() {
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
		analysisRelationships := make([]core.RelationshipCandidate, 0)
		if s.intelligence != nil && s.intelligence.IsLLMBacked() {
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
				analysisRelationships = append(analysisRelationships, analysis.Relationships...)
			}
		} else {
			extracted, err := s.intelligence.ExtractMemoryCandidateBatch(ctx, contents)
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

			if matchesRetractedMemory(retractedMemories, candidateMemory) {
				continue
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
					markExtracted(duplicate, method, s.extractionModelName(), false)
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
						candidateRelationships := selectAnalysisRelationshipsForContent(analysisRelationships, analysisEntities, sourceContent)
						if len(candidateRelationships) > 0 {
							if err := s.createRelationshipsFromAnalysis(ctx, candidateRelationships); err != nil {
								return created, superseded, fmt.Errorf("create reprocessed relationships: %w", err)
							}
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
				ID:               core.GenerateID("mem_"),
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
			markExtracted(mem, method, s.extractionModelName(), false)
			setProcessingMeta(mem, "source_system", "reprocess")
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
					candidateRelationships := selectAnalysisRelationshipsForContent(analysisRelationships, analysisEntities, sourceContent)
					if len(candidateRelationships) > 0 {
						if err := s.createRelationshipsFromAnalysis(ctx, candidateRelationships); err != nil {
							return created, superseded, fmt.Errorf("create reprocessed relationships: %w", err)
						}
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
	if s.intelligence != nil && s.intelligence.IsLLMBacked() {
		return MethodLLM
	}
	return MethodHeuristic
}

func (s *AMMService) extractionModelName() string {
	if s.intelligence == nil {
		return ""
	}
	return s.intelligence.ModelName()
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
		if candidateMatchesContent(entity, content) {
			selected = append(selected, entity)
		}
	}
	return selected
}

func selectAnalysisRelationshipsForContent(relationships []core.RelationshipCandidate, entities []core.EntityCandidate, sourceContent string) []core.RelationshipCandidate {
	if len(relationships) == 0 {
		return nil
	}
	content := strings.ToLower(strings.TrimSpace(sourceContent))
	if content == "" {
		return nil
	}
	entityByCanonical := make(map[string]core.EntityCandidate, len(entities))
	for _, entity := range entities {
		canonical := strings.ToLower(strings.TrimSpace(entity.CanonicalName))
		if canonical == "" {
			continue
		}
		entityByCanonical[canonical] = entity
	}
	selected := make([]core.RelationshipCandidate, 0, len(relationships))
	for _, relationship := range relationships {
		fromTerm := strings.ToLower(strings.TrimSpace(relationship.FromEntity))
		toTerm := strings.ToLower(strings.TrimSpace(relationship.ToEntity))
		if fromTerm == "" || toTerm == "" {
			continue
		}

		fromMatches := strings.Contains(content, fromTerm)
		if !fromMatches {
			if fromEntity, ok := entityByCanonical[fromTerm]; ok {
				fromMatches = candidateMatchesContent(fromEntity, content)
			}
		}

		toMatches := strings.Contains(content, toTerm)
		if !toMatches {
			if toEntity, ok := entityByCanonical[toTerm]; ok {
				toMatches = candidateMatchesContent(toEntity, content)
			}
		}

		if fromMatches && toMatches {
			selected = append(selected, relationship)
		}
	}
	return selected
}
