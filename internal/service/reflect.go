package service

import (
	"context"
	"fmt"
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
	created := 0
	processedCount := 0
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

		filtered := filterReflectEventsByMetadata(events, s.hasLLMSummarizer)
		if s.hasLLMSummarizer && s.intelligence != nil {
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

			candidates, err := s.summarizer.ExtractMemoryCandidateBatch(ctx, contents)
			if err != nil {
				return created, fmt.Errorf("extract memory candidate batch: %w", err)
			}
			if len(candidates) == 0 {
				continue
			}

			for _, rawCandidate := range candidates {
				candidate, ok := prepareMemoryCandidate(rawCandidate)
				if !ok {
					continue
				}
				candidateEvents, ok := resolveCandidateEvents(batch, candidate.SourceEventNums)
				if !ok {
					continue
				}

				scope, projectID := inferScopeFromEvents(candidateEvents)
				sourceEventIDs := eventIDsFromEvents(candidateEvents)
				if len(sourceEventIDs) == 0 {
					continue
				}
				sourceContent := joinEventContent(candidateEvents)

				now := time.Now().UTC()
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

				fuzzyText := strings.TrimSpace(strings.Join([]string{candidateMemory.Subject, candidateMemory.TightDescription, candidateMemory.Body}, " "))
				existing, err := s.repo.SearchMemoriesFuzzy(ctx, fuzzyText, core.ListMemoriesOptions{Type: candidate.Type, Scope: scope, ProjectID: projectID, Status: core.MemoryStatusActive, Limit: 100})
				if err != nil {
					return created, fmt.Errorf("search memories for reflect duplicate detection: %w", err)
				}
				activeMemories := make([]*core.Memory, 0, len(existing))
				for i := range existing {
					activeMemories = append(activeMemories, &existing[i])
				}

				duplicates := findDuplicateActiveMemories(activeMemories, candidateMemory)
				if len(duplicates) > 0 {
					duplicate := selectDuplicateKeeper(duplicates)
					duplicate.SourceEventIDs = mergeUniqueStrings(duplicate.SourceEventIDs, candidateMemory.SourceEventIDs)
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
					if getProcessingMeta(duplicate, MetaExtractionMethod) == "" || method == MethodLLM {
						markExtracted(duplicate, method, s.extractionModelName())
					}
					duplicate.UpdatedAt = now
					if err := s.repo.UpdateMemory(ctx, duplicate); err != nil {
						return created, fmt.Errorf("update duplicate reflected memory %s: %w", duplicate.ID, err)
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
							return created, fmt.Errorf("supersede duplicate reflected sibling %s: %w", sibling.ID, err)
						}
					}

					if err := s.linkEntitiesToMemory(ctx, duplicate.ID, sourceContent); err != nil {
						return created, fmt.Errorf("link reflected entities: %w", err)
					}
					continue
				}

				mem := &core.Memory{
					ID:               generateID("mem_"),
					Type:             candidateMemory.Type,
					Scope:            candidateMemory.Scope,
					ProjectID:        candidateMemory.ProjectID,
					Subject:          candidateMemory.Subject,
					Body:             candidateMemory.Body,
					TightDescription: candidateMemory.TightDescription,
					Confidence:       candidateMemory.Confidence,
					Importance:       importance,
					PrivacyLevel:     core.PrivacyPrivate,
					Status:           core.MemoryStatusActive,
					SourceEventIDs:   candidateMemory.SourceEventIDs,
					CreatedAt:        now,
					UpdatedAt:        now,
				}
				markExtracted(mem, s.extractionMethod(), s.extractionModelName())

				if err := s.repo.InsertMemory(ctx, mem); err != nil {
					return created, fmt.Errorf("insert reflected memory: %w", err)
				}

				if err := s.linkEntitiesToMemory(ctx, mem.ID, sourceContent); err != nil {
					return created, fmt.Errorf("link reflected entities: %w", err)
				}

				created++
			}
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

func filterReflectEventsByMetadata(events []core.Event, hasLLMSummarizer bool) []core.Event {
	filtered := make([]core.Event, 0, len(events))
	for _, evt := range events {
		if mode, ok := evt.Metadata["ingestion_mode"]; ok {
			if mode == "ignore" {
				continue
			}
			if mode == "read_only" && !hasLLMSummarizer {
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
			ID:               generateID("rel_"),
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
			if entityMatchesTerm(existing[i], canonicalName) || entityMatchesTerm(existing[i], term) {
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
		ID:            generateID("ent_"),
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
		if entityMatchesTerm(existing[i], term) {
			entity := existing[i]
			return &entity, nil
		}
	}
	return nil, nil
}

func relationshipDedupKey(fromEntityID, toEntityID, relationshipType string) string {
	return strings.TrimSpace(fromEntityID) + "|" + strings.TrimSpace(toEntityID) + "|" + strings.ToLower(strings.TrimSpace(relationshipType))
}

func entityMatchesTerm(entity core.Entity, term string) bool {
	needle := normalizeEntityTerm(term)
	if needle == "" {
		return false
	}
	if normalizeEntityTerm(entity.CanonicalName) == needle {
		return true
	}
	for _, alias := range entity.Aliases {
		if normalizeEntityTerm(alias) == needle {
			return true
		}
	}
	return false
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

func normalizeEntityTerm(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
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

func extractTightDescription(content string, maxLen int) string {
	for i, ch := range content {
		if i >= maxLen {
			break
		}
		if ch == '.' || ch == '!' || ch == '?' {
			return content[:i+1]
		}
	}
	if len(content) <= maxLen {
		return content
	}
	return content[:maxLen]
}
