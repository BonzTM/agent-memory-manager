package service

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	minRecallScore            = 0.2
	minRecallMemoryConfidence = 0.35
	minHybridHistoryScore     = 0.55
	embeddingOnlyFTSPosition  = 999
	entityHubThreshold        = 10
	entityHubDampeningFloor   = 0.05
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
	start := time.Now()

	if opts.Mode == "" {
		opts.Mode = core.RecallModeHybrid
	}

	if opts.Limit == 0 {
		switch opts.Mode {
		case core.RecallModeAmbient:
			opts.Limit = 5
		default:
			opts.Limit = 10
		}
	}

	// Build scoring context.
	sctx := s.buildScoringContext(ctx, query, opts)

	var items []core.RecallItem
	var err error

	switch opts.Mode {
	case core.RecallModeAmbient:
		items, err = s.recallAmbient(ctx, query, opts, sctx)
	case core.RecallModeFacts:
		items, err = s.recallFacts(ctx, query, opts, sctx)
	case core.RecallModeEpisodes:
		items, err = s.recallEpisodes(ctx, query, opts, sctx)
	case core.RecallModeProject:
		items, err = s.recallProject(ctx, query, opts, sctx)
	case core.RecallModeEntity:
		items, err = s.recallEntity(ctx, query, opts, sctx)
	case core.RecallModeHistory:
		items, err = s.recallHistory(ctx, query, opts, sctx)
	case core.RecallModeHybrid:
		items, err = s.recallHybrid(ctx, query, opts, sctx)
	case core.RecallModeTimeline:
		items, err = s.recallTimeline(ctx, query, opts)
	case core.RecallModeActive:
		items, err = s.recallActive(ctx, query, opts)
	default:
		return nil, fmt.Errorf("%w: %q", core.ErrInvalidMode, opts.Mode)
	}
	if err != nil {
		return nil, err
	}

	// Truncate to limit.
	if len(items) > opts.Limit {
		items = items[:opts.Limit]
	}

	// Record recall history for repetition suppression.
	if opts.SessionID != "" {
		recallRecords := make([]core.RecallRecord, 0, len(items))
		for _, item := range items {
			recallRecords = append(recallRecords, core.RecallRecord{ItemID: item.ID, ItemKind: item.Kind})
		}
		_ = s.repo.RecordRecallBatch(ctx, opts.SessionID, recallRecords)
	}

	elapsed := time.Since(start).Milliseconds()
	return &core.RecallResult{
		Items: items,
		Meta: core.RecallMeta{
			Mode:        opts.Mode,
			QueryTimeMs: elapsed,
		},
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

		for _, entity := range entities {
			if !entityMatchesTerm(entity, trimmed) {
				continue
			}
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

		if linkCount < entityHubThreshold {
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

		dampening := entityHubDampening(totalMemories, linkCount)
		weights[term] = weight * dampening
	}
}

func (s *AMMService) resolveEntityIDForTerm(ctx context.Context, term string) string {
	entities, err := s.repo.SearchEntities(ctx, term, 50)
	if err != nil {
		return ""
	}
	for _, entity := range entities {
		if entityMatchesTerm(entity, term) {
			return entity.ID
		}
	}
	return ""
}

func entityHubDampening(totalMemories, linkCount int64) float64 {
	if linkCount < entityHubThreshold || totalMemories <= 1 {
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
		entityItems = append(entityItems, core.RecallItem{
			ID:               ent.ID,
			Kind:             "entity",
			Type:             ent.Type,
			Score:            positionScore(i),
			TightDescription: ent.Description,
		})
	}

	items = append(items, entityItems...)
	sort.Slice(items, func(i, j int) bool {
		return items[i].Score > items[j].Score
	})

	return items, nil
}

// recallHistory searches events with full scoring.
func (s *AMMService) recallHistory(ctx context.Context, query string, opts core.RecallOptions, sctx ScoringContext) ([]core.RecallItem, error) {
	events, err := s.repo.SearchEvents(ctx, query, opts.Limit*2)
	if err != nil {
		return nil, fmt.Errorf("search events: %w", err)
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
	var candidates []ScoringCandidate
	candidateIDs := make(map[string]bool)

	memories, err := s.repo.SearchMemories(ctx, query, core.ListMemoriesOptions{AgentID: opts.AgentID, Limit: perType * 2})
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
		embIDs := s.searchByEmbedding(ctx, sctx.QueryEmbedding, "memory", perType*2)
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

	summaries, err := s.repo.SearchSummaries(ctx, query, perType)
	if err != nil {
		return nil, fmt.Errorf("search summaries: %w", err)
	}
	for i, sm := range summaries {
		candidates = append(candidates, SummaryToCandidate(sm, i))
		candidateIDs[sm.ID] = true
	}
	if len(sctx.QueryEmbedding) > 0 {
		embIDs := s.searchByEmbedding(ctx, sctx.QueryEmbedding, "summary", perType)
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

	episodes, err := s.repo.SearchEpisodes(ctx, query, perType)
	if err != nil {
		return nil, fmt.Errorf("search episodes: %w", err)
	}
	for i, ep := range episodes {
		candidates = append(candidates, EpisodeToCandidate(ep, i))
		candidateIDs[ep.ID] = true
	}
	if len(sctx.QueryEmbedding) > 0 {
		embIDs := s.searchByEmbedding(ctx, sctx.QueryEmbedding, "episode", perType)
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

	events, err := s.repo.SearchEvents(ctx, query, perType)
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
	if err != nil || len(vectors) == 0 || len(vectors[0]) == 0 {
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
	ids := make([]string, 0, len(scored))
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

// scoreAndConvert scores candidates and converts to RecallItems sorted by score.
func scoreAndConvert(candidates []ScoringCandidate, sctx ScoringContext, opts recallFilterOptions, explain bool) []core.RecallItem {
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
	items := make([]core.RecallItem, 0, len(scoredItems))
	for _, si := range scoredItems {
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
		Limit:     opts.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	items := eventsToRecallItems(events)
	rankByPosition(items)
	return items, nil
}

// recallActive returns active-context memories.
func (s *AMMService) recallActive(ctx context.Context, query string, opts core.RecallOptions) ([]core.RecallItem, error) {
	memories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Type:    core.MemoryTypeActiveContext,
		AgentID: opts.AgentID,
		Status:  core.MemoryStatusActive,
		Limit:   opts.Limit,
	})
	if err != nil {
		return nil, fmt.Errorf("list active memories: %w", err)
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
