package service

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	defaultReflectBatchSize    = 100
	defaultReflectLLMBatchSize = 20
)

func (s *AMMService) Reflect(ctx context.Context, jobID string) (int, error) {
	slog.Debug("Reflect called", "job_id", jobID)
	created := 0
	processedCount := 0
	retryEvents := make([]core.Event, 0)
	claimBatchSize := s.reflectBatchSize
	if claimBatchSize <= 0 {
		claimBatchSize = defaultReflectBatchSize
	}
	llmBatchSize := s.reflectLLMBatchSize
	if llmBatchSize <= 0 {
		llmBatchSize = defaultReflectLLMBatchSize
	}

	for {
		events, err := s.repo.ClaimUnreflectedEvents(ctx, claimBatchSize)
		if err != nil {
			return created, fmt.Errorf("claim events for reflect: %w", err)
		}
		if len(events) == 0 {
			break
		}
		processedCount += len(events)

		isLLMBacked := s.intelligence != nil && s.intelligence.IsLLMBacked()
		filtered := filterReflectEventsByMetadata(events, isLLMBacked)
		if isLLMBacked {
			triaged, triageErr := s.filterReflectEventsByTriage(ctx, events)
			if triageErr == nil {
				filtered = triaged
			}
		}
		if len(filtered) == 0 {
			continue
		}

		for i := 0; i < len(filtered); i += llmBatchSize {
			end := i + llmBatchSize
			if end > len(filtered) {
				end = len(filtered)
			}
			batch := filtered[i:end]

			contents := make([]string, 0, len(batch))
			for _, evt := range batch {
				contents = append(contents, evt.Content)
			}

			candidates := make([]core.MemoryCandidate, 0)
			analysisEntities := make([]core.EntityCandidate, 0)
			analysisRelationships := make([]core.RelationshipCandidate, 0)
			var eventQuality map[int]string
			usedAnalysis := false
			extractionMethod := MethodHeuristic
			retryableHeuristic := false

			if s.intelligence != nil && s.intelligence.IsLLMBacked() {
				analysisInputs := make([]core.EventContent, 0, len(batch))
				for idx, evt := range batch {
					analysisInputs = append(analysisInputs, core.EventContent{
						Index:     idx + 1,
						Content:   evt.Content,
						ProjectID: evt.ProjectID,
						SessionID: evt.SessionID,
					})
				}

				analysis, analysisMethod, analysisErr := analyzeEventsWithMethod(ctx, s.intelligence, analysisInputs)
				if analysisErr == nil {
					if analysis != nil {
						candidates = append(candidates, analysis.Memories...)
						analysisEntities = append(analysisEntities, analysis.Entities...)
						analysisRelationships = append(analysisRelationships, analysis.Relationships...)
						eventQuality = analysis.EventQuality
					}
					usedAnalysis = len(candidates) > 0
					if usedAnalysis {
						extractionMethod = analysisMethod
						retryableHeuristic = analysisMethod == MethodHeuristic
					}
					if len(candidates) == 0 {
						extracted, method, err := extractBatchWithMethod(ctx, s.intelligence, contents)
						if err != nil {
							return created, fmt.Errorf("extract memory candidate batch: %w", err)
						}
						extractionMethod = method
						retryableHeuristic = method == MethodHeuristic
						candidates = append(candidates, extracted...)
					}
				} else {
					extracted, method, err := extractBatchWithMethod(ctx, s.intelligence, contents)
					if err != nil {
						return created, fmt.Errorf("extract memory candidate batch: %w", err)
					}
					extractionMethod = method
					retryableHeuristic = method == MethodHeuristic
					candidates = append(candidates, extracted...)
				}
			} else {
				extracted, method, err := extractBatchWithMethod(ctx, s.intelligence, contents)
				if err != nil {
					return created, fmt.Errorf("extract memory candidate batch: %w", err)
				}
				extractionMethod = method
				candidates = append(candidates, extracted...)
			}

			if len(candidates) == 0 {
				if retryableHeuristic {
					retryBatch, err := s.recordReflectFallbackAttempt(ctx, batch)
					if err != nil {
						return created, fmt.Errorf("record empty reflect fallback attempt: %w", err)
					}
					retryEvents = append(retryEvents, retryBatch...)
				} else if err := s.clearReflectFallbackAttempts(ctx, batch); err != nil {
					return created, fmt.Errorf("clear reflect fallback state: %w", err)
				}
				continue
			}

			sourceEventIDs := eventIDsFromEvents(batch)
			batchCreated, err := s.processMemoryCandidates(ctx, candidateProcessingInput{
				candidates:            candidates,
				sourceEvents:          batch,
				eventQuality:          eventQuality,
				analysisEntities:      analysisEntities,
				analysisRelationships: analysisRelationships,
				usedAnalysis:          usedAnalysis,
				sourceSystem:          "reflect",
				extractionMethod:      extractionMethod,
				retryableHeuristic:    retryableHeuristic,
			})
			if err != nil {
				return created, err
			}
			created += batchCreated

			if retryableHeuristic {
				retryState, err := s.sourceEventRetryState(ctx, sourceEventIDs)
				if err != nil {
					return created, fmt.Errorf("check retryable reflected events: %w", err)
				}
				if retryState.shouldRetry {
					retryEvents = append(retryEvents, batch...)
					continue
				}
				if !retryState.hasActiveMemory {
					retryBatch, err := s.recordReflectFallbackAttempt(ctx, batch)
					if err != nil {
						return created, fmt.Errorf("record reflect fallback attempt: %w", err)
					}
					retryEvents = append(retryEvents, retryBatch...)
					if len(retryBatch) > 0 {
						continue
					}
				}
			}
			if err := s.clearReflectFallbackAttempts(ctx, batch); err != nil {
				return created, fmt.Errorf("clear reflect fallback state: %w", err)
			}
		}
	}

	// Clear reflected_at on retry events so they are re-claimed on the
	// next Reflect pass via ClaimUnreflectedEvents (WHERE reflected_at IS
	// NULL). This is safe because the reflect job frontier tracks progress
	// separately and ClaimUnreflectedEvents does not use a sequence-based
	// frontier — it relies solely on reflected_at IS NULL.
	if len(retryEvents) > 0 {
		if err := s.clearEventsReflected(ctx, dedupeEventsByID(retryEvents)); err != nil {
			return created, fmt.Errorf("clear reflected events for retry: %w", err)
		}
	}

	finishedAt := time.Now().UTC()
	result := map[string]string{"created": fmt.Sprintf("%d", created), "processed": fmt.Sprintf("%d", processedCount)}

	if jobID != "" {
		if job, err := s.repo.GetJob(ctx, jobID); err == nil {
			job.Status = "completed"
			job.FinishedAt = &finishedAt
			job.Result = result
			if err := s.repo.UpdateJob(ctx, job); err != nil {
				return created, fmt.Errorf("update reflect job: %w", err)
			}
		}
	}

	return created, nil
}

func dedupeEventsByID(events []core.Event) []core.Event {
	if len(events) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(events))
	out := make([]core.Event, 0, len(events))
	for _, evt := range events {
		if evt.ID == "" || seen[evt.ID] {
			continue
		}
		seen[evt.ID] = true
		out = append(out, evt)
	}
	return out
}

func filterReflectEventsByMetadata(events []core.Event, isLLMBacked bool) []core.Event {
	filtered := make([]core.Event, 0, len(events))
	for _, evt := range events {
		// Skip events that belong to a session — ConsolidateSessions handles those.
		if evt.SessionID != "" {
			continue
		}
		if mode, ok := evt.Metadata["ingestion_mode"]; ok {
			if mode == "ignore" {
				continue
			}
			if mode == "read_only" && !isLLMBacked {
				continue
			}
		}
		filtered = append(filtered, evt)
	}
	return filtered
}

func (s *AMMService) filterReflectEventsByTriage(ctx context.Context, events []core.Event) ([]core.Event, error) {
	candidates := make([]core.Event, 0, len(events))
	triageInputs := make([]core.EventContent, 0, len(events))

	for i, evt := range events {
		if mode, ok := evt.Metadata["ingestion_mode"]; ok && mode == "ignore" {
			continue
		}
		index := i + 1
		candidates = append(candidates, evt)
		triageInputs = append(triageInputs, core.EventContent{
			Index:     index,
			Content:   evt.Content,
			ProjectID: evt.ProjectID,
			SessionID: evt.SessionID,
		})
	}

	if len(triageInputs) == 0 {
		return []core.Event{}, nil
	}

	decisions, err := s.intelligence.TriageEvents(ctx, triageInputs)
	if err != nil {
		return nil, err
	}

	filtered := make([]core.Event, 0, len(candidates))
	for i, evt := range candidates {
		index := triageInputs[i].Index
		decision, ok := decisions[index]
		if !ok {
			decision = core.TriageReflect
		}
		if decision == core.TriageSkip {
			continue
		}
		filtered = append(filtered, evt)
	}

	return filtered, nil
}

func joinEventContent(events []core.Event) string {
	parts := make([]string, 0, len(events))
	for _, evt := range events {
		content := strings.TrimSpace(evt.Content)
		if content == "" {
			continue
		}
		parts = append(parts, content)
	}
	if len(parts) == 0 {
		return ""
	}
	parts = slices.Compact(parts)
	return strings.Join(parts, "\n\n")
}

func (s *AMMService) linkEntitiesToMemory(ctx context.Context, memoryID, content string) error {
	names := ExtractEntities(content)
	links := make([]core.MemoryEntityLink, 0, len(names))
	linked := make(map[string]bool, len(names))
	for _, name := range names {
		entity, err := s.findOrCreateEntity(ctx, name)
		if err != nil {
			return err
		}
		if entity == nil {
			continue
		}
		if linked[entity.ID] {
			continue
		}
		linked[entity.ID] = true
		links = append(links, core.MemoryEntityLink{MemoryID: memoryID, EntityID: entity.ID, Role: "mentioned"})
	}
	if len(links) == 0 {
		return nil
	}
	if err := s.repo.LinkMemoryEntitiesBatch(ctx, links); err != nil {
		return err
	}
	return nil
}

func (s *AMMService) linkEntitiesFromAnalysis(ctx context.Context, memoryID string, entities []core.EntityCandidate) error {
	links := make([]core.MemoryEntityLink, 0, len(entities))
	linked := make(map[string]bool, len(entities))
	for _, candidate := range entities {
		entity, err := s.findOrCreateEntityWithDetails(ctx, candidate)
		if err != nil {
			return err
		}
		if entity == nil {
			continue
		}
		if linked[entity.ID] {
			continue
		}
		linked[entity.ID] = true
		links = append(links, core.MemoryEntityLink{MemoryID: memoryID, EntityID: entity.ID, Role: "mentioned"})
	}
	if len(links) == 0 {
		return nil
	}
	if err := s.repo.LinkMemoryEntitiesBatch(ctx, links); err != nil {
		return err
	}
	return nil
}

func (s *AMMService) createRelationshipsFromAnalysis(ctx context.Context, relationships []core.RelationshipCandidate) error {
	pending := make([]*core.Relationship, 0, len(relationships))
	involvedEntityIDs := make(map[string]bool)

	for _, rel := range relationships {
		fromName := strings.TrimSpace(rel.FromEntity)
		toName := strings.TrimSpace(rel.ToEntity)
		relType := strings.TrimSpace(rel.Type)
		if fromName == "" || toName == "" || relType == "" {
			continue
		}

		fromEntity, err := s.findEntityByNameOrAlias(ctx, fromName)
		if err != nil {
			return err
		}
		toEntity, err := s.findEntityByNameOrAlias(ctx, toName)
		if err != nil {
			return err
		}
		if fromEntity == nil || toEntity == nil {
			continue
		}

		now := time.Now().UTC()
		relModel := &core.Relationship{
			ID:               core.GenerateID("rel_"),
			FromEntityID:     fromEntity.ID,
			ToEntityID:       toEntity.ID,
			RelationshipType: relType,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		if strings.TrimSpace(rel.Description) != "" {
			relModel.Metadata = map[string]string{"description": strings.TrimSpace(rel.Description)}
		}
		pending = append(pending, relModel)
		involvedEntityIDs[fromEntity.ID] = true
		involvedEntityIDs[toEntity.ID] = true
	}

	if len(pending) == 0 {
		return nil
	}

	entityIDs := make([]string, 0, len(involvedEntityIDs))
	for entityID := range involvedEntityIDs {
		entityIDs = append(entityIDs, entityID)
	}
	existing, err := s.repo.ListRelationshipsByEntityIDs(ctx, entityIDs)
	if err != nil {
		return err
	}

	existingKeys := make(map[string]bool, len(existing)+len(pending))
	for i := range existing {
		existingKeys[relationshipDedupKey(existing[i].FromEntityID, existing[i].ToEntityID, existing[i].RelationshipType)] = true
	}

	toInsert := make([]*core.Relationship, 0, len(pending))
	for _, rel := range pending {
		key := relationshipDedupKey(rel.FromEntityID, rel.ToEntityID, rel.RelationshipType)
		if existingKeys[key] {
			continue
		}
		existingKeys[key] = true
		toInsert = append(toInsert, rel)
	}

	if len(toInsert) == 0 {
		return nil
	}

	if err := s.repo.InsertRelationshipsBatch(ctx, toInsert); err != nil {
		return err
	}

	return nil
}

func (s *AMMService) findOrCreateEntity(ctx context.Context, canonicalName string) (*core.Entity, error) {
	return s.findOrCreateEntityWithDetails(ctx, core.EntityCandidate{
		CanonicalName: canonicalName,
		Type:          "topic",
	})
}

func (s *AMMService) findOrCreateEntityWithDetails(ctx context.Context, candidate core.EntityCandidate) (*core.Entity, error) {
	canonicalName := strings.TrimSpace(candidate.CanonicalName)
	if canonicalName == "" {
		return nil, nil
	}

	inputType := strings.TrimSpace(candidate.Type)
	if inputType == "" {
		inputType = "topic"
	}
	newAliases := mergeEntityAliases(candidate.Aliases, canonicalName)
	description := strings.TrimSpace(candidate.Description)

	searchTerms := mergeEntityAliases(newAliases, canonicalName)
	for _, alias := range candidate.Aliases {
		searchTerms = append(searchTerms, strings.TrimSpace(alias))
	}
	searchTerms = mergeEntityAliases(searchTerms, canonicalName)

	var matched *core.Entity
	for _, term := range searchTerms {
		if term == "" {
			continue
		}
		existing, err := s.repo.SearchEntities(ctx, term, 100)
		if err != nil {
			return nil, err
		}
		for i := range existing {
			if entityMatchesTerm(&existing[i], canonicalName) || entityMatchesTerm(&existing[i], term) {
				entity := existing[i]
				matched = &entity
				break
			}
		}
		if matched != nil {
			break
		}
	}
	if matched != nil {
		changed := false

		mergedAliases := mergeEntityAliases(matched.Aliases, matched.CanonicalName)
		mergedAliases = mergeEntityAliases(mergedAliases, canonicalName)
		mergedAliases = mergeEntityAliases(mergedAliases, newAliases...)
		if !stringSetEqualFold(mergedAliases, matched.Aliases) {
			matched.Aliases = mergedAliases
			changed = true
		}

		if strings.EqualFold(strings.TrimSpace(matched.Type), "topic") && !strings.EqualFold(inputType, "topic") {
			matched.Type = inputType
			changed = true
		}

		if strings.TrimSpace(matched.Description) == "" && description != "" {
			matched.Description = description
			changed = true
		}

		if changed {
			matched.UpdatedAt = time.Now().UTC()
			if err := s.updateEntity(ctx, matched); err != nil {
				return nil, err
			}
		}
		return matched, nil
	}

	now := time.Now().UTC()
	entity := &core.Entity{
		ID:            core.GenerateID("ent_"),
		Type:          inputType,
		CanonicalName: canonicalName,
		Aliases:       newAliases,
		Description:   description,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := s.repo.InsertEntity(ctx, entity); err != nil {
		return nil, err
	}
	return entity, nil
}

func (s *AMMService) findEntityByNameOrAlias(ctx context.Context, name string) (*core.Entity, error) {
	term := strings.TrimSpace(name)
	if term == "" {
		return nil, nil
	}
	existing, err := s.repo.SearchEntities(ctx, term, 100)
	if err != nil {
		return nil, err
	}
	for i := range existing {
		if entityMatchesTerm(&existing[i], term) {
			entity := existing[i]
			return &entity, nil
		}
	}
	return nil, nil
}

func relationshipDedupKey(fromEntityID, toEntityID, relationshipType string) string {
	return strings.TrimSpace(fromEntityID) + "|" + strings.TrimSpace(toEntityID) + "|" + strings.ToLower(strings.TrimSpace(relationshipType))
}

func mergeEntityAliases(existing []string, candidates ...string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(existing)+len(candidates))
	appendAlias := func(alias string) {
		trimmed := strings.TrimSpace(alias)
		key := normalizeEntityTerm(trimmed)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		result = append(result, trimmed)
	}
	for _, alias := range existing {
		appendAlias(alias)
	}
	for _, alias := range candidates {
		appendAlias(alias)
	}
	return result
}

func stringSetEqualFold(a, b []string) bool {
	set := make(map[string]bool, len(a))
	for _, item := range a {
		set[normalizeEntityTerm(item)] = true
	}
	for _, item := range b {
		key := normalizeEntityTerm(item)
		if !set[key] {
			return false
		}
		delete(set, key)
	}
	return len(set) == 0
}

func (s *AMMService) updateEntity(ctx context.Context, entity *core.Entity) error {
	if entity == nil {
		return nil
	}
	return s.repo.UpdateEntity(ctx, entity)
}
