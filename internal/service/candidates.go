package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

// candidateProcessingInput holds the context needed to process extracted
// memory candidates through the validation/dedup/insert pipeline.
type candidateProcessingInput struct {
	// candidates are the raw MemoryCandidate values from the extraction LLM.
	candidates []core.MemoryCandidate

	// sourceEvents is the batch of events the candidates were derived from.
	// Used for source_event_num resolution and scope inference.
	sourceEvents []core.Event

	// eventQuality is the per-event quality classification from AnalyzeEvents.
	// May be nil when analysis was not used.
	eventQuality map[int]string

	// analysisEntities and analysisRelationships come from AnalyzeEvents.
	// May be nil when analysis was not used.
	analysisEntities      []core.EntityCandidate
	analysisRelationships []core.RelationshipCandidate
	usedAnalysis          bool

	// sourceSystem labels the pipeline that produced these candidates
	// (e.g. "reflect", "consolidate_sessions").
	sourceSystem string

	// scopeOverride, when set, forces scope and projectID instead of
	// inferring from sourceEvents. Used by ConsolidateSessions where
	// scope is already known from the full session.
	scopeOverride   *core.Scope
	projectOverride string

	// sessionID is attached to created memories when non-empty.
	sessionID string

	// extractionMethod records whether the current batch output came from the
	// LLM or from heuristic fallback.
	extractionMethod string

	// retryableHeuristic marks heuristic output that came from an LLM-backed
	// pipeline fallback and should be retried on later passes.
	retryableHeuristic bool
}

// processMemoryCandidates validates, deduplicates, and inserts memory
// candidates. It returns the number of memories created.
func (s *AMMService) processMemoryCandidates(ctx context.Context, input candidateProcessingInput) (int, error) {
	created := 0
	method := input.extractionMethod
	if method == "" {
		method = s.extractionMethod()
	}

	for _, rawCandidate := range input.candidates {
		candidate, ok := prepareMemoryCandidate(rawCandidate)
		if !ok {
			continue
		}
		if !passesIntakeQualityGates(candidate, s.minConfidenceForCreation, s.minImportanceForCreation) {
			slog.Debug("candidate rejected by intake quality gate",
				"confidence", candidate.Confidence,
				"min_confidence", s.minConfidenceForCreation,
				"min_importance", s.minImportanceForCreation,
				"type", candidate.Type,
			)
			continue
		}
		if candidateSourcedFromLowQualityEvents(candidate, input.eventQuality) {
			slog.Debug("candidate rejected: all source events classified as ephemeral or noise",
				"source_events", candidate.SourceEventNums,
				"type", candidate.Type,
			)
			continue
		}

		// Resolve source events and scope.
		var scope core.Scope
		var projectID string
		var sourceEventIDs []string
		var sourceContent string

		if input.scopeOverride != nil {
			// ConsolidateSessions path: scope already known.
			scope = *input.scopeOverride
			projectID = input.projectOverride
			sourceEventIDs = eventIDsFromEvents(input.sourceEvents)
			sourceContent = joinEventContent(input.sourceEvents)
		} else {
			// Reflect path: resolve from source event numbers.
			candidateEvents, ok := resolveCandidateEvents(input.sourceEvents, candidate.SourceEventNums)
			if !ok {
				continue
			}
			scope, projectID = inferScopeFromEvents(candidateEvents)
			sourceEventIDs = eventIDsFromEvents(candidateEvents)
			sourceContent = joinEventContent(candidateEvents)
		}

		if len(sourceEventIDs) == 0 {
			continue
		}

		// Check retracted memories.
		fuzzyText := strings.TrimSpace(strings.Join([]string{candidate.Subject, candidate.TightDescription, candidate.Body}, " "))
		retractedResults, _ := s.repo.SearchMemoriesFuzzy(ctx, fuzzyText, core.ListMemoriesOptions{Type: candidate.Type, Scope: scope, ProjectID: projectID, Status: core.MemoryStatusRetracted, Limit: 20})
		retractedPtrs := make([]*core.Memory, 0, len(retractedResults))
		for i := range retractedResults {
			retractedPtrs = append(retractedPtrs, &retractedResults[i])
		}
		if matchesRetractedMemory(retractedPtrs, core.Memory{
			Type: candidate.Type, Scope: scope, ProjectID: projectID,
			Subject: candidate.Subject, Body: candidate.Body, TightDescription: candidate.TightDescription,
		}) {
			continue
		}

		// Check active duplicates.
		existing, err := s.repo.SearchMemoriesFuzzy(ctx, fuzzyText, core.ListMemoriesOptions{Type: candidate.Type, Scope: scope, ProjectID: projectID, Status: core.MemoryStatusActive, Limit: 100})
		if err != nil {
			return created, fmt.Errorf("search memories for %s duplicate detection: %w", input.sourceSystem, err)
		}
		activeMemories := make([]*core.Memory, 0, len(existing))
		for i := range existing {
			activeMemories = append(activeMemories, &existing[i])
		}
		if len(sourceEventIDs) > 0 {
			relatedBySource, err := s.repo.ListMemoriesBySourceEventIDs(ctx, sourceEventIDs)
			if err != nil {
				return created, fmt.Errorf("list %s memories by source events: %w", input.sourceSystem, err)
			}
			activeMemories = mergeMemoryPointerSets(activeMemories, relatedBySource)
		}

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

		// Select entities/relationships for this candidate.
		candidateEntities := []core.EntityCandidate(nil)
		candidateRelationships := []core.RelationshipCandidate(nil)
		if input.usedAnalysis {
			candidateEntities = selectAnalysisEntitiesForContent(input.analysisEntities, sourceContent)
			candidateRelationships = selectAnalysisRelationshipsForContent(input.analysisRelationships, input.analysisEntities, sourceContent)
		}

		duplicates := findDuplicateActiveMemories(activeMemories, candidateMemory)
		duplicates = mergeDuplicateMemories(duplicates, findRetryUpgradeDuplicates(activeMemories, candidateMemory, method))
		if len(duplicates) > 0 {
			if err := s.handleDuplicateCandidate(ctx, duplicates, candidateMemory, input, candidateEntities, candidateRelationships, sourceContent, method); err != nil {
				return created, err
			}
			continue
		}

		// Insert new memory.
		now := time.Now().UTC()
		mem := &core.Memory{
			ID:               core.GenerateID("mem_"),
			Type:             candidateMemory.Type,
			Scope:            candidateMemory.Scope,
			ProjectID:        candidateMemory.ProjectID,
			SessionID:        input.sessionID,
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
		markExtracted(mem, method, s.extractionModelName(), input.retryableHeuristic)
		setProcessingMeta(mem, "source_system", input.sourceSystem)

		if err := s.repo.InsertMemory(ctx, mem); err != nil {
			return created, fmt.Errorf("insert %s memory: %w", input.sourceSystem, err)
		}

		if err := s.linkCandidateEntities(ctx, mem.ID, sourceContent, input.usedAnalysis, candidateEntities, candidateRelationships); err != nil {
			return created, err
		}

		created++
	}

	return created, nil
}

func mergeMemoryPointerSets(existing []*core.Memory, additional []core.Memory) []*core.Memory {
	if len(additional) == 0 {
		return existing
	}
	seen := make(map[string]bool, len(existing)+len(additional))
	merged := make([]*core.Memory, 0, len(existing)+len(additional))
	for _, mem := range existing {
		if mem == nil || mem.ID == "" || seen[mem.ID] {
			continue
		}
		seen[mem.ID] = true
		merged = append(merged, mem)
	}
	for i := range additional {
		mem := &additional[i]
		if mem.ID == "" || seen[mem.ID] {
			continue
		}
		seen[mem.ID] = true
		merged = append(merged, mem)
	}
	return merged
}

func mergeDuplicateMemories(existing []*core.Memory, additional []*core.Memory) []*core.Memory {
	if len(additional) == 0 {
		return existing
	}
	seen := make(map[string]bool, len(existing)+len(additional))
	merged := make([]*core.Memory, 0, len(existing)+len(additional))
	for _, mem := range existing {
		if mem == nil || mem.ID == "" || seen[mem.ID] {
			continue
		}
		seen[mem.ID] = true
		merged = append(merged, mem)
	}
	for _, mem := range additional {
		if mem == nil || mem.ID == "" || seen[mem.ID] {
			continue
		}
		seen[mem.ID] = true
		merged = append(merged, mem)
	}
	return merged
}

func findRetryUpgradeDuplicates(activeMemories []*core.Memory, candidate core.Memory, extractionMethod string) []*core.Memory {
	if extractionMethod != MethodLLM || len(candidate.SourceEventIDs) == 0 {
		return nil
	}
	sourceIDs := make(map[string]bool, len(candidate.SourceEventIDs))
	for _, id := range candidate.SourceEventIDs {
		if id != "" {
			sourceIDs[id] = true
		}
	}
	if len(sourceIDs) == 0 {
		return nil
	}
	duplicates := make([]*core.Memory, 0, 2)
	for _, existing := range activeMemories {
		if existing == nil || existing.Status != core.MemoryStatusActive {
			continue
		}
		if existing.Type != candidate.Type || existing.Scope != candidate.Scope || existing.ProjectID != candidate.ProjectID {
			continue
		}
		if !needsLLMUpgrade(existing, MetaExtractionMethod) {
			continue
		}
		for _, sourceID := range existing.SourceEventIDs {
			if sourceIDs[sourceID] {
				duplicates = append(duplicates, existing)
				break
			}
		}
	}
	return duplicates
}

// handleDuplicateCandidate merges a candidate into existing duplicate memories.
func (s *AMMService) handleDuplicateCandidate(
	ctx context.Context,
	duplicates []*core.Memory,
	candidateMemory core.Memory,
	input candidateProcessingInput,
	candidateEntities []core.EntityCandidate,
	candidateRelationships []core.RelationshipCandidate,
	sourceContent string,
	method string,
) error {
	now := time.Now().UTC()
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
	if shouldUpgradeDuplicateContent(duplicate, candidateMemory, method) {
		duplicate.Subject = candidateMemory.Subject
		duplicate.Body = candidateMemory.Body
		duplicate.TightDescription = candidateMemory.TightDescription
	}
	if getProcessingMeta(duplicate, MetaExtractionMethod) == "" || method == MethodLLM || (method == MethodHeuristic && input.retryableHeuristic) {
		markExtracted(duplicate, method, s.extractionModelName(), input.retryableHeuristic)
	}
	duplicate.UpdatedAt = now
	if err := s.repo.UpdateMemory(ctx, duplicate); err != nil {
		return fmt.Errorf("update duplicate %s memory %s: %w", input.sourceSystem, duplicate.ID, err)
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
			return fmt.Errorf("supersede duplicate %s sibling %s: %w", input.sourceSystem, sibling.ID, err)
		}
	}

	return s.linkCandidateEntities(ctx, duplicate.ID, sourceContent, input.usedAnalysis, candidateEntities, candidateRelationships)
}

// linkCandidateEntities links entities and relationships to a memory.
func (s *AMMService) linkCandidateEntities(
	ctx context.Context,
	memoryID string,
	sourceContent string,
	usedAnalysis bool,
	candidateEntities []core.EntityCandidate,
	candidateRelationships []core.RelationshipCandidate,
) error {
	if usedAnalysis && len(candidateEntities) > 0 {
		if err := s.linkEntitiesFromAnalysis(ctx, memoryID, candidateEntities); err != nil {
			return fmt.Errorf("link analysis entities: %w", err)
		}
	} else {
		if err := s.linkEntitiesToMemory(ctx, memoryID, sourceContent); err != nil {
			return fmt.Errorf("link entities: %w", err)
		}
	}
	if usedAnalysis && len(candidateRelationships) > 0 {
		if err := s.createRelationshipsFromAnalysis(ctx, candidateRelationships); err != nil {
			return fmt.Errorf("create relationships: %w", err)
		}
	}
	return nil
}
