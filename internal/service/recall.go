package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	minRecallScore            = 0.2
	minRecallMemoryConfidence = 0.35
	minHybridHistoryScore     = 0.55
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
		for _, item := range items {
			_ = s.repo.RecordRecall(ctx, opts.SessionID, item.ID, item.Kind)
		}
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

	return ScoringContext{
		Query:          query,
		QueryEmbedding: queryEmbedding,
		QueryEntities:  queryEntities,
		ProjectID:      opts.ProjectID,
		SessionID:      opts.SessionID,
		RecentRecalls:  recentRecalls,
		Now:            time.Now().UTC(),
	}
}

// recallAmbient searches memories, summaries, and episodes with full scoring.
func (s *AMMService) recallAmbient(ctx context.Context, query string, opts core.RecallOptions, sctx ScoringContext) ([]core.RecallItem, error) {
	limit := opts.Limit
	var candidates []ScoringCandidate

	memories, err := s.repo.SearchMemories(ctx, query, limit*2)
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	for i, m := range memories {
		if !isRecallMemoryStatusAllowed(m.Status) {
			continue
		}
		candidates = append(candidates, MemoryToCandidate(m, i))
	}

	summaries, err := s.repo.SearchSummaries(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search summaries: %w", err)
	}
	for i, sm := range summaries {
		candidates = append(candidates, SummaryToCandidate(sm, i))
	}

	episodes, err := s.repo.SearchEpisodes(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search episodes: %w", err)
	}
	for i, ep := range episodes {
		candidates = append(candidates, EpisodeToCandidate(ep, i))
	}

	s.attachCandidateEmbeddings(ctx, candidates)

	return scoreAndConvert(candidates, sctx, defaultRecallFilterOptions()), nil
}

// recallFacts searches only memories with full scoring.
func (s *AMMService) recallFacts(ctx context.Context, query string, opts core.RecallOptions, sctx ScoringContext) ([]core.RecallItem, error) {
	memories, err := s.repo.SearchMemories(ctx, query, opts.Limit*2)
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	var candidates []ScoringCandidate
	for i, m := range memories {
		if !isRecallMemoryStatusAllowed(m.Status) {
			continue
		}
		candidates = append(candidates, MemoryToCandidate(m, i))
	}
	s.attachCandidateEmbeddings(ctx, candidates)
	return scoreAndConvert(candidates, sctx, defaultRecallFilterOptions()), nil
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
	return scoreAndConvert(candidates, sctx, defaultRecallFilterOptions()), nil
}

// recallProject searches memories filtered by project_id with full scoring.
func (s *AMMService) recallProject(ctx context.Context, query string, opts core.RecallOptions, sctx ScoringContext) ([]core.RecallItem, error) {
	memories, err := s.repo.SearchMemories(ctx, query, opts.Limit*3)
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	var candidates []ScoringCandidate
	for i, mem := range memories {
		if opts.ProjectID != "" && mem.ProjectID != opts.ProjectID {
			continue
		}
		if !isRecallMemoryStatusAllowed(mem.Status) {
			continue
		}
		candidates = append(candidates, MemoryToCandidate(mem, i))
	}
	s.attachCandidateEmbeddings(ctx, candidates)
	return scoreAndConvert(candidates, sctx, defaultRecallFilterOptions()), nil
}

// recallEntity searches memories and entities with full scoring.
func (s *AMMService) recallEntity(ctx context.Context, query string, opts core.RecallOptions, sctx ScoringContext) ([]core.RecallItem, error) {
	var candidates []ScoringCandidate

	memories, err := s.repo.SearchMemories(ctx, query, opts.Limit*2)
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	for i, m := range memories {
		if !isRecallMemoryStatusAllowed(m.Status) {
			continue
		}
		candidates = append(candidates, MemoryToCandidate(m, i))
	}
	s.attachCandidateEmbeddings(ctx, candidates)

	items := scoreAndConvert(candidates, sctx, defaultRecallFilterOptions())
	entities, err := s.repo.SearchEntities(ctx, query, opts.Limit)
	if err != nil {
		return nil, fmt.Errorf("search entities: %w", err)
	}
	for i, ent := range entities {
		items = append(items, core.RecallItem{
			ID:               ent.ID,
			Kind:             "entity",
			Type:             ent.Type,
			Score:            positionScore(i),
			TightDescription: ent.Description,
		})
	}
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
	return scoreAndConvert(candidates, sctx, historyRecallFilterOptions()), nil
}

// recallHybrid searches all types with full scoring.
func (s *AMMService) recallHybrid(ctx context.Context, query string, opts core.RecallOptions, sctx ScoringContext) ([]core.RecallItem, error) {
	perType := opts.Limit
	var candidates []ScoringCandidate

	memories, err := s.repo.SearchMemories(ctx, query, perType*2)
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}
	for i, m := range memories {
		if !isRecallMemoryStatusAllowed(m.Status) {
			continue
		}
		candidates = append(candidates, MemoryToCandidate(m, i))
	}

	summaries, err := s.repo.SearchSummaries(ctx, query, perType)
	if err != nil {
		return nil, fmt.Errorf("search summaries: %w", err)
	}
	for i, sm := range summaries {
		candidates = append(candidates, SummaryToCandidate(sm, i))
	}

	episodes, err := s.repo.SearchEpisodes(ctx, query, perType)
	if err != nil {
		return nil, fmt.Errorf("search episodes: %w", err)
	}
	for i, ep := range episodes {
		candidates = append(candidates, EpisodeToCandidate(ep, i))
	}

	events, err := s.repo.SearchEvents(ctx, query, perType)
	if err != nil {
		return nil, fmt.Errorf("search events: %w", err)
	}
	for i, evt := range events {
		candidates = append(candidates, EventToCandidate(evt, i))
	}

	s.attachCandidateEmbeddings(ctx, candidates)

	return scoreAndConvert(candidates, sctx, hybridRecallFilterOptions()), nil
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

func (s *AMMService) attachCandidateEmbeddings(ctx context.Context, candidates []ScoringCandidate) {
	if s.embeddingProvider == nil {
		return
	}
	model := s.embeddingProvider.Model()
	if model == "" {
		return
	}
	for i := range candidates {
		objectKind := embeddingObjectKind(candidates[i].Kind)
		rec, err := s.repo.GetEmbedding(ctx, candidates[i].ID, objectKind, model)
		if err != nil || rec == nil || len(rec.Vector) == 0 {
			continue
		}
		candidates[i].Embedding = rec.Vector
	}
}

func embeddingObjectKind(candidateKind string) string {
	if candidateKind == "history-node" {
		return "event"
	}
	return candidateKind
}

// scoreAndConvert scores candidates and converts to RecallItems sorted by score.
func scoreAndConvert(candidates []ScoringCandidate, sctx ScoringContext, opts recallFilterOptions) []core.RecallItem {
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
		Type:   core.MemoryTypeActiveContext,
		Status: core.MemoryStatusActive,
		Limit:  opts.Limit,
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
