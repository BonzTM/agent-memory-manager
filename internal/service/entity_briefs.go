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
	entityBriefSummaryKind     = "entity_brief"
	minMemoriesForEntityBrief  = 3
	entityBriefBodyMaxChars    = 2000
	entityBriefMetaEntityID    = "entity_id"
	entityBriefMetaEntityName  = "entity_name"
	entityBriefMaxEntities     = 100
	entityBriefMemoryFetchLimit = 20
)

// BuildEntityBriefs generates synthesis summaries for entities with enough
// linked memories. For each qualifying entity, it gathers all linked
// memories and produces a coherent briefing via LLM (or heuristic fallback).
// Existing briefs are updated if the entity has new linked memories.
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

		// Build the input for the LLM.
		input := buildEntityBriefInput(entity, memories)

		// Generate the brief body.
		body, err := s.escalate(ctx, input, entityBriefBodyMaxChars)
		if err != nil {
			slog.Warn("failed to generate entity brief", "entity_id", entityID, "error", err)
			continue
		}
		if strings.TrimSpace(body) == "" {
			continue
		}

		tightDesc := fmt.Sprintf("Entity brief: %s", entity.CanonicalName)
		if tight, err := s.intelligence.Summarize(ctx, body, 100); err == nil {
			if cleaned := strings.TrimSpace(tight); cleaned != "" && len(cleaned) <= 120 {
				tightDesc = cleaned
			}
		}

		// Determine scope from linked memories.
		scope, projectID := inferScopeFromMemories(memories)

		now := time.Now().UTC()

		if existing, ok := briefByEntityID[entityID]; ok {
			if err := s.repo.DeleteSummary(ctx, existing.ID); err != nil {
				slog.Warn("failed to delete old entity brief", "entity_id", entityID, "error", err)
				continue
			}
		}
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
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := s.repo.InsertSummary(ctx, summary); err != nil {
			slog.Warn("failed to insert entity brief", "entity_id", entityID, "error", err)
			continue
		}

		created++
	}

	return created, nil
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
