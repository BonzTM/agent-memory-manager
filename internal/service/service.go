package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

// AMMService implements core.Service by coordinating repository access,
// summarization, and maintenance workflows for durable memory operations.
type AMMService struct {
	repo                            core.Repository
	dbPath                          string
	intelligence                    core.IntelligenceProvider
	defaultIntelligence             core.IntelligenceProvider
	embeddingProvider               core.EmbeddingProvider
	reprocessBatchSize              int
	reflectBatchSize                int
	reflectLLMBatchSize             int
	lifecycleReviewBatchSize        int
	compressChunkSize               int
	compressMaxEvents               int
	compressBatchSize               int
	topicBatchSize                  int
	escalationDeterministicMaxChars int
	embeddingBatchSize              int
	crossProjectSimilarityThreshold float64
	maxExpandDepth                  int
	scoringWeights                  ScoringWeights
	scoringWeightsMu                sync.RWMutex
}

// Compile-time check that AMMService implements core.Service.
var _ core.Service = (*AMMService)(nil)

func New(repo core.Repository, dbPath string, summarizer core.Summarizer, embeddingProvider core.EmbeddingProvider) *AMMService {
	intelligence := NewSummarizerIntelligenceAdapter(summarizer)
	svc := &AMMService{
		repo:                            repo,
		dbPath:                          dbPath,
		intelligence:                    intelligence,
		defaultIntelligence:             intelligence,
		embeddingProvider:               embeddingProvider,
		reprocessBatchSize:              defaultBatchSize,
		reflectBatchSize:                defaultReflectBatchSize,
		reflectLLMBatchSize:             defaultReflectLLMBatchSize,
		lifecycleReviewBatchSize:        defaultLifecycleReviewBatchSize,
		compressChunkSize:               defaultCompressChunkSize,
		compressMaxEvents:               defaultCompressMaxEvents,
		compressBatchSize:               defaultCompressBatchSize,
		topicBatchSize:                  defaultTopicBatchSize,
		escalationDeterministicMaxChars: defaultEscalationDeterministicMaxChars,
		embeddingBatchSize:              defaultEmbeddingBatchSize,
		crossProjectSimilarityThreshold: defaultCrossProjectSimilarityThreshold,
		maxExpandDepth:                  1,
		scoringWeights:                  DefaultScoringWeights(),
	}
	if repo != nil && dbPath != "" {
		if _, err := os.Stat(dbPath); err == nil {
			if err := svc.loadScoringWeights(context.Background()); err != nil {
				slog.Warn("New service defaulting scoring weights after load failure", "error", err)
			}
		}
	}
	return svc
}

// SetReprocessBatchSize configures the batch size used by Reprocess.
// Non-positive values restore the default batch size.
func (s *AMMService) SetReprocessBatchSize(batchSize int) {
	if batchSize <= 0 {
		s.reprocessBatchSize = defaultBatchSize
		return
	}
	s.reprocessBatchSize = batchSize
}

func (s *AMMService) SetReflectBatchSize(batchSize int) {
	if batchSize <= 0 {
		s.reflectBatchSize = defaultReflectBatchSize
		return
	}
	s.reflectBatchSize = batchSize
}

func (s *AMMService) SetReflectLLMBatchSize(batchSize int) {
	if batchSize <= 0 {
		s.reflectLLMBatchSize = defaultReflectLLMBatchSize
		return
	}
	s.reflectLLMBatchSize = batchSize
}

func (s *AMMService) SetCompressChunkSize(batchSize int) {
	if batchSize <= 0 {
		s.compressChunkSize = defaultCompressChunkSize
		return
	}
	s.compressChunkSize = batchSize
}

func (s *AMMService) SetCompressMaxEvents(batchSize int) {
	if batchSize <= 0 {
		s.compressMaxEvents = defaultCompressMaxEvents
		return
	}
	s.compressMaxEvents = batchSize
}

func (s *AMMService) SetCompressBatchSize(batchSize int) {
	if batchSize <= 0 {
		s.compressBatchSize = defaultCompressBatchSize
		return
	}
	s.compressBatchSize = batchSize
}

func (s *AMMService) SetTopicBatchSize(batchSize int) {
	if batchSize <= 0 {
		s.topicBatchSize = defaultTopicBatchSize
		return
	}
	s.topicBatchSize = batchSize
}

// SetEscalationDeterministicMaxChars configures the maximum character length
// used by the Level-3 deterministic truncation fallback in summarizeWithEscalation.
// Non-positive values restore the default (2048).
func (s *AMMService) SetEscalationDeterministicMaxChars(n int) {
	if n <= 0 {
		s.escalationDeterministicMaxChars = defaultEscalationDeterministicMaxChars
		return
	}
	s.escalationDeterministicMaxChars = n
}

func (s *AMMService) SetEmbeddingBatchSize(batchSize int) {
	if batchSize <= 0 {
		s.embeddingBatchSize = defaultEmbeddingBatchSize
		return
	}
	s.embeddingBatchSize = batchSize
}

func (s *AMMService) SetCrossProjectSimilarityThreshold(threshold float64) {
	if threshold <= 0 {
		s.crossProjectSimilarityThreshold = defaultCrossProjectSimilarityThreshold
		return
	}
	s.crossProjectSimilarityThreshold = threshold
}

func (s *AMMService) SetMaxExpandDepth(depth int) {
	if depth < -1 {
		s.maxExpandDepth = 1
		return
	}
	s.maxExpandDepth = depth
}

func (s *AMMService) SetIntelligenceProvider(provider core.IntelligenceProvider) {
	if provider == nil {
		s.intelligence = s.defaultIntelligence
		return
	}
	s.intelligence = provider
}

// IngestEvent stores a raw event in history after applying ingestion policies
// and defaulting missing identifiers, timestamps, and privacy metadata.
func (s *AMMService) IngestEvent(ctx context.Context, event *core.Event) (*core.Event, error) {
	if event == nil {
		return nil, fmt.Errorf("%w: event is required", core.ErrInvalidInput)
	}

	var kind string
	var eventID string
	var sourceSystem string
	kind = event.Kind
	eventID = event.ID
	sourceSystem = event.SourceSystem
	slog.Debug("IngestEvent called", "kind", kind, "sourceSystem", sourceSystem)

	// Check ingestion policy.
	shouldIngest, createMemory, err := s.ShouldIngest(ctx, event)
	if err != nil {
		return nil, fmt.Errorf("check ingestion policy: %w", err)
	}
	if !shouldIngest {
		slog.Debug("IngestEvent completed successfully", "kind", kind, "sourceSystem", sourceSystem, "id", eventID, "ingested", false)
		return event, nil // silently skip per policy
	}

	if event.ID == "" {
		event.ID = core.GenerateID("evt_")
	}
	eventID = event.ID
	if event.PrivacyLevel == "" {
		event.PrivacyLevel = core.PrivacyPrivate
	}
	event.IngestedAt = time.Now().UTC()
	if event.OccurredAt.IsZero() {
		event.OccurredAt = event.IngestedAt
	}

	// Tag read-only events so Reflect skips them.
	if !createMemory {
		if event.Metadata == nil {
			event.Metadata = make(map[string]string)
		}
		event.Metadata["ingestion_mode"] = "read_only"
	}

	if err := s.repo.InsertEvent(ctx, event); err != nil {
		return nil, fmt.Errorf("insert event: %w", err)
	}
	slog.Debug("IngestEvent completed successfully", "kind", kind, "sourceSystem", sourceSystem, "id", eventID, "ingested", true)
	return event, nil
}

// IngestTranscript ingests a sequence of events and returns the count written
// before the first failure, if any.
func (s *AMMService) IngestTranscript(ctx context.Context, events []*core.Event) (int, error) {
	slog.Debug("IngestTranscript called", "eventCount", len(events))

	ingested := 0
	for _, evt := range events {
		if _, err := s.IngestEvent(ctx, evt); err != nil {
			return ingested, fmt.Errorf("ingest event %d: %w", ingested, err)
		}
		ingested++
	}
	slog.Debug("IngestTranscript completed successfully", "eventCount", len(events), "ingested", ingested)
	return ingested, nil
}

// Remember persists an explicit durable memory and updates any superseded
// predecessor referenced by the new memory.
func (s *AMMService) Remember(ctx context.Context, memory *core.Memory) (*core.Memory, error) {
	if memory == nil {
		return nil, fmt.Errorf("%w: memory is required", core.ErrInvalidInput)
	}

	var memoryType core.MemoryType
	var memoryID string
	memoryType = memory.Type
	memoryID = memory.ID
	slog.Debug("Remember called", "type", memoryType, "id", memoryID)

	now := time.Now().UTC()
	if memory.ID == "" {
		memory.ID = core.GenerateID("mem_")
	}
	if memory.CreatedAt.IsZero() {
		memory.CreatedAt = now
	}
	if memory.UpdatedAt.IsZero() {
		memory.UpdatedAt = now
	}
	if memory.Status == "" {
		memory.Status = core.MemoryStatusActive
	}
	if memory.PrivacyLevel == "" {
		memory.PrivacyLevel = core.PrivacyPrivate
	}
	if memory.Confidence == 0 {
		memory.Confidence = 0.8
	}
	if memory.Importance == 0 {
		memory.Importance = 0.5
	}
	if memory.Scope == "" {
		memory.Scope = core.ScopeGlobal
	}
	if memory.Metadata == nil {
		memory.Metadata = make(map[string]string)
	}
	setProcessingMeta(memory, MetaExtractionQuality, QualityVerified)

	if memory.Supersedes == "" {
		activeMemories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
			Type:      memory.Type,
			Scope:     memory.Scope,
			ProjectID: memory.ProjectID,
			Status:    core.MemoryStatusActive,
			Limit:     200,
		})
		if err != nil {
			return nil, fmt.Errorf("list active memories for dedup: %w", err)
		}

		activeMemoryPtrs := make([]*core.Memory, 0, len(activeMemories))
		for i := range activeMemories {
			activeMemoryPtrs = append(activeMemoryPtrs, &activeMemories[i])
		}

		duplicates := findDuplicateActiveMemories(activeMemoryPtrs, *memory)
		embeddingDuplicateMatch := false
		if len(duplicates) == 0 {
			duplicates = s.findDuplicatesByEmbedding(ctx, *memory, activeMemoryPtrs)
			embeddingDuplicateMatch = len(duplicates) > 0
		}
		if len(duplicates) > 0 {
			keeper := selectDuplicateKeeper(duplicates)
			if keeper != nil {
				bodySimilarity := jaccardSimilarity(normalizeMemoryText(keeper.Body), normalizeMemoryText(memory.Body))
				if embeddingDuplicateMatch || bodySimilarity >= 0.85 {
					keeper.SourceEventIDs = mergeUniqueStrings(keeper.SourceEventIDs, memory.SourceEventIDs)
					if keeper.Metadata == nil {
						keeper.Metadata = make(map[string]string)
					}
					setProcessingMeta(keeper, MetaExtractionQuality, QualityVerified)
					if memory.Confidence > keeper.Confidence {
						keeper.Confidence = memory.Confidence
					}
					if memory.Importance > keeper.Importance {
						keeper.Importance = memory.Importance
					}
					keeper.UpdatedAt = now
					if err := s.repo.UpdateMemory(ctx, keeper); err != nil {
						return nil, fmt.Errorf("update merged memory: %w", err)
					}
					slog.Debug("Remember merged into existing", "keeperID", keeper.ID, "newType", memory.Type)
					return keeper, nil
				}
			}
		}
	}

	// Handle supersession: mark the old memory as superseded.
	if memory.Supersedes != "" {
		old, err := s.repo.GetMemory(ctx, memory.Supersedes)
		if err == nil {
			old.Status = core.MemoryStatusSuperseded
			old.SupersededBy = memory.ID
			old.SupersededAt = &now
			old.UpdatedAt = now
			if err := s.repo.UpdateMemory(ctx, old); err != nil {
				slog.Warn("failed to supersede memory", "old_id", old.ID, "new_id", memory.ID, "error", err)
			}
		}
	}

	if err := s.repo.InsertMemory(ctx, memory); err != nil {
		return nil, fmt.Errorf("insert memory: %w", err)
	}
	s.upsertMemoryEmbeddingBestEffort(ctx, memory)
	slog.Debug("Remember completed successfully", "type", memory.Type, "id", memory.ID)
	return memory, nil
}

// GetMemory returns the memory with the given ID.
func (s *AMMService) GetMemory(ctx context.Context, id string) (*core.Memory, error) {
	slog.Debug("GetMemory called", "id", id)
	memory, err := s.repo.GetMemory(ctx, id)
	if err != nil {
		return nil, err
	}
	slog.Debug("GetMemory completed successfully", "id", id, "found", true)
	return memory, nil
}

func (s *AMMService) ShareMemory(ctx context.Context, id string, privacy core.PrivacyLevel) (*core.Memory, error) {
	slog.Debug("ShareMemory called", "id", id, "privacy", privacy)

	if strings.TrimSpace(id) == "" {
		err := fmt.Errorf("%w: id is required", core.ErrInvalidInput)
		return nil, err
	}

	switch privacy {
	case core.PrivacyPrivate, core.PrivacyShared, core.PrivacyPublicSafe:
	default:
		err := fmt.Errorf("%w: invalid privacy_level %q: must be one of private, shared, public_safe", core.ErrInvalidInput, privacy)
		return nil, err
	}

	memory, err := s.repo.GetMemory(ctx, id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			err = fmt.Errorf("%w: memory %q", core.ErrNotFound, id)
		}
		return nil, err
	}

	memory.PrivacyLevel = privacy
	memory.UpdatedAt = time.Now().UTC()

	if err := s.repo.UpdateMemory(ctx, memory); err != nil {
		return nil, fmt.Errorf("update memory: %w", err)
	}

	slog.Debug("ShareMemory completed successfully", "id", id, "privacy", privacy)
	return memory, nil
}

func (s *AMMService) ForgetMemory(ctx context.Context, id string) (*core.Memory, error) {
	slog.Debug("ForgetMemory called", "id", id)

	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("%w: id is required", core.ErrInvalidInput)
	}

	memory, err := s.repo.GetMemory(ctx, id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not found") {
			err = fmt.Errorf("%w: memory %q", core.ErrNotFound, id)
		}
		return nil, err
	}

	now := time.Now().UTC()
	memory.Status = core.MemoryStatusRetracted
	memory.UpdatedAt = now
	if memory.Metadata == nil {
		memory.Metadata = map[string]string{}
	}
	memory.Metadata["retracted_at"] = now.Format(time.RFC3339)
	memory.Metadata["retracted_reason"] = "user_forget"

	if err := s.repo.UpdateMemory(ctx, memory); err != nil {
		return nil, fmt.Errorf("update memory: %w", err)
	}

	slog.Info("memory forgotten", "id", id)
	return memory, nil
}

// GetSummary returns the summary with the given ID.
func (s *AMMService) GetSummary(ctx context.Context, id string) (*core.Summary, error) {
	slog.Debug("GetSummary called", "id", id)
	summary, err := s.repo.GetSummary(ctx, id)
	if err != nil {
		return nil, err
	}
	slog.Debug("GetSummary completed successfully", "id", id, "found", true)
	return summary, nil
}

// GetEpisode returns the episode with the given ID.
func (s *AMMService) GetEpisode(ctx context.Context, id string) (*core.Episode, error) {
	slog.Debug("GetEpisode called", "id", id)
	episode, err := s.repo.GetEpisode(ctx, id)
	if err != nil {
		return nil, err
	}
	slog.Debug("GetEpisode completed successfully", "id", id, "found", true)
	return episode, nil
}

// GetEntity returns the entity with the given ID.
func (s *AMMService) GetEntity(ctx context.Context, id string) (*core.Entity, error) {
	slog.Debug("GetEntity called", "id", id)
	entity, err := s.repo.GetEntity(ctx, id)
	if err != nil {
		return nil, err
	}
	slog.Debug("GetEntity completed successfully", "id", id, "found", true)
	return entity, nil
}

// UpdateMemory persists changes to an existing memory after refreshing its
// UpdatedAt timestamp.
func (s *AMMService) UpdateMemory(ctx context.Context, memory *core.Memory) (*core.Memory, error) {
	if memory == nil {
		return nil, fmt.Errorf("%w: memory is required", core.ErrInvalidInput)
	}

	var memoryID string
	memoryID = memory.ID
	slog.Debug("UpdateMemory called", "id", memoryID)

	now := time.Now().UTC()
	memory.UpdatedAt = now

	if memory.Supersedes != "" {
		old, err := s.repo.GetMemory(ctx, memory.Supersedes)
		if err == nil {
			old.Status = core.MemoryStatusSuperseded
			old.SupersededBy = memory.ID
			old.SupersededAt = &now
			old.UpdatedAt = now
			if err := s.repo.UpdateMemory(ctx, old); err != nil {
				slog.Warn("failed to supersede memory", "old_id", old.ID, "new_id", memory.ID, "error", err)
			}
		}
	}

	if err := s.repo.UpdateMemory(ctx, memory); err != nil {
		return nil, fmt.Errorf("update memory: %w", err)
	}
	s.upsertMemoryEmbeddingBestEffort(ctx, memory)
	slog.Debug("UpdateMemory completed successfully", "id", memory.ID)
	return memory, nil
}

// Describe returns thin descriptions for the supplied IDs by probing memories,
// then summaries, then episodes.
func (s *AMMService) Describe(ctx context.Context, ids []string) ([]core.DescribeResult, error) {
	slog.Debug("Describe called", "idCount", len(ids))

	results := make([]core.DescribeResult, 0, len(ids))
	for _, id := range ids {
		// Try memory first.
		if mem, err := s.repo.GetMemory(ctx, id); err == nil {
			results = append(results, core.DescribeResult{
				ID:               mem.ID,
				Kind:             "memory",
				Type:             string(mem.Type),
				Scope:            mem.Scope,
				TightDescription: mem.TightDescription,
				Status:           mem.Status,
				CreatedAt:        mem.CreatedAt,
			})
			continue
		}
		// Try summary.
		if sum, err := s.repo.GetSummary(ctx, id); err == nil {
			results = append(results, core.DescribeResult{
				ID:               sum.ID,
				Kind:             "summary",
				Type:             sum.Kind,
				Scope:            sum.Scope,
				TightDescription: sum.TightDescription,
				CreatedAt:        sum.CreatedAt,
			})
			continue
		}
		// Try episode.
		if ep, err := s.repo.GetEpisode(ctx, id); err == nil {
			results = append(results, core.DescribeResult{
				ID:               ep.ID,
				Kind:             "episode",
				Scope:            ep.Scope,
				TightDescription: ep.TightDescription,
				CreatedAt:        ep.CreatedAt,
			})
			continue
		}
		// Not found — skip silently.
	}
	slog.Debug("Describe completed successfully", "idCount", len(ids), "resultCount", len(results))
	return results, nil
}

// Expand returns the full expansion of a memory, summary, or episode,
// including linked children where available.
func (s *AMMService) Expand(ctx context.Context, id string, kind string, opts core.ExpandOptions) (*core.ExpandResult, error) {
	slog.Debug("Expand called", "id", id, "kind", kind)
	if s.maxExpandDepth >= 0 && opts.DelegationDepth > 0 && opts.DelegationDepth >= s.maxExpandDepth {
		slog.Warn("expansion recursion blocked", "delegation_depth", opts.DelegationDepth, "max_expand_depth", s.maxExpandDepth)
		return nil, core.ErrExpansionRecursionBlocked
	}

	result := &core.ExpandResult{}

	switch kind {
	case "memory":
		mem, err := s.repo.GetMemory(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get memory: %w", err)
		}
		result.Memory = mem
		claims, err := s.repo.ListClaimsByMemory(ctx, id)
		if err == nil {
			result.Claims = claims
		}

	case "summary":
		sum, err := s.repo.GetSummary(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get summary: %w", err)
		}
		result.Summary = sum

		// Traverse summary hierarchy via summary_edges.
		edges, err := s.repo.GetSummaryChildren(ctx, id)
		if err == nil {
			children := make([]core.Summary, 0, len(edges))
			events := make([]core.Event, 0)
			for _, edge := range edges {
				switch edge.ChildKind {
				case "summary":
					if child, cerr := s.repo.GetSummary(ctx, edge.ChildID); cerr == nil {
						children = append(children, *child)
					}
				case "event":
					if evt, eerr := s.repo.GetEvent(ctx, edge.ChildID); eerr == nil {
						events = append(events, *evt)
					}
				}
			}
			result.Children = children
			if len(events) > 0 {
				result.Events = events
			}
		}

		// Also include events from SourceSpan if no edges found.
		if len(result.Events) == 0 && len(sum.SourceSpan.EventIDs) > 0 {
			events := make([]core.Event, 0, len(sum.SourceSpan.EventIDs))
			for _, eid := range sum.SourceSpan.EventIDs {
				if evt, eerr := s.repo.GetEvent(ctx, eid); eerr == nil {
					events = append(events, *evt)
				}
			}
			if len(events) > 0 {
				result.Events = events
			}
		}

	case "episode":
		ep, err := s.repo.GetEpisode(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get episode: %w", err)
		}
		result.Episode = ep
		// Fetch source events if available.
		events := make([]core.Event, 0)
		for _, eid := range ep.SourceSpan.EventIDs {
			if evt, eerr := s.repo.GetEvent(ctx, eid); eerr == nil {
				events = append(events, *evt)
			}
		}
		if len(events) > 0 {
			result.Events = events
		}

	default:
		err := fmt.Errorf("%w: unknown kind %q", core.ErrInvalidInput, kind)
		return nil, err
	}

	if opts.SessionID != "" {
		if err := s.repo.InsertRelevanceFeedback(ctx, opts.SessionID, id, kind, "expanded"); err != nil {
			slog.Debug("Expand relevance feedback insert failed", "id", id, "kind", kind, "sessionID", opts.SessionID, "error", err)
		}
	}

	slog.Debug("Expand completed successfully", "id", id, "kind", kind)
	return result, nil
}

// History returns raw events filtered by session, query, or the provided
// history options.
func (s *AMMService) History(ctx context.Context, query string, opts core.HistoryOptions) ([]core.Event, error) {
	slog.Debug("History called", "query_len", len(query), "sessionID", opts.SessionID)

	if opts.Limit == 0 {
		opts.Limit = 50
	}

	var (
		events []core.Event
		err    error
	)
	if opts.SessionID != "" {
		events, err = s.repo.ListEvents(ctx, core.ListEventsOptions{
			SessionID: opts.SessionID,
			ProjectID: opts.ProjectID,
			Limit:     opts.Limit,
			Before:    opts.Before,
			After:     opts.After,
		})
	} else if query != "" {
		events, err = s.repo.SearchEvents(ctx, query, opts.Limit)
	} else {
		// Fallback: list all events with the given filters.
		events, err = s.repo.ListEvents(ctx, core.ListEventsOptions{
			ProjectID: opts.ProjectID,
			Limit:     opts.Limit,
			Before:    opts.Before,
			After:     opts.After,
		})
	}
	if err != nil {
		return nil, err
	}
	slog.Debug("History completed successfully", "query_len", len(query), "sessionID", opts.SessionID, "resultCount", len(events))
	return events, nil
}

// RunJob creates, executes, and records a maintenance job for the requested
// kind.
const defaultJobTimeout = 10 * time.Minute

func (s *AMMService) RunJob(ctx context.Context, kind string) (*core.Job, error) {
	slog.Debug("RunJob called", "kind", kind)

	now := time.Now().UTC()
	job := &core.Job{
		ID:        core.GenerateID("job_"),
		Kind:      kind,
		Status:    "running",
		StartedAt: &now,
		CreatedAt: now,
	}
	if err := s.repo.InsertJob(ctx, job); err != nil {
		return nil, fmt.Errorf("insert job: %w", err)
	}

	bookkeepingCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultJobTimeout)
		defer cancel()
	}

	var jobErr error
	switch kind {
	case "reflect":
		count, err := s.Reflect(ctx, job.ID)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "reflect", "memories_created": fmt.Sprintf("%d", count)}
		}
	case "compress_history":
		count, err := s.CompressHistory(ctx)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "compress_history", "summaries_created": fmt.Sprintf("%d", count)}
		}
	case "consolidate_sessions":
		count, err := s.ConsolidateSessions(ctx)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "consolidate_sessions", "summaries_created": fmt.Sprintf("%d", count)}
		}
	case "build_topic_summaries":
		count, err := s.BuildTopicSummaries(ctx)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "build_topic_summaries", "summaries_created": fmt.Sprintf("%d", count)}
		}
	case "rebuild_indexes":
		jobErr = s.repo.RebuildFTSIndexes(ctx)
		if jobErr == nil {
			jobErr = s.rebuildEmbeddings(ctx, false)
		}
		if jobErr == nil {
			job.Result = map[string]string{"action": "indexes rebuilt", "embeddings": "incremental"}
		}
	case "rebuild_indexes_full":
		jobErr = s.repo.RebuildFTSIndexes(ctx)
		if jobErr == nil {
			jobErr = s.rebuildEmbeddings(ctx, true)
		}
		if jobErr == nil {
			job.Result = map[string]string{"action": "indexes rebuilt", "embeddings": "full"}
		}
	case "extract_claims":
		count, err := s.ExtractClaims(ctx)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "extract_claims", "claims_created": fmt.Sprintf("%d", count)}
		}
	case "enrich_memories":
		count, err := s.EnrichMemories(ctx)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "enrich_memories", "memories_enriched": fmt.Sprintf("%d", count)}
		}
	case "rebuild_entity_graph":
		jobErr = s.repo.RebuildEntityGraphProjection(ctx)
		if jobErr == nil {
			job.Result = map[string]string{"action": "rebuild_entity_graph", "status": "completed"}
		}
	case "form_episodes":
		count, err := s.FormEpisodes(ctx)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "form_episodes", "episodes_created": fmt.Sprintf("%d", count)}
		}
	case "detect_contradictions":
		count, err := s.DetectContradictions(ctx)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "detect_contradictions", "contradictions_found": fmt.Sprintf("%d", count)}
		}
	case "decay_stale_memory":
		count, err := s.DecayStaleMemories(ctx)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "decay_stale_memory", "memories_decayed": fmt.Sprintf("%d", count)}
		}
	case "merge_duplicates":
		count, err := s.MergeDuplicates(ctx)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "merge_duplicates", "merges_performed": fmt.Sprintf("%d", count)}
		}
	case "cleanup_recall_history":
		cleaned, err := s.repo.CleanupRecallHistory(ctx, 7)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "cleanup_recall_history", "deleted": fmt.Sprintf("%d", cleaned)}
		}
	case "reprocess":
		created, superseded, err := s.Reprocess(ctx, false)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "reprocess", "memories_created": fmt.Sprintf("%d", created), "memories_superseded": fmt.Sprintf("%d", superseded)}
		}
	case "reprocess_all":
		created, superseded, err := s.Reprocess(ctx, true)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "reprocess_all", "memories_created": fmt.Sprintf("%d", created), "memories_superseded": fmt.Sprintf("%d", superseded)}
		}
	case "lifecycle_review":
		count, err := s.LifecycleReview(ctx)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "lifecycle_review", "memories_affected": fmt.Sprintf("%d", count)}
		}
	case "cross_project_transfer":
		count, err := s.CrossProjectTransfer(ctx)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "cross_project_transfer", "memories_promoted": fmt.Sprintf("%d", count)}
		}
	case "archive_session_traces":
		count, err := s.ArchiveLowSalienceSessionTraces(ctx)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "archive_session_traces", "memories_archived": fmt.Sprintf("%d", count)}
		}
	case "purge_old_events":
		deleted, err := s.PurgeOldEvents(ctx, 30)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "purge_old_events", "deleted": fmt.Sprintf("%d", deleted)}
		}
	case "purge_old_jobs":
		deleted, err := s.PurgeOldJobs(ctx, 30)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "purge_old_jobs", "deleted": fmt.Sprintf("%d", deleted)}
		}
	case "expire_retrieval_cache":
		deleted, err := s.ExpireRetrievalCache(ctx)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "expire_retrieval_cache", "deleted": fmt.Sprintf("%d", deleted)}
		}
	case "purge_relevance_feedback":
		deleted, err := s.PurgeOldRelevanceFeedback(ctx, 30)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "purge_relevance_feedback", "deleted": fmt.Sprintf("%d", deleted)}
		}
	case "vacuum_analyze":
		jobErr = s.VacuumAnalyze(ctx)
		if jobErr == nil {
			job.Result = map[string]string{"action": "vacuum_analyze", "status": "completed"}
		}
	case "update_ranking_weights":
		count, err := s.UpdateRankingWeights(ctx)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{
				"action":          "update_ranking_weights",
				"weights_updated": fmt.Sprintf("%d", count),
				"scoring_weights": s.scoringWeightsJSON(),
			}
		}
	default:
		jobErr = fmt.Errorf("%w: unknown job kind %q", core.ErrInvalidInput, kind)
	}

	finished := time.Now().UTC()
	job.FinishedAt = &finished
	if jobErr != nil {
		job.Status = "failed"
		job.ErrorText = jobErr.Error()
	} else {
		job.Status = "completed"
	}
	if err := s.repo.UpdateJob(bookkeepingCtx, job); err != nil {
		return job, fmt.Errorf("update job: %w", err)
	}
	if jobErr != nil {
		return job, jobErr
	}
	slog.Debug("RunJob completed successfully", "kind", kind, "jobID", job.ID, "status", job.Status)
	return job, nil
}

// Repair runs integrity checks and optionally applies a targeted repair pass.
func (s *AMMService) Repair(ctx context.Context, check bool, fix string) (*core.RepairReport, error) {
	slog.Debug("Repair called", "check", check, "fix", fix)

	report := &core.RepairReport{}

	if check {
		integrityReport, err := s.CheckIntegrity(ctx)
		if err != nil {
			return report, fmt.Errorf("check integrity: %w", err)
		}
		report.Checked = integrityReport.Checked
		report.Issues = integrityReport.Issues
		report.Details = append(report.Details, integrityReport.Details...)
	}

	if fix != "" {
		switch fix {
		case "indexes":
			if err := s.repo.RebuildFTSIndexes(ctx); err != nil {
				return report, fmt.Errorf("rebuild FTS indexes: %w", err)
			}
			if err := s.rebuildEmbeddings(ctx, true); err != nil {
				return report, fmt.Errorf("rebuild embeddings: %w", err)
			}
			report.Fixed++
			report.Details = append(report.Details, "rebuilt FTS indexes and embeddings")
		case "links":
			fixReport, err := s.FixLinks(ctx)
			if err != nil {
				return report, fmt.Errorf("fix links: %w", err)
			}
			report.Fixed += fixReport.Fixed
			report.Details = append(report.Details, fixReport.Details...)
		case "recall_history":
			cleaned, err := s.repo.CleanupRecallHistory(ctx, 7)
			if err != nil {
				return report, fmt.Errorf("cleanup recall history: %w", err)
			}
			report.Fixed += int(cleaned)
			report.Details = append(report.Details, fmt.Sprintf("cleaned %d recall history entries", cleaned))
		default:
			err := fmt.Errorf("%w: unknown fix type %q", core.ErrInvalidInput, fix)
			return report, err
		}
	}

	slog.Debug("Repair completed successfully", "check", check, "fix", fix, "checked", report.Checked, "fixed", report.Fixed, "issues", report.Issues, "detailsCount", len(report.Details))
	return report, nil
}

// ExplainRecall returns the scoring breakdown that would cause itemID to surface
// for query.
func (s *AMMService) ExplainRecall(ctx context.Context, query string, itemID string) (map[string]interface{}, error) {
	slog.Debug("ExplainRecall called", "query_len", len(query), "itemID", itemID)

	// Build scoring context.
	queryEntities := ExtractEntities(query)
	recentRecalls := make(map[string]bool)
	sctx := ScoringContext{
		Query:          query,
		QueryEmbedding: s.buildQueryEmbedding(ctx, query),
		QueryEntities:  queryEntities,
		Now:            time.Now().UTC(),
		RecentRecalls:  recentRecalls,
		Weights:        nil,
	}
	weights := s.getScoringWeights()
	sctx.Weights = &weights

	// Try to find the item as memory, summary, episode, or event.
	var candidate ScoringCandidate
	var found bool

	if mem, err := s.repo.GetMemory(ctx, itemID); err == nil {
		candidate = MemoryToCandidate(*mem, 0)
		found = true
	} else if sum, err := s.repo.GetSummary(ctx, itemID); err == nil {
		candidate = SummaryToCandidate(*sum, 0)
		found = true
	} else if ep, err := s.repo.GetEpisode(ctx, itemID); err == nil {
		candidate = EpisodeToCandidate(*ep, 0)
		found = true
	} else if evt, err := s.repo.GetEvent(ctx, itemID); err == nil {
		candidate = EventToCandidate(*evt, 0)
		found = true
	}

	if !found {
		err := fmt.Errorf("%w: item %q", core.ErrNotFound, itemID)
		return nil, err
	}

	candidates := []ScoringCandidate{candidate}
	s.attachCandidateEmbeddings(ctx, candidates)
	s.attachCandidateEntities(ctx, candidates)
	candidate = candidates[0]

	breakdown := ScoreItem(candidate, sctx)

	result := map[string]interface{}{
		"query":            query,
		"item_id":          itemID,
		"item_kind":        candidate.Kind,
		"query_entities":   queryEntities,
		"signal_breakdown": breakdown,
		"final_score":      breakdown.FinalScore,
	}
	slog.Debug("ExplainRecall completed successfully", "query_len", len(query), "itemID", itemID, "itemKind", candidate.Kind)
	return result, nil
}

// Status reports repository initialization state and top-level record counts.
func (s *AMMService) Status(ctx context.Context) (*core.StatusResult, error) {
	slog.Debug("Status called")

	initialized, err := s.repo.IsInitialized(ctx)
	if err != nil {
		return nil, fmt.Errorf("check initialized: %w", err)
	}

	evtCount, err := s.repo.CountEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("count events: %w", err)
	}
	pendingCount, err := s.repo.CountUnreflectedEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("count unreflected events: %w", err)
	}
	memCount, err := s.repo.CountMemories(ctx)
	if err != nil {
		return nil, fmt.Errorf("count memories: %w", err)
	}
	sumCount, err := s.repo.CountSummaries(ctx)
	if err != nil {
		return nil, fmt.Errorf("count summaries: %w", err)
	}
	epCount, err := s.repo.CountEpisodes(ctx)
	if err != nil {
		return nil, fmt.Errorf("count episodes: %w", err)
	}
	entCount, err := s.repo.CountEntities(ctx)
	if err != nil {
		return nil, fmt.Errorf("count entities: %w", err)
	}

	result := &core.StatusResult{
		DBPath:            s.dbPath,
		Initialized:       initialized,
		EventCount:        evtCount,
		PendingEventCount: pendingCount,
		MemoryCount:       memCount,
		SummaryCount:      sumCount,
		EpisodeCount:      epCount,
		EntityCount:       entCount,
	}
	slog.Debug("Status completed successfully", "initialized", initialized, "eventCount", evtCount, "pendingEventCount", pendingCount, "memoryCount", memCount, "summaryCount", sumCount, "episodeCount", epCount, "entityCount", entCount)
	return result, nil
}

func (s *AMMService) ResetDerived(ctx context.Context) (*core.ResetDerivedResult, error) {
	slog.Debug("ResetDerived called")
	return s.repo.ResetDerived(ctx)
}
