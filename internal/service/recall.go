package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	minRecallScore            = 0.2
	minRecallMemoryConfidence = 0.35
	minHybridHistoryScore     = 0.48
	embeddingOnlyFTSPosition  = 999
	defaultEntityHubThreshold = 10
	entityHubDampeningFloor   = 0.05
	recallDedupThreshold      = 0.85
)

type recallFilterOptions struct {
	minScore            float64
	minMemoryConfidence float64
	allowHistoryNodes   bool
	minHistoryScore     float64
	suppressToolResults bool
}

// Recall retrieves items for query using the requested recall mode and the
// package scoring pipeline.
func (s *AMMService) Recall(ctx context.Context, query string, opts core.RecallOptions) (*core.RecallResult, error) {
	slog.Debug("Recall called", "mode", opts.Mode, "limit", opts.Limit, "project_id", opts.ProjectID, "session_id", opts.SessionID, "query_len", len(query), "after", opts.After, "before", opts.Before)
	start := time.Now()

	originalMode := opts.Mode
	if opts.Mode == "" {
		opts.Mode = core.RecallModeHybrid
		originalMode = core.RecallModeHybrid
	}

	if opts.Limit == 0 {
		switch opts.Mode {
		case core.RecallModeAmbient:
			opts.Limit = 5
		default:
			opts.Limit = 10
		}
	}

	// Temporal extraction: parse temporal references from query text
	// unless explicit After/Before flags are already set.
	ftsQuery := query
	var temporalAfter, temporalBefore *time.Time
	if opts.After != "" {
		if t, err := time.Parse(time.RFC3339, opts.After); err == nil {
			temporalAfter = &t
		}
	}
	if opts.Before != "" {
		if t, err := time.Parse(time.RFC3339, opts.Before); err == nil {
			temporalBefore = &t
		}
	}
	if temporalAfter == nil && temporalBefore == nil {
		ext := ExtractTemporal(query, time.Now().UTC())
		if ext.Range != nil {
			temporalAfter = &ext.Range.After
			temporalBefore = &ext.Range.Before
			ftsQuery = ext.StrippedQuery
			slog.Debug("temporal extraction", "after", ext.Range.After, "before", ext.Range.Before, "stripped_query", ftsQuery)
		}
	}
	// Backfill opts.After/Before from extraction so modes that read opts
	// directly (timeline, active) also get temporal bounds.
	if temporalAfter != nil && opts.After == "" {
		opts.After = temporalAfter.Format(time.RFC3339)
	}
	if temporalBefore != nil && opts.Before == "" {
		opts.Before = temporalBefore.Format(time.RFC3339)
	}

	// Build scoring context.
	sctx := s.buildScoringContext(ctx, query, opts)
	sctx.TemporalAfter = temporalAfter
	sctx.TemporalBefore = temporalBefore
	sctx.TemporalAttenuation = s.getTemporalAttenuation()
	if ftsQuery != query {
		sctx.Query = ftsQuery
	}

	// Use the stripped query for all FTS/dispatch calls so temporal phrases
	// like "yesterday" don't pollute text search.
	searchQuery := ftsQuery

	// When temporal extraction consumed the entire query (e.g., "yesterday",
	// "last week"), FTS-based modes would short-circuit on blank input. Route
	// to timeline mode which lists events by date without needing a text query.
	// Sessions mode already handles empty queries natively; timeline and active
	// don't use FTS so they're fine too.
	ftsDependentModes := map[core.RecallMode]bool{
		core.RecallModeHybrid:         true,
		core.RecallModeHistory:        true,
		core.RecallModeFacts:          true,
		core.RecallModeAmbient:        true,
		core.RecallModeEpisodes:       true,
		core.RecallModeProject:        true,
		core.RecallModeEntity:         true,
		core.RecallModeContradictions: true,
	}
	if strings.TrimSpace(searchQuery) == "" && (temporalAfter != nil || temporalBefore != nil) && ftsDependentModes[opts.Mode] {
		slog.Debug("temporal-only query, routing to timeline", "original_mode", opts.Mode, "after", temporalAfter, "before", temporalBefore)
		opts.Mode = core.RecallModeTimeline
	}

	// Auto-route hybrid queries to specialized modes when intent is clear.
	// The specialized mode runs first; if results are sparse, hybrid backfills
	// so that summaries, episodes, and events are never lost.
	var routedMode core.RecallMode
	if opts.Mode == core.RecallModeHybrid {
		if routed, ok := classifyRecallIntent(searchQuery, sctx.QueryEntities); ok {
			slog.Debug("intent routing", "from", opts.Mode, "to", routed, "query", searchQuery)
			routedMode = routed
		}
	}

	var items []core.RecallItem
	var err error

	if routedMode != "" {
		// Try the specialized mode first.
		routedOpts := opts
		routedOpts.Mode = routedMode
		items, err = s.dispatchRecall(ctx, searchQuery, routedOpts, sctx)
		if err != nil {
			return nil, err
		}
		// Backfill with hybrid if the specialized mode returned sparse results.
		if len(items) < opts.Limit {
			hybridItems, hybridErr := s.recallHybrid(ctx, searchQuery, opts, sctx)
			if hybridErr == nil {
				items = mergeRecallItems(items, hybridItems, opts.Limit)
			}
		}
		opts.Mode = routedMode
	} else {
		items, err = s.dispatchRecall(ctx, searchQuery, opts, sctx)
		if err != nil {
			return nil, err
		}
	}

	// Truncate to limit.
	if len(items) > opts.Limit {
		items = items[:opts.Limit]
	}

	// Annotate results with active contradictions.
	s.annotateContradictions(ctx, items, opts.AgentID)

	// Record recall history for repetition suppression.
	if opts.SessionID != "" {
		recallRecords := make([]core.RecallRecord, 0, len(items))
		for _, item := range items {
			recallRecords = append(recallRecords, core.RecallRecord{ItemID: item.ID, ItemKind: item.Kind})
		}
		_ = s.repo.RecordRecallBatch(ctx, opts.SessionID, recallRecords)
	}

	elapsed := time.Since(start).Milliseconds()
	meta := core.RecallMeta{
		Mode:        opts.Mode,
		QueryTimeMs: elapsed,
	}
	if opts.Mode != originalMode {
		meta.RoutedFrom = originalMode
	}
	return &core.RecallResult{
		Items: items,
		Meta:  meta,
	}, nil
}

// buildScoringContext creates the scoring context for a recall operation.
func (s *AMMService) buildScoringContext(ctx context.Context, query string, opts core.RecallOptions) ScoringContext {
	queryEntities := ExtractEntities(query)
	queryEntityWeights := s.expandQueryEntities(ctx, queryEntities)
	if len(queryEntityWeights) == 0 {
		queryEntityWeights = make(map[string]float64)
		for _, entity := range queryEntities {
			addEntityTermWithWeight(queryEntityWeights, entity, 1.0)
		}
	}
	expandedQueryEntities := make([]string, 0, len(queryEntityWeights))
	for entity := range queryEntityWeights {
		expandedQueryEntities = append(expandedQueryEntities, entity)
	}
	sort.Strings(expandedQueryEntities)
	queryEmbedding := s.buildQueryEmbedding(ctx, query)

	// Build recent recalls set for repetition suppression.
	recentRecalls := make(map[string]bool)
	if opts.SessionID != "" {
		recents, err := s.repo.GetRecentRecalls(ctx, opts.SessionID, 50)
		if err == nil {
			for _, r := range recents {
				recentRecalls[r.ItemID] = true
			}
		}
	}

	weights := s.getScoringWeights()

	return ScoringContext{
		Query:              query,
		QueryEmbedding:     queryEmbedding,
		QueryEntities:      expandedQueryEntities,
		QueryEntityWeights: queryEntityWeights,
		ProjectID:          opts.ProjectID,
		SessionID:          opts.SessionID,
		RecentRecalls:      recentRecalls,
		Now:                time.Now().UTC(),
		Weights:            &weights,
	}
}

func (s *AMMService) expandQueryEntities(ctx context.Context, queryEntities []string) map[string]float64 {
	weights := make(map[string]float64)
	termEntityIDs := make(map[string]string)
	addWeightedEntityTerm := func(term string, weight float64, entityID string) {
		addEntityTermWithWeight(weights, term, weight)
		normalized := normalizeEntityTerm(term)
		if normalized == "" || entityID == "" {
			return
		}
		if _, ok := termEntityIDs[normalized]; !ok {
			termEntityIDs[normalized] = entityID
		}
	}
	for _, entity := range queryEntities {
		addEntityTermWithWeight(weights, entity, 1.0)
	}
	if len(queryEntities) == 0 {
		return weights
	}

	visitedEntityIDs := make(map[string]bool)
	for _, queryEntity := range queryEntities {
		trimmed := strings.TrimSpace(queryEntity)
		if trimmed == "" {
			continue
		}

		entities, err := s.repo.SearchEntities(ctx, trimmed, 50)
		if err != nil {
			continue
		}

		for i := range entities {
			if !entityMatchesTerm(&entities[i], trimmed) {
				continue
			}
			entity := entities[i]
			addWeightedEntityTerm(entity.CanonicalName, 1.0, entity.ID)
			for _, alias := range entity.Aliases {
				addWeightedEntityTerm(alias, 1.0, entity.ID)
			}

			if visitedEntityIDs[entity.ID] {
				continue
			}
			visitedEntityIDs[entity.ID] = true

			related, err := s.listRelatedEntitiesForRecall(ctx, entity.ID)
			if err != nil {
				continue
			}
			for _, rel := range related {
				hopWeight := relationHopWeight(rel.HopDistance)
				if hopWeight <= 0 {
					continue
				}
				addWeightedEntityTerm(rel.Entity.CanonicalName, hopWeight, rel.Entity.ID)
				for _, alias := range rel.Entity.Aliases {
					addWeightedEntityTerm(alias, hopWeight, rel.Entity.ID)
				}
			}
		}
	}

	s.applyEntityHubDampening(ctx, weights, termEntityIDs)

	return weights
}

func (s *AMMService) applyEntityHubDampening(ctx context.Context, weights map[string]float64, termEntityIDs map[string]string) {
	if len(weights) == 0 {
		return
	}

	entityIDs := make(map[string]struct{})
	for term, weight := range weights {
		if weight <= 0 || term == "" {
			continue
		}

		entityID := termEntityIDs[term]
		if entityID == "" {
			entityID = s.resolveEntityIDForTerm(ctx, term)
			if entityID == "" {
				continue
			}
			termEntityIDs[term] = entityID
		}

		entityIDs[entityID] = struct{}{}
	}

	batchIDs := make([]string, 0, len(entityIDs))
	for entityID := range entityIDs {
		batchIDs = append(batchIDs, entityID)
	}

	linkCounts, err := s.repo.CountMemoryEntityLinksBatch(ctx, batchIDs)
	if err != nil {
		return
	}

	var totalMemories int64
	haveTotalMemories := false

	for term, weight := range weights {
		if weight <= 0 || term == "" {
			continue
		}

		entityID := termEntityIDs[term]
		if entityID == "" {
			entityID = s.resolveEntityIDForTerm(ctx, term)
			if entityID == "" {
				continue
			}
			termEntityIDs[term] = entityID
		}

		linkCount := linkCounts[entityID]

		if linkCount < s.entityHubThreshold {
			continue
		}

		if !haveTotalMemories {
			count, err := s.repo.CountActiveMemories(ctx)
			if err != nil {
				continue
			}
			totalMemories = count
			haveTotalMemories = true
		}

		dampening := entityHubDampening(totalMemories, linkCount, s.entityHubThreshold)
		weights[term] = weight * dampening
	}
}

func (s *AMMService) resolveEntityIDForTerm(ctx context.Context, term string) string {
	entities, err := s.repo.SearchEntities(ctx, term, 50)
	if err != nil {
		return ""
	}
	for i := range entities {
		if entityMatchesTerm(&entities[i], term) {
			return entities[i].ID
		}
	}
	return ""
}

func entityHubDampening(totalMemories, linkCount, hubThreshold int64) float64 {
	if linkCount < hubThreshold || totalMemories <= 1 {
		return 1.0
	}

	numerator := math.Log(float64(totalMemories) / float64(1+linkCount))
	denominator := math.Log(float64(totalMemories))
	if denominator <= 0 || math.IsNaN(numerator) || math.IsNaN(denominator) || math.IsInf(numerator, 0) || math.IsInf(denominator, 0) {
		return 1.0
	}

	dampening := numerator / denominator
	if dampening < entityHubDampeningFloor {
		dampening = entityHubDampeningFloor
	}
	if dampening > 1.0 {
		dampening = 1.0
	}
	return dampening
}

func (s *AMMService) listRelatedEntitiesForRecall(ctx context.Context, entityID string) ([]core.RelatedEntity, error) {
	projected, err := s.repo.ListProjectedRelatedEntities(ctx, entityID)
	if err == nil && len(projected) > 0 {
		entityIDs := make([]string, 0, len(projected))
		for _, projection := range projected {
			entityIDs = append(entityIDs, projection.RelatedEntityID)
		}
		entities, getErr := s.repo.GetEntitiesByIDs(ctx, entityIDs)
		if getErr == nil {
			entityByID := make(map[string]core.Entity, len(entities))
			for _, entity := range entities {
				entityByID[entity.ID] = entity
			}

			related := make([]core.RelatedEntity, 0, len(projected))
			for _, projection := range projected {
				entity, ok := entityByID[projection.RelatedEntityID]
				if !ok {
					continue
				}
				related = append(related, core.RelatedEntity{
					Entity:       entity,
					HopDistance:  projection.HopDistance,
					Relationship: projection.RelationshipPath,
				})
			}
			if len(related) > 0 {
				return related, nil
			}
		}

		related := make([]core.RelatedEntity, 0, len(projected))
		for _, projection := range projected {
			entity, getErr := s.repo.GetEntity(ctx, projection.RelatedEntityID)
			if getErr != nil {
				continue
			}
			related = append(related, core.RelatedEntity{
				Entity:       *entity,
				HopDistance:  projection.HopDistance,
				Relationship: projection.RelationshipPath,
			})
		}
		if len(related) > 0 {
			return related, nil
		}
	}

	return s.repo.ListRelatedEntities(ctx, entityID, 2)
}

func addEntityTermWithWeight(weights map[string]float64, term string, weight float64) {
	if len(weights) == 0 || weight <= 0 {
		if weight <= 0 {
			return
		}
	}
	normalized := normalizeEntityTerm(term)
	if normalized == "" {
		return
	}
	if existing, ok := weights[normalized]; ok && existing >= weight {
		return
	}
	weights[normalized] = weight
}

func relationHopWeight(hopDistance int) float64 {
	switch hopDistance {
	case 1:
		return 0.7
	case 2:
		return 0.4
	default:
		return 0.0
	}
}

// recallAmbient searches memories, summaries, and episodes with full scoring.
func (s *AMMService) recallAmbient(ctx context.Context, query string, opts core.RecallOptions, sctx ScoringContext) ([]core.RecallItem, error) {
	limit := opts.Limit
	var candidates []ScoringCandidate
	candidateIDs := make(map[string]bool)

	memories, err := s.repo.SearchMemories(ctx, query, core.ListMemoriesOptions{AgentID: opts.AgentID, Limit: limit * 2})
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	for i, m := range memories {
		if !isRecallMemoryStatusAllowed(m.Status) {
			continue
		}
		candidates = append(candidates, MemoryToCandidate(m, i))
		candidateIDs[m.ID] = true
	}

	if len(sctx.QueryEmbedding) > 0 {
		embIDs := s.searchByEmbedding(ctx, sctx.QueryEmbedding, "memory", limit*2)
		for _, id := range embIDs {
			if candidateIDs[id] {
				continue
			}
			mem, err := s.repo.GetMemory(ctx, id)
			if err != nil || mem == nil || !isRecallMemoryStatusAllowed(mem.Status) || !memoryVisibleToAgent(mem, opts.AgentID) {
				continue
			}
			c := MemoryToCandidate(*mem, embeddingOnlyFTSPosition)
			candidates = append(candidates, c)
			candidateIDs[id] = true
		}
	}

	summaries, err := s.repo.SearchSummaries(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search summaries: %w", err)
	}
	for i, sm := range summaries {
		candidates = append(candidates, SummaryToCandidate(sm, i))
		candidateIDs[sm.ID] = true
	}

	if len(sctx.QueryEmbedding) > 0 {
		embIDs := s.searchByEmbedding(ctx, sctx.QueryEmbedding, "summary", limit)
		for _, id := range embIDs {
			if candidateIDs[id] {
				continue
			}
			sm, err := s.repo.GetSummary(ctx, id)
			if err != nil || sm == nil {
				continue
			}
			c := SummaryToCandidate(*sm, embeddingOnlyFTSPosition)
			candidates = append(candidates, c)
			candidateIDs[id] = true
		}
	}

	episodes, err := s.repo.SearchEpisodes(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search episodes: %w", err)
	}
	for i, ep := range episodes {
		candidates = append(candidates, EpisodeToCandidate(ep, i))
		candidateIDs[ep.ID] = true
	}

	if len(sctx.QueryEmbedding) > 0 {
		embIDs := s.searchByEmbedding(ctx, sctx.QueryEmbedding, "episode", limit)
		for _, id := range embIDs {
			if candidateIDs[id] {
				continue
			}
			ep, err := s.repo.GetEpisode(ctx, id)
			if err != nil || ep == nil {
				continue
			}
			c := EpisodeToCandidate(*ep, embeddingOnlyFTSPosition)
			candidates = append(candidates, c)
			candidateIDs[id] = true
		}
	}

	s.attachCandidateEmbeddings(ctx, candidates)
	s.attachCandidateEntities(ctx, candidates)

	return scoreAndConvert(candidates, sctx, defaultRecallFilterOptions(), opts.Explain), nil
}

// recallFacts searches only memories with full scoring.
func (s *AMMService) recallFacts(ctx context.Context, query string, opts core.RecallOptions, sctx ScoringContext) ([]core.RecallItem, error) {
	memories, err := s.repo.SearchMemories(ctx, query, core.ListMemoriesOptions{AgentID: opts.AgentID, Limit: opts.Limit * 2})
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	var candidates []ScoringCandidate
	candidateIDs := make(map[string]bool)
	for i, m := range memories {
		if !isRecallMemoryStatusAllowed(m.Status) {
			continue
		}
		candidates = append(candidates, MemoryToCandidate(m, i))
		candidateIDs[m.ID] = true
	}
	if len(sctx.QueryEmbedding) > 0 {
		embIDs := s.searchByEmbedding(ctx, sctx.QueryEmbedding, "memory", opts.Limit*2)
		for _, id := range embIDs {
			if candidateIDs[id] {
				continue
			}
			mem, err := s.repo.GetMemory(ctx, id)
			if err != nil || mem == nil || !isRecallMemoryStatusAllowed(mem.Status) || !memoryVisibleToAgent(mem, opts.AgentID) {
				continue
			}
			c := MemoryToCandidate(*mem, embeddingOnlyFTSPosition)
			candidates = append(candidates, c)
			candidateIDs[id] = true
		}
	}
	s.attachCandidateEmbeddings(ctx, candidates)
	s.attachCandidateEntities(ctx, candidates)
	return scoreAndConvert(candidates, sctx, defaultRecallFilterOptions(), opts.Explain), nil
}

func (s *AMMService) recallContradictions(ctx context.Context, query string, opts core.RecallOptions, sctx ScoringContext) ([]core.RecallItem, error) {
	memories, err := s.repo.SearchMemories(ctx, query, core.ListMemoriesOptions{AgentID: opts.AgentID, Limit: opts.Limit * 3})
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}

	var candidates []ScoringCandidate
	candidateIDs := make(map[string]bool)
	for i, memory := range memories {
		if memory.Type != core.MemoryTypeContradiction || !isRecallMemoryStatusAllowed(memory.Status) {
			continue
		}
		candidates = append(candidates, MemoryToCandidate(memory, i))
		candidateIDs[memory.ID] = true
	}

	if len(sctx.QueryEmbedding) > 0 {
		embIDs := s.searchByEmbedding(ctx, sctx.QueryEmbedding, "memory", opts.Limit*3)
		for _, id := range embIDs {
			if candidateIDs[id] {
				continue
			}
			memory, err := s.repo.GetMemory(ctx, id)
			if err != nil || memory == nil || memory.Type != core.MemoryTypeContradiction || !isRecallMemoryStatusAllowed(memory.Status) || !memoryVisibleToAgent(memory, opts.AgentID) {
				continue
			}
			candidates = append(candidates, MemoryToCandidate(*memory, embeddingOnlyFTSPosition))
			candidateIDs[id] = true
		}
	}

	slog.Debug("recall contradictions",
		"query", query,
		"candidate_count", len(candidates),
		"limit", opts.Limit,
	)

	s.attachCandidateEmbeddings(ctx, candidates)
	s.attachCandidateEntities(ctx, candidates)
	return scoreAndConvert(candidates, sctx, defaultRecallFilterOptions(), opts.Explain), nil
}

// recallEpisodes searches only episodes with full scoring.
func (s *AMMService) recallEpisodes(ctx context.Context, query string, opts core.RecallOptions, sctx ScoringContext) ([]core.RecallItem, error) {
	episodes, err := s.repo.SearchEpisodes(ctx, query, opts.Limit*2)
	if err != nil {
		return nil, fmt.Errorf("search episodes: %w", err)
	}
	var candidates []ScoringCandidate
	for i, ep := range episodes {
		candidates = append(candidates, EpisodeToCandidate(ep, i))
	}
	s.attachCandidateEmbeddings(ctx, candidates)
	s.attachCandidateEntities(ctx, candidates)
	return scoreAndConvert(candidates, sctx, defaultRecallFilterOptions(), opts.Explain), nil
}

// recallProject searches memories filtered by project_id with full scoring.
func (s *AMMService) recallProject(ctx context.Context, query string, opts core.RecallOptions, sctx ScoringContext) ([]core.RecallItem, error) {
	memories, err := s.repo.SearchMemories(ctx, query, core.ListMemoriesOptions{AgentID: opts.AgentID, Limit: opts.Limit * 3})
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	var candidates []ScoringCandidate
	candidateIDs := make(map[string]bool)
	for i, mem := range memories {
		if opts.ProjectID != "" && mem.ProjectID != opts.ProjectID {
			continue
		}
		if !isRecallMemoryStatusAllowed(mem.Status) {
			continue
		}
		candidates = append(candidates, MemoryToCandidate(mem, i))
		candidateIDs[mem.ID] = true
	}
	if len(sctx.QueryEmbedding) > 0 {
		embIDs := s.searchByEmbedding(ctx, sctx.QueryEmbedding, "memory", opts.Limit*3)
		for _, id := range embIDs {
			if candidateIDs[id] {
				continue
			}
			mem, err := s.repo.GetMemory(ctx, id)
			if err != nil || mem == nil {
				continue
			}
			if !memoryVisibleToAgent(mem, opts.AgentID) {
				continue
			}
			if opts.ProjectID != "" && mem.ProjectID != opts.ProjectID {
				continue
			}
			if !isRecallMemoryStatusAllowed(mem.Status) {
				continue
			}
			c := MemoryToCandidate(*mem, embeddingOnlyFTSPosition)
			candidates = append(candidates, c)
			candidateIDs[id] = true
		}
	}
	s.attachCandidateEmbeddings(ctx, candidates)
	s.attachCandidateEntities(ctx, candidates)
	return scoreAndConvert(candidates, sctx, defaultRecallFilterOptions(), opts.Explain), nil
}

// recallEntity searches memories and entities with full scoring.
func (s *AMMService) recallEntity(ctx context.Context, query string, opts core.RecallOptions, sctx ScoringContext) ([]core.RecallItem, error) {
	var candidates []ScoringCandidate
	candidateIDs := make(map[string]bool)

	memories, err := s.repo.SearchMemories(ctx, query, core.ListMemoriesOptions{AgentID: opts.AgentID, Limit: opts.Limit * 2})
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	for i, m := range memories {
		if !isRecallMemoryStatusAllowed(m.Status) {
			continue
		}
		candidates = append(candidates, MemoryToCandidate(m, i))
		candidateIDs[m.ID] = true
	}
	if len(sctx.QueryEmbedding) > 0 {
		embIDs := s.searchByEmbedding(ctx, sctx.QueryEmbedding, "memory", opts.Limit*2)
		for _, id := range embIDs {
			if candidateIDs[id] {
				continue
			}
			mem, err := s.repo.GetMemory(ctx, id)
			if err != nil || mem == nil || !isRecallMemoryStatusAllowed(mem.Status) || !memoryVisibleToAgent(mem, opts.AgentID) {
				continue
			}
			c := MemoryToCandidate(*mem, embeddingOnlyFTSPosition)
			candidates = append(candidates, c)
			candidateIDs[id] = true
		}
	}
	s.attachCandidateEmbeddings(ctx, candidates)
	s.attachCandidateEntities(ctx, candidates)

	items := scoreAndConvert(candidates, sctx, defaultRecallFilterOptions(), opts.Explain)
	entities, err := s.repo.SearchEntities(ctx, query, opts.Limit)
	if err != nil {
		return nil, fmt.Errorf("search entities: %w", err)
	}
	entityItems := make([]core.RecallItem, 0, len(entities))
	for i, ent := range entities {
		item := core.RecallItem{
			ID:               ent.ID,
			Kind:             "entity",
			Type:             ent.Type,
			Score:            positionScore(i),
			TightDescription: ent.Description,
		}
		// Attach entity brief body if one exists, giving richer context.
		if brief := s.findEntityBrief(ctx, ent.ID); brief != nil {
			item.TightDescription = brief.TightDescription
		}
		entityItems = append(entityItems, item)
	}

	items = append(items, entityItems...)
	sort.Slice(items, func(i, j int) bool {
		return items[i].Score > items[j].Score
	})

	return items, nil
}

// recallHistory searches events with full scoring.
func (s *AMMService) recallHistory(ctx context.Context, query string, opts core.RecallOptions, sctx ScoringContext) ([]core.RecallItem, error) {
	fetchMult := 2
	if sctx.TemporalAfter != nil || sctx.TemporalBefore != nil {
		fetchMult = 20 // over-fetch aggressively when temporal hard-filter will discard
	}
	events, err := s.repo.SearchEvents(ctx, query, opts.Limit*fetchMult)
	if err != nil {
		return nil, fmt.Errorf("search events: %w", err)
	}
	// Hard-filter by temporal bounds when set.
	if sctx.TemporalAfter != nil || sctx.TemporalBefore != nil {
		filtered := events[:0]
		for _, evt := range events {
			if sctx.TemporalAfter != nil && evt.OccurredAt.Before(*sctx.TemporalAfter) {
				continue
			}
			if sctx.TemporalBefore != nil && evt.OccurredAt.After(*sctx.TemporalBefore) {
				continue
			}
			filtered = append(filtered, evt)
		}
		events = filtered
	}
	var candidates []ScoringCandidate
	for i, evt := range events {
		candidates = append(candidates, EventToCandidate(evt, i))
	}
	s.attachCandidateEmbeddings(ctx, candidates)
	s.attachCandidateEntities(ctx, candidates)
	return scoreAndConvert(candidates, sctx, historyRecallFilterOptions(), opts.Explain), nil
}

// recallHybrid searches all types with full scoring.
func (s *AMMService) recallHybrid(ctx context.Context, query string, opts core.RecallOptions, sctx ScoringContext) ([]core.RecallItem, error) {
	perType := opts.Limit
	// When temporal bounds are active, the scoreAndConvert hard-filter will
	// discard out-of-window candidates. Over-fetch to ensure enough in-window
	// candidates survive past the per-source FTS limit.
	fetchMult := 2
	if sctx.TemporalAfter != nil || sctx.TemporalBefore != nil {
		fetchMult = 10
	}
	var candidates []ScoringCandidate
	candidateIDs := make(map[string]bool)

	memories, err := s.repo.SearchMemories(ctx, query, core.ListMemoriesOptions{AgentID: opts.AgentID, Limit: perType * fetchMult})
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	for i, m := range memories {
		if !isRecallMemoryStatusAllowed(m.Status) {
			continue
		}
		candidates = append(candidates, MemoryToCandidate(m, i))
		candidateIDs[m.ID] = true
	}
	if len(sctx.QueryEmbedding) > 0 {
		embIDs := s.searchByEmbedding(ctx, sctx.QueryEmbedding, "memory", perType*fetchMult)
		for _, id := range embIDs {
			if candidateIDs[id] {
				continue
			}
			mem, err := s.repo.GetMemory(ctx, id)
			if err != nil || mem == nil || !isRecallMemoryStatusAllowed(mem.Status) || !memoryVisibleToAgent(mem, opts.AgentID) {
				continue
			}
			c := MemoryToCandidate(*mem, embeddingOnlyFTSPosition)
			candidates = append(candidates, c)
			candidateIDs[id] = true
		}
	}

	summaries, err := s.repo.SearchSummaries(ctx, query, perType*fetchMult)
	if err != nil {
		return nil, fmt.Errorf("search summaries: %w", err)
	}
	for i, sm := range summaries {
		candidates = append(candidates, SummaryToCandidate(sm, i))
		candidateIDs[sm.ID] = true
	}
	if len(sctx.QueryEmbedding) > 0 {
		embIDs := s.searchByEmbedding(ctx, sctx.QueryEmbedding, "summary", perType*fetchMult)
		for _, id := range embIDs {
			if candidateIDs[id] {
				continue
			}
			sm, err := s.repo.GetSummary(ctx, id)
			if err != nil || sm == nil {
				continue
			}
			c := SummaryToCandidate(*sm, embeddingOnlyFTSPosition)
			candidates = append(candidates, c)
			candidateIDs[id] = true
		}
	}

	episodes, err := s.repo.SearchEpisodes(ctx, query, perType*fetchMult)
	if err != nil {
		return nil, fmt.Errorf("search episodes: %w", err)
	}
	for i, ep := range episodes {
		candidates = append(candidates, EpisodeToCandidate(ep, i))
		candidateIDs[ep.ID] = true
	}
	if len(sctx.QueryEmbedding) > 0 {
		embIDs := s.searchByEmbedding(ctx, sctx.QueryEmbedding, "episode", perType*fetchMult)
		for _, id := range embIDs {
			if candidateIDs[id] {
				continue
			}
			ep, err := s.repo.GetEpisode(ctx, id)
			if err != nil || ep == nil {
				continue
			}
			c := EpisodeToCandidate(*ep, embeddingOnlyFTSPosition)
			candidates = append(candidates, c)
			candidateIDs[id] = true
		}
	}

	events, err := s.repo.SearchEvents(ctx, query, perType*fetchMult)
	if err != nil {
		return nil, fmt.Errorf("search events: %w", err)
	}
	for i, evt := range events {
		candidates = append(candidates, EventToCandidate(evt, i))
	}

	s.attachCandidateEmbeddings(ctx, candidates)
	s.attachCandidateEntities(ctx, candidates)

	return scoreAndConvert(candidates, sctx, hybridRecallFilterOptions(), opts.Explain), nil
}

func (s *AMMService) buildQueryEmbedding(ctx context.Context, query string) []float32 {
	if s.embeddingProvider == nil || query == "" {
		return nil
	}
	vectors, err := s.embeddingProvider.Embed(ctx, []string{query})
	if err != nil || len(vectors) == 0 || len(vectors[0]) == 0 || !embeddingHasMagnitude(vectors[0]) {
		return nil
	}
	return vectors[0]
}

func (s *AMMService) searchByEmbedding(ctx context.Context, queryEmbedding []float32, objectKind string, limit int) []string {
	if s.embeddingProvider == nil || len(queryEmbedding) == 0 || limit <= 0 {
		return nil
	}
	model := s.embeddingProvider.Model()
	if model == "" {
		return nil
	}

	// Prefer ANN search via the repository (e.g. pgvecto.rs) when available.
	// Fall back to brute-force if ANN returns an error or empty results
	// (empty can mean the vector column isn't populated yet).
	ids, err := s.repo.SearchNearestEmbeddings(ctx, queryEmbedding, objectKind, model, limit)
	if err == nil && len(ids) > 0 {
		return ids
	}
	if err != nil && !errors.Is(err, core.ErrNotImplemented) {
		slog.Warn("ANN search failed, falling back to brute-force", "error", err)
	}

	// Brute-force fallback: load all embeddings and compute cosine similarity.
	records, err := s.repo.ListEmbeddingsByKind(ctx, objectKind, model, limit*5)
	if err != nil || len(records) == 0 {
		return nil
	}

	type scoredEmbedding struct {
		id    string
		score float64
	}
	scored := make([]scoredEmbedding, 0, len(records))
	for _, rec := range records {
		score, ok := cosineSimilarity(queryEmbedding, rec.Vector)
		if !ok {
			continue
		}
		scored = append(scored, scoredEmbedding{id: rec.ObjectID, score: score})
	}
	if len(scored) == 0 {
		return nil
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	ids = make([]string, 0, len(scored))
	for _, item := range scored {
		ids = append(ids, item.id)
	}
	return ids
}

func (s *AMMService) attachCandidateEmbeddings(ctx context.Context, candidates []ScoringCandidate) {
	if s.embeddingProvider == nil {
		return
	}
	model := s.embeddingProvider.Model()
	if model == "" {
		return
	}
	idsByKind := make(map[string][]string)
	seenByKind := make(map[string]map[string]struct{})
	for i := range candidates {
		objectKind := embeddingObjectKind(candidates[i].Kind)
		if _, ok := seenByKind[objectKind]; !ok {
			seenByKind[objectKind] = make(map[string]struct{})
		}
		if _, seen := seenByKind[objectKind][candidates[i].ID]; seen {
			continue
		}
		seenByKind[objectKind][candidates[i].ID] = struct{}{}
		idsByKind[objectKind] = append(idsByKind[objectKind], candidates[i].ID)
	}

	embeddingsByKind := make(map[string]map[string]core.EmbeddingRecord, len(idsByKind))
	for objectKind, ids := range idsByKind {
		records, err := s.repo.GetEmbeddingsBatch(ctx, ids, objectKind, model)
		if err != nil {
			continue
		}
		embeddingsByKind[objectKind] = records
	}

	for i := range candidates {
		objectKind := embeddingObjectKind(candidates[i].Kind)
		records := embeddingsByKind[objectKind]
		rec, ok := records[candidates[i].ID]
		if !ok || len(rec.Vector) == 0 {
			continue
		}
		candidates[i].Embedding = rec.Vector
	}
}

func (s *AMMService) attachCandidateEntities(ctx context.Context, candidates []ScoringCandidate) {
	memoryIDs := make([]string, 0)
	seen := make(map[string]struct{})
	for i := range candidates {
		if candidates[i].Kind != "memory" {
			continue
		}
		if _, ok := seen[candidates[i].ID]; ok {
			continue
		}
		seen[candidates[i].ID] = struct{}{}
		memoryIDs = append(memoryIDs, candidates[i].ID)
	}

	if len(memoryIDs) == 0 {
		return
	}

	entitiesByMemoryID, err := s.repo.GetMemoryEntitiesBatch(ctx, memoryIDs)
	if err != nil {
		return
	}

	for i := range candidates {
		if candidates[i].Kind != "memory" {
			continue
		}
		entities := entitiesByMemoryID[candidates[i].ID]
		if len(entities) == 0 {
			continue
		}
		names := make([]string, 0, len(entities))
		aliases := make([]string, 0, len(entities)*2)
		for _, entity := range entities {
			names = append(names, entity.CanonicalName)
			aliases = append(aliases, entity.Aliases...)
		}
		candidates[i].EntityNames = names
		candidates[i].EntityAliases = aliases
	}
}

func embeddingObjectKind(candidateKind string) string {
	if candidateKind == "history-node" {
		return "event"
	}
	return candidateKind
}

// scoreAndConvert scores candidates, deduplicates near-identical items, and
// converts to RecallItems sorted by score.
func scoreAndConvert(candidates []ScoringCandidate, sctx ScoringContext, opts recallFilterOptions, explain bool) []core.RecallItem {
	// Hard-filter candidates outside the temporal window before scoring.
	// Uses occurrenceTimestamp (ObservedAt/CreatedAt) not UpdatedAt, so
	// reprocessed or updated items are filtered by when they originally happened.
	if sctx.TemporalAfter != nil || sctx.TemporalBefore != nil {
		filtered := candidates[:0]
		for _, c := range candidates {
			ts := occurrenceTimestamp(c)
			if ts.IsZero() {
				filtered = append(filtered, c) // no timestamp — keep
				continue
			}
			if sctx.TemporalAfter != nil && ts.Before(*sctx.TemporalAfter) {
				continue
			}
			if sctx.TemporalBefore != nil && ts.After(*sctx.TemporalBefore) {
				continue
			}
			filtered = append(filtered, c)
		}
		candidates = filtered
	}

	type scored struct {
		candidate ScoringCandidate
		breakdown SignalBreakdown
	}
	scoredItems := make([]scored, 0, len(candidates))
	for _, c := range candidates {
		b := ScoreItem(c, sctx)
		if !shouldIncludeRecallCandidate(c, b, opts) {
			continue
		}
		scoredItems = append(scoredItems, scored{candidate: c, breakdown: b})
	}
	// Sort by score descending.
	sort.Slice(scoredItems, func(i, j int) bool {
		return scoredItems[i].breakdown.FinalScore > scoredItems[j].breakdown.FinalScore
	})

	// Deduplicate near-identical items. Walk in score order; for each item,
	// compare its embedding to already-accepted items. If cosine similarity
	// exceeds the threshold, skip the lower-scored duplicate.
	deduped := make([]scored, 0, len(scoredItems))
	accepted := make([]ScoringCandidate, 0, len(scoredItems))
	for _, si := range scoredItems {
		if isDuplicateRecallCandidate(si.candidate, accepted) {
			continue
		}
		deduped = append(deduped, si)
		accepted = append(accepted, si.candidate)
	}

	items := make([]core.RecallItem, 0, len(deduped))
	for _, si := range deduped {
		c := si.candidate
		item := core.RecallItem{
			ID:               c.ID,
			Kind:             c.Kind,
			Type:             c.Type,
			Scope:            c.Scope,
			Score:            si.breakdown.FinalScore,
			TightDescription: c.TightDescription,
		}
		if c.Confidence > 0 {
			conf := c.Confidence
			item.Confidence = &conf
		}
		if c.ObservedAt != nil {
			item.ObservedAt = c.ObservedAt.Format(time.RFC3339)
		}
		if explain {
			item.Signals = si.breakdown.ToMap()
		}
		items = append(items, item)
	}
	return items
}

// isDuplicateRecallCandidate checks if candidate is a near-duplicate of any
// already-accepted item using embedding cosine similarity (>0.85) with a
// Jaccard text fallback when embeddings are unavailable.
func isDuplicateRecallCandidate(candidate ScoringCandidate, accepted []ScoringCandidate) bool {
	for _, existing := range accepted {
		if candidate.Kind != existing.Kind {
			continue
		}
		// Try embedding-based dedup first.
		if cos, ok := cosineSimilarity(candidate.Embedding, existing.Embedding); ok {
			if cos >= recallDedupThreshold {
				return true
			}
			continue
		}
		// Fallback: Jaccard similarity on body tokens.
		if jaccardBodySimilarity(candidate.Body, existing.Body) >= recallDedupThreshold {
			return true
		}
	}
	return false
}

// jaccardBodySimilarity computes Jaccard index over whitespace-tokenized bodies.
func jaccardBodySimilarity(a, b string) float64 {
	if a == "" || b == "" {
		return 0
	}
	tokensA := strings.Fields(strings.ToLower(a))
	tokensB := strings.Fields(strings.ToLower(b))
	setA := make(map[string]bool, len(tokensA))
	for _, t := range tokensA {
		setA[t] = true
	}
	setB := make(map[string]bool, len(tokensB))
	for _, t := range tokensB {
		setB[t] = true
	}
	intersection := 0
	for t := range setA {
		if setB[t] {
			intersection++
		}
	}
	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// dispatchRecall routes to the appropriate recall implementation based on mode.
func (s *AMMService) dispatchRecall(ctx context.Context, query string, opts core.RecallOptions, sctx ScoringContext) ([]core.RecallItem, error) {
	switch opts.Mode {
	case core.RecallModeAmbient:
		return s.recallAmbient(ctx, query, opts, sctx)
	case core.RecallModeFacts:
		return s.recallFacts(ctx, query, opts, sctx)
	case core.RecallModeContradictions:
		return s.recallContradictions(ctx, query, opts, sctx)
	case core.RecallModeEpisodes:
		return s.recallEpisodes(ctx, query, opts, sctx)
	case core.RecallModeProject:
		return s.recallProject(ctx, query, opts, sctx)
	case core.RecallModeEntity:
		return s.recallEntity(ctx, query, opts, sctx)
	case core.RecallModeHistory:
		return s.recallHistory(ctx, query, opts, sctx)
	case core.RecallModeHybrid:
		return s.recallHybrid(ctx, query, opts, sctx)
	case core.RecallModeTimeline:
		return s.recallTimeline(ctx, query, opts)
	case core.RecallModeActive:
		return s.recallActive(ctx, query, opts)
	case core.RecallModeSessions:
		return s.recallSessions(ctx, query, opts, sctx)
	default:
		return nil, fmt.Errorf("%w: %q", core.ErrInvalidMode, opts.Mode)
	}
}

// recallSessions lists and searches session summaries with optional date filtering.
// When a query is provided, FTS search is used. When only date range is set,
// summaries are listed chronologically. Results are always reverse-chronological.
func (s *AMMService) recallSessions(ctx context.Context, query string, opts core.RecallOptions, sctx ScoringContext) ([]core.RecallItem, error) {
	slog.Debug("recallSessions", "query", query, "after", opts.After, "before", opts.Before, "project_id", opts.ProjectID)

	listOpts := core.ListSummariesOptions{
		Kind:      "session",
		ProjectID: opts.ProjectID,
		SessionID: opts.SessionID,
		Limit:     opts.Limit * 3, // over-fetch for scoring
	}

	// Apply temporal bounds from explicit flags or extraction.
	if sctx.TemporalAfter != nil {
		listOpts.After = sctx.TemporalAfter.Format(time.RFC3339)
	}
	if sctx.TemporalBefore != nil {
		listOpts.Before = sctx.TemporalBefore.Format(time.RFC3339)
	}

	var summaries []core.Summary
	var err error

	strippedQuery := strings.TrimSpace(sctx.Query)
	if strippedQuery != "" {
		// Scoped FTS search: kind, project, session, and date filters are
		// applied in SQL before the LIMIT, so valid sessions cannot be
		// crowded out by non-session or out-of-scope summaries.
		summaries, err = s.repo.SearchScopedSummaries(ctx, strippedQuery, listOpts)
		if err != nil {
			return nil, fmt.Errorf("search session summaries: %w", err)
		}
	} else {
		// No query text — list session summaries in date range.
		summaries, err = s.repo.ListSummaries(ctx, listOpts)
		if err != nil {
			return nil, fmt.Errorf("list session summaries: %w", err)
		}
	}

	// Convert to RecallItems with position-based scoring (reverse-chronological).
	items := make([]core.RecallItem, 0, len(summaries))
	for i, sm := range summaries {
		item := core.RecallItem{
			ID:               sm.ID,
			Kind:             "summary",
			Type:             sm.Kind,
			Scope:            sm.Scope,
			Score:            positionScore(i),
			TightDescription: sm.TightDescription,
		}
		if sm.Title != "" {
			item.TightDescription = sm.Title + " — " + sm.TightDescription
		}
		items = append(items, item)
	}

	if len(items) > opts.Limit {
		items = items[:opts.Limit]
	}
	return items, nil
}

// mergeRecallItems merges routed results with hybrid backfill, deduplicating
// by ID and preserving the routed items' priority (they appear first).
func mergeRecallItems(primary, backfill []core.RecallItem, limit int) []core.RecallItem {
	seen := make(map[string]bool, len(primary))
	for _, item := range primary {
		seen[item.ID] = true
	}
	merged := make([]core.RecallItem, len(primary), limit)
	copy(merged, primary)
	for _, item := range backfill {
		if len(merged) >= limit {
			break
		}
		if !seen[item.ID] {
			merged = append(merged, item)
			seen[item.ID] = true
		}
	}
	return merged
}

// annotateContradictions looks up active contradiction memories and annotates
// any returned items that are referenced in a contradiction with the IDs of the
// conflicting memory. Only memory-kind items are checked. The agentID parameter
// gates visibility: conflicting memory IDs are only included when the caller is
// allowed to see the referenced memory.
func (s *AMMService) annotateContradictions(ctx context.Context, items []core.RecallItem, agentID string) {
	// Collect memory IDs from the result set.
	memoryIDs := make(map[string]int) // id -> index in items
	for i, item := range items {
		if item.Kind == "memory" {
			memoryIDs[item.ID] = i
		}
	}
	if len(memoryIDs) == 0 {
		return
	}

	// Load active contradictions.
	contradictions, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Type:   core.MemoryTypeContradiction,
		Status: core.MemoryStatusActive,
		Limit:  1000,
	})
	if err != nil || len(contradictions) == 0 {
		return
	}

	// Build a map: memoryID -> set of conflicting memory IDs.
	conflicts := make(map[string]map[string]bool)
	for _, c := range contradictions {
		refIDs := extractMemoryIDsFromContradiction(c.Body)
		if len(refIDs) < 2 {
			continue
		}
		// Each referenced memory conflicts with all others in this contradiction.
		for _, id := range refIDs {
			if conflicts[id] == nil {
				conflicts[id] = make(map[string]bool)
			}
			for _, other := range refIDs {
				if other != id {
					conflicts[id][other] = true
				}
			}
		}
	}

	// Annotate items, filtering by visibility.
	for id, idx := range memoryIDs {
		if conflicting, ok := conflicts[id]; ok && len(conflicting) > 0 {
			ids := make([]string, 0, len(conflicting))
			for cid := range conflicting {
				// Only include the conflicting ID if the caller can see that memory.
				mem, err := s.repo.GetMemory(ctx, cid)
				if err != nil || mem == nil || !memoryVisibleToAgent(mem, agentID) {
					continue
				}
				ids = append(ids, cid)
			}
			sort.Strings(ids)
			items[idx].ConflictsWith = ids
		}
	}
}

// contradictionMemoryIDPattern matches the two structural "memory mem_..." refs
// in contradiction bodies. It uses a non-greedy match to skip over the Go %q
// quoted body text between the two structural positions.
var contradictionMemoryIDPattern = regexp.MustCompile(`(?:^|[:,])\s*memory\s+(mem_[^\s]+)\s+says\s`)

// extractMemoryIDsFromContradiction parses the two structural memory IDs from
// a contradiction body. The body format is:
//
//	Conflicting claims about "subject": memory mem_A says "...", memory mem_B says "..."
//
// Only the first two matches are returned to avoid picking up nested mem_ IDs
// that may appear inside the Go %q-quoted memory bodies.
func extractMemoryIDsFromContradiction(body string) []string {
	matches := contradictionMemoryIDPattern.FindAllStringSubmatch(body, 2)
	if len(matches) == 0 {
		return nil
	}
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		if len(m) >= 2 {
			ids = append(ids, m[1])
		}
	}
	return ids
}

func defaultRecallFilterOptions() recallFilterOptions {
	return recallFilterOptions{
		minScore:            minRecallScore,
		minMemoryConfidence: minRecallMemoryConfidence,
	}
}

func hybridRecallFilterOptions() recallFilterOptions {
	return recallFilterOptions{
		minScore:            minRecallScore,
		minMemoryConfidence: minRecallMemoryConfidence,
		allowHistoryNodes:   true,
		minHistoryScore:     minHybridHistoryScore,
		suppressToolResults: true,
	}
}

func historyRecallFilterOptions() recallFilterOptions {
	return recallFilterOptions{
		allowHistoryNodes: true,
	}
}

func shouldIncludeRecallCandidate(candidate ScoringCandidate, breakdown SignalBreakdown, opts recallFilterOptions) bool {
	if candidate.Kind == "history-node" {
		if !opts.allowHistoryNodes {
			return false
		}
		if opts.suppressToolResults && candidate.Type == "tool_result" {
			return false
		}
		if opts.minHistoryScore > 0 && breakdown.FinalScore < opts.minHistoryScore {
			return false
		}
	}

	if breakdown.FinalScore < opts.minScore {
		return false
	}
	if candidate.Kind == "memory" && candidate.Confidence < opts.minMemoryConfidence {
		return false
	}
	return true
}

func isRecallMemoryStatusAllowed(status core.MemoryStatus) bool {
	return status == "" || status == core.MemoryStatusActive
}

func memoryVisibleToAgent(mem *core.Memory, agentID string) bool {
	if mem == nil || agentID == "" {
		return true
	}
	return mem.AgentID == "" || mem.AgentID == agentID || mem.PrivacyLevel == core.PrivacyShared || mem.PrivacyLevel == core.PrivacyPublicSafe
}

// recallTimeline lists events ordered by occurred_at.
func (s *AMMService) recallTimeline(ctx context.Context, query string, opts core.RecallOptions) ([]core.RecallItem, error) {
	events, err := s.repo.ListEvents(ctx, core.ListEventsOptions{
		SessionID: opts.SessionID,
		ProjectID: opts.ProjectID,
		After:     opts.After,
		Before:    opts.Before,
		Limit:     opts.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	items := eventsToRecallItems(events)
	rankByPosition(items)
	return items, nil
}

// recallActive returns active-context memories, filtered by after/before when set.
func (s *AMMService) recallActive(ctx context.Context, query string, opts core.RecallOptions) ([]core.RecallItem, error) {
	fetchLimit := opts.Limit
	if opts.After != "" || opts.Before != "" {
		fetchLimit = opts.Limit * 50 // over-fetch aggressively for temporal post-filter
	}
	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Type:    core.MemoryTypeActiveContext,
		AgentID: opts.AgentID,
		Status:  core.MemoryStatusActive,
		Limit:   fetchLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list active memories: %w", err)
	}
	// Hard-filter by temporal bounds when set.
	if opts.After != "" || opts.Before != "" {
		afterT, _ := time.Parse(time.RFC3339, opts.After)
		beforeT, _ := time.Parse(time.RFC3339, opts.Before)
		filtered := memories[:0]
		for _, m := range memories {
			ts := m.CreatedAt
			if m.ObservedAt != nil {
				ts = *m.ObservedAt
			}
			if !afterT.IsZero() && ts.Before(afterT) {
				continue
			}
			if !beforeT.IsZero() && ts.After(beforeT) {
				continue
			}
			filtered = append(filtered, m)
		}
		memories = filtered
	}
	if len(memories) > opts.Limit {
		memories = memories[:opts.Limit]
	}
	items := memoriesToRecallItems(memories)
	rankByPosition(items)
	return items, nil
}

// --- conversion helpers ---

func memoryToRecallItem(m core.Memory, position int) core.RecallItem {
	item := core.RecallItem{
		ID:               m.ID,
		Kind:             "memory",
		Type:             string(m.Type),
		Scope:            m.Scope,
		Score:            positionScore(position),
		TightDescription: m.TightDescription,
		Confidence:       &m.Confidence,
	}
	if m.ObservedAt != nil {
		item.ObservedAt = m.ObservedAt.Format(time.RFC3339)
	}
	return item
}

func memoriesToRecallItems(memories []core.Memory) []core.RecallItem {
	items := make([]core.RecallItem, 0, len(memories))
	for i, m := range memories {
		items = append(items, memoryToRecallItem(m, i))
	}
	return items
}

func summariesToRecallItems(summaries []core.Summary) []core.RecallItem {
	items := make([]core.RecallItem, 0, len(summaries))
	for i, s := range summaries {
		items = append(items, core.RecallItem{
			ID:               s.ID,
			Kind:             "summary",
			Type:             s.Kind,
			Scope:            s.Scope,
			Score:            positionScore(i),
			TightDescription: s.TightDescription,
		})
	}
	return items
}

func episodesToRecallItems(episodes []core.Episode) []core.RecallItem {
	items := make([]core.RecallItem, 0, len(episodes))
	for i, e := range episodes {
		items = append(items, core.RecallItem{
			ID:               e.ID,
			Kind:             "episode",
			Scope:            e.Scope,
			Score:            positionScore(i),
			TightDescription: e.TightDescription,
		})
	}
	return items
}

func eventsToRecallItems(events []core.Event) []core.RecallItem {
	items := make([]core.RecallItem, 0, len(events))
	for i, e := range events {
		items = append(items, core.RecallItem{
			ID:               e.ID,
			Kind:             "history-node",
			Type:             e.Kind,
			Score:            positionScore(i),
			TightDescription: e.Content,
		})
	}
	return items
}

// positionScore assigns a score based on position in FTS results.
// First result gets 1.0, decaying by position.
func positionScore(position int) float64 {
	if position <= 0 {
		return 1.0
	}
	return 1.0 / (1.0 + float64(position)*0.2)
}

// rankByPosition re-assigns scores based on the current slice order.
// Items returned by FTS are already ranked; this normalizes scores
// when items from different sources are merged.
func rankByPosition(items []core.RecallItem) {
	for i := range items {
		items[i].Score = positionScore(i)
	}
}
