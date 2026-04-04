package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	entityBriefSummaryKind      = "entity_brief"
	minMemoriesForEntityBrief   = 3
	entityBriefBodyMaxChars     = 2000
	entityBriefMetaEntityID     = "entity_id"
	entityBriefMetaEntityName   = "entity_name"
	entityBriefMetaMemoryCount  = "source_memory_count"
	entityBriefMaxEntities      = 100
	entityBriefMemoryFetchLimit = 20
)

// BuildEntityBriefs generates synthesis summaries for entities with enough
// linked memories. Incremental: skips entities whose existing brief is
// already up to date (no new linked memories since the brief was last
// generated). Tracks extraction metadata and retry state so heuristic
// fallback briefs are retried when LLM becomes available.
func (s *AMMService) BuildEntityBriefs(ctx context.Context) (int, error) {
	slog.Debug("BuildEntityBriefs called")

	entities, err := s.repo.ListEntities(ctx, core.ListEntitiesOptions{Limit: entityBriefMaxEntities})
	if err != nil {
		return 0, fmt.Errorf("list entities: %w", err)
	}

	// Batch-count memory links to filter entities worth briefing.
	entityIDs := make([]string, 0, len(entities))
	entityByID := make(map[string]core.Entity, len(entities))
	for _, ent := range entities {
		entityIDs = append(entityIDs, ent.ID)
		entityByID[ent.ID] = ent
	}
	linkCounts, err := s.repo.CountMemoryEntityLinksBatch(ctx, entityIDs)
	if err != nil {
		return 0, fmt.Errorf("count entity links: %w", err)
	}

	// Find existing briefs to detect which entities already have one.
	existingBriefs, err := s.repo.ListSummaries(ctx, core.ListSummariesOptions{
		Kind:  entityBriefSummaryKind,
		Limit: entityBriefMaxEntities * 2,
	})
	if err != nil {
		return 0, fmt.Errorf("list existing entity briefs: %w", err)
	}
	briefByEntityID := make(map[string]*core.Summary, len(existingBriefs))
	for i := range existingBriefs {
		eid := existingBriefs[i].Metadata[entityBriefMetaEntityID]
		if eid != "" {
			briefByEntityID[eid] = &existingBriefs[i]
		}
	}

	isLLMBacked := s.intelligence != nil && s.intelligence.IsLLMBacked()
	created := 0

	for _, entityID := range entityIDs {
		count := linkCounts[entityID]
		if count < minMemoriesForEntityBrief {
			continue
		}

		entity := entityByID[entityID]
		memories, err := s.repo.ListMemoriesByEntityID(ctx, entityID, entityBriefMemoryFetchLimit)
		if err != nil {
			slog.Warn("failed to list memories for entity brief", "entity_id", entityID, "error", err)
			continue
		}
		if len(memories) < minMemoriesForEntityBrief {
			continue
		}

		// Incremental check: skip if the existing brief is up to date.
		existing := briefByEntityID[entityID]
		if existing != nil && !entityBriefNeedsUpdate(existing, memories, isLLMBacked) {
			continue
		}

		// Determine extraction method before generating.
		method := MethodHeuristic
		if isLLMBacked {
			method = MethodLLM
		}

		// Build the input for the LLM.
		input := buildEntityBriefInput(entity, memories)

		// Generate the brief body via escalation (LLM → aggressive → truncate).
		body, err := s.escalate(ctx, input, entityBriefBodyMaxChars)
		if err != nil {
			slog.Warn("failed to generate entity brief", "entity_id", entityID, "error", err)
			continue
		}
		if strings.TrimSpace(body) == "" {
			continue
		}

		// Detect if escalation fell back to truncation (heuristic).
		if isLLMBacked && len(body) >= entityBriefBodyMaxChars-10 && strings.HasSuffix(body, "]") {
			// Likely a "[Truncated from N chars]" deterministic fallback.
			method = MethodHeuristic
		}

		tightDesc := fmt.Sprintf("Entity brief: %s", entity.CanonicalName)
		if s.intelligence != nil {
			if tight, err := s.intelligence.Summarize(ctx, body, 100); err == nil {
				if cleaned := strings.TrimSpace(tight); cleaned != "" && len(cleaned) <= 120 {
					tightDesc = cleaned
				}
			}
		}

		scope, projectID := inferScopeFromMemories(memories)
		now := time.Now().UTC()

		// Delete old brief if it exists.
		if existing != nil {
			if err := s.repo.DeleteSummary(ctx, existing.ID); err != nil {
				slog.Warn("failed to delete old entity brief", "entity_id", entityID, "error", err)
				continue
			}
		}

		priorFallbackCount := 0
		if existing != nil {
			priorFallbackCount = entityBriefFallbackCount(existing)
		}
		retryable := method == MethodHeuristic && isLLMBacked

		summary := &core.Summary{
			ID:               core.GenerateID("sum_"),
			Kind:             entityBriefSummaryKind,
			Depth:            0,
			Scope:            scope,
			ProjectID:        projectID,
			Title:            fmt.Sprintf("Brief: %s", entity.CanonicalName),
			Body:             body,
			TightDescription: tightDesc,
			PrivacyLevel:     core.PrivacyPrivate,
			Metadata: map[string]string{
				entityBriefMetaEntityID:   entityID,
				entityBriefMetaEntityName: entity.CanonicalName,
				entityBriefMetaMemoryCount: fmt.Sprintf("%d", len(memories)),
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		summary.Metadata = applyExtractionMetadata(
			summary.Metadata, method, s.extractionModelName(),
			retryable, priorFallbackCount,
		)

		if err := s.repo.InsertSummary(ctx, summary); err != nil {
			slog.Warn("failed to insert entity brief", "entity_id", entityID, "error", err)
			continue
		}

		slog.Debug("entity brief created",
			"entity_id", entityID,
			"entity_name", entity.CanonicalName,
			"method", method,
			"memory_count", len(memories),
		)
		created++
	}

	return created, nil
}

// entityBriefNeedsUpdate returns true if the brief should be regenerated:
// - The newest linked memory was created after the brief was last updated, OR
// - The brief was generated by heuristic fallback and should be retried with LLM.
func entityBriefNeedsUpdate(brief *core.Summary, memories []core.Memory, isLLMBacked bool) bool {
	if brief == nil {
		return true
	}

	// Retry heuristic briefs when LLM is available.
	if isLLMBacked && entityBriefNeedsLLMRetry(brief) {
		return true
	}

	// Check if any linked memory is newer than the brief.
	for _, m := range memories {
		if m.CreatedAt.After(brief.UpdatedAt) {
			return true
		}
	}

	return false
}

// entityBriefNeedsLLMRetry returns true if the brief was produced by heuristic
// fallback and hasn't exhausted retry attempts.
func entityBriefNeedsLLMRetry(brief *core.Summary) bool {
	if brief == nil {
		return false
	}
	method := strings.TrimSpace(brief.Metadata[MetaExtractionMethod])
	if method != MethodHeuristic {
		return false
	}
	count := entityBriefFallbackCount(brief)
	return count > 0 && count < maxHeuristicFallbackRetries
}

func entityBriefFallbackCount(brief *core.Summary) int {
	if brief == nil {
		return 0
	}
	return fallbackCountFromMetadata(brief.Metadata)
}

// buildEntityBriefInput formats an entity and its linked memories into a
// synthesis prompt input.
func buildEntityBriefInput(entity core.Entity, memories []core.Memory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Entity: %s (type: %s)\n", entity.CanonicalName, entity.Type)
	if entity.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", entity.Description)
	}
	if len(entity.Aliases) > 0 {
		fmt.Fprintf(&b, "Aliases: %s\n", strings.Join(entity.Aliases, ", "))
	}

	b.WriteString("\nLinked memories:\n")
	for i, m := range memories {
		fmt.Fprintf(&b, "[%d] type=%s subject=%s\n%s\n\n",
			i+1, m.Type, m.Subject, m.Body)
	}

	b.WriteString(`
Synthesize the above into a coherent entity briefing. The briefing should answer: "What do we know about this entity?"

Include:
- Current state and role
- Key decisions involving this entity
- Relationships to other entities
- Any open questions or unresolved items

Rules:
- Be concise but comprehensive — this briefing replaces reading individual memories
- Ground everything in the provided memories; do not invent facts
- If there are contradictions, note them explicitly
- Write in present tense for current state, past tense for historical decisions`)

	return b.String()
}

// findEntityBrief looks up the entity_brief summary for a given entity ID.
func (s *AMMService) findEntityBrief(ctx context.Context, entityID string) *core.Summary {
	summaries, err := s.repo.ListSummaries(ctx, core.ListSummariesOptions{
		Kind:  entityBriefSummaryKind,
		Limit: 50,
	})
	if err != nil {
		return nil
	}
	for i := range summaries {
		if summaries[i].Metadata[entityBriefMetaEntityID] == entityID {
			return &summaries[i]
		}
	}
	return nil
}

// inferScopeFromMemories determines scope from a set of memories.
// If all share the same project, scope is project; otherwise global.
func inferScopeFromMemories(memories []core.Memory) (core.Scope, string) {
	if len(memories) == 0 {
		return core.ScopeGlobal, ""
	}
	projectID := memories[0].ProjectID
	if projectID == "" {
		return core.ScopeGlobal, ""
	}
	for _, m := range memories[1:] {
		if m.ProjectID != projectID {
			return core.ScopeGlobal, ""
		}
	}
	return core.ScopeProject, projectID
}
