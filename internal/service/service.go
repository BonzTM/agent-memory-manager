package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/joshd-04/agent-memory-manager/internal/core"
)

// generateID creates a random ID with the given prefix (e.g. "evt_", "mem_").
// Panics if crypto/rand fails, which only happens when the OS entropy source
// is broken — an unrecoverable condition where continuing would be unsafe.
func generateID(prefix string) string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return prefix + hex.EncodeToString(b)
}

// AMMService implements core.Service with business logic on top of a Repository.
type AMMService struct {
	repo               core.Repository
	dbPath             string
	summarizer         core.Summarizer
	reprocessBatchSize int
}

// Compile-time check that AMMService implements core.Service.
var _ core.Service = (*AMMService)(nil)

// New creates a new AMMService backed by the given repository.
func New(repo core.Repository, dbPath string, summarizer ...core.Summarizer) *AMMService {
	selected := core.Summarizer(&HeuristicSummarizer{})
	if len(summarizer) > 0 && summarizer[0] != nil {
		selected = summarizer[0]
	}
	return &AMMService{repo: repo, dbPath: dbPath, summarizer: selected, reprocessBatchSize: defaultBatchSize}
}

func (s *AMMService) SetReprocessBatchSize(batchSize int) {
	if batchSize <= 0 {
		s.reprocessBatchSize = defaultBatchSize
		return
	}
	s.reprocessBatchSize = batchSize
}

// Init initializes the database: creates the parent directory, opens the DB,
// and runs migrations.
func (s *AMMService) Init(ctx context.Context, dbPath string) error {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent directory: %w", err)
	}
	if err := s.repo.Open(ctx, dbPath); err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	s.dbPath = dbPath
	if err := s.repo.Migrate(ctx); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

// IngestEvent appends a raw event to history, respecting ingestion policies.
func (s *AMMService) IngestEvent(ctx context.Context, event *core.Event) (*core.Event, error) {
	// Check ingestion policy.
	shouldIngest, createMemory, err := s.ShouldIngest(ctx, event)
	if err != nil {
		return nil, fmt.Errorf("check ingestion policy: %w", err)
	}
	if !shouldIngest {
		return event, nil // silently skip per policy
	}

	if event.ID == "" {
		event.ID = generateID("evt_")
	}
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
	return event, nil
}

// IngestTranscript bulk-ingests a sequence of events.
func (s *AMMService) IngestTranscript(ctx context.Context, events []*core.Event) (int, error) {
	ingested := 0
	for _, evt := range events {
		if _, err := s.IngestEvent(ctx, evt); err != nil {
			return ingested, fmt.Errorf("ingest event %d: %w", ingested, err)
		}
		ingested++
	}
	return ingested, nil
}

// Remember commits an explicit durable memory, handling supersession if specified.
func (s *AMMService) Remember(ctx context.Context, memory *core.Memory) (*core.Memory, error) {
	now := time.Now().UTC()
	if memory.ID == "" {
		memory.ID = generateID("mem_")
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

	// Handle supersession: mark the old memory as superseded.
	if memory.Supersedes != "" {
		old, err := s.repo.GetMemory(ctx, memory.Supersedes)
		if err == nil {
			old.Status = core.MemoryStatusSuperseded
			old.SupersededBy = memory.ID
			old.SupersededAt = &now
			old.UpdatedAt = now
			_ = s.repo.UpdateMemory(ctx, old)
		}
	}

	if err := s.repo.InsertMemory(ctx, memory); err != nil {
		return nil, fmt.Errorf("insert memory: %w", err)
	}
	return memory, nil
}

// GetMemory retrieves a single memory by ID.
func (s *AMMService) GetMemory(ctx context.Context, id string) (*core.Memory, error) {
	return s.repo.GetMemory(ctx, id)
}

// GetSummary retrieves a single summary by ID.
func (s *AMMService) GetSummary(ctx context.Context, id string) (*core.Summary, error) {
	return s.repo.GetSummary(ctx, id)
}

// GetEpisode retrieves a single episode by ID.
func (s *AMMService) GetEpisode(ctx context.Context, id string) (*core.Episode, error) {
	return s.repo.GetEpisode(ctx, id)
}

// GetEntity retrieves a single entity by ID.
func (s *AMMService) GetEntity(ctx context.Context, id string) (*core.Entity, error) {
	return s.repo.GetEntity(ctx, id)
}

// UpdateMemory updates an existing memory, setting UpdatedAt.
func (s *AMMService) UpdateMemory(ctx context.Context, memory *core.Memory) (*core.Memory, error) {
	memory.UpdatedAt = time.Now().UTC()
	if err := s.repo.UpdateMemory(ctx, memory); err != nil {
		return nil, fmt.Errorf("update memory: %w", err)
	}
	return memory, nil
}

// Describe returns thin descriptions for one or more items.
// For each ID it tries memories, then summaries, then episodes.
func (s *AMMService) Describe(ctx context.Context, ids []string) ([]core.DescribeResult, error) {
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
	return results, nil
}

// Expand returns the full expansion of a single item.
func (s *AMMService) Expand(ctx context.Context, id string, kind string) (*core.ExpandResult, error) {
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
		return nil, fmt.Errorf("%w: unknown kind %q", core.ErrInvalidInput, kind)
	}

	return result, nil
}

// History retrieves raw history by query or session.
func (s *AMMService) History(ctx context.Context, query string, opts core.HistoryOptions) ([]core.Event, error) {
	if opts.Limit == 0 {
		opts.Limit = 50
	}
	if opts.SessionID != "" {
		return s.repo.ListEvents(ctx, core.ListEventsOptions{
			SessionID: opts.SessionID,
			ProjectID: opts.ProjectID,
			Limit:     opts.Limit,
			Before:    opts.Before,
			After:     opts.After,
		})
	}
	if query != "" {
		return s.repo.SearchEvents(ctx, query, opts.Limit)
	}
	// Fallback: list all events with the given filters.
	return s.repo.ListEvents(ctx, core.ListEventsOptions{
		ProjectID: opts.ProjectID,
		Limit:     opts.Limit,
		Before:    opts.Before,
		After:     opts.After,
	})
}

// RunJob executes a maintenance job by kind.
func (s *AMMService) RunJob(ctx context.Context, kind string) (*core.Job, error) {
	now := time.Now().UTC()
	job := &core.Job{
		ID:        generateID("job_"),
		Kind:      kind,
		Status:    "running",
		StartedAt: &now,
		CreatedAt: now,
	}
	if err := s.repo.InsertJob(ctx, job); err != nil {
		return nil, fmt.Errorf("insert job: %w", err)
	}

	var jobErr error
	switch kind {
	case "reflect":
		count, err := s.Reflect(ctx)
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
	case "rebuild_indexes":
		jobErr = s.repo.RebuildFTSIndexes(ctx)
		if jobErr == nil {
			job.Result = map[string]string{"action": "indexes rebuilt"}
		}
	case "extract_claims":
		count, err := s.ExtractClaims(ctx)
		jobErr = err
		if jobErr == nil {
			job.Result = map[string]string{"action": "extract_claims", "claims_created": fmt.Sprintf("%d", count)}
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
	if err := s.repo.UpdateJob(ctx, job); err != nil {
		return job, fmt.Errorf("update job: %w", err)
	}
	return job, jobErr
}

// Repair runs integrity checks and optionally fixes issues.
func (s *AMMService) Repair(ctx context.Context, check bool, fix string) (*core.RepairReport, error) {
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
			report.Fixed++
			report.Details = append(report.Details, "rebuilt FTS indexes")
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
			return report, fmt.Errorf("%w: unknown fix type %q", core.ErrInvalidInput, fix)
		}
	}

	return report, nil
}

// ExplainRecall explains why an item surfaced for a query using the scoring engine.
func (s *AMMService) ExplainRecall(ctx context.Context, query string, itemID string) (map[string]interface{}, error) {
	// Build scoring context.
	queryEntities := ExtractEntities(query)
	recentRecalls := make(map[string]bool)
	sctx := ScoringContext{
		Query:         query,
		QueryEntities: queryEntities,
		Now:           time.Now().UTC(),
		RecentRecalls: recentRecalls,
	}

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
		return nil, fmt.Errorf("%w: item %q", core.ErrNotFound, itemID)
	}

	breakdown := ScoreItem(candidate, sctx)

	return map[string]interface{}{
		"query":            query,
		"item_id":          itemID,
		"item_kind":        candidate.Kind,
		"query_entities":   queryEntities,
		"signal_breakdown": breakdown,
		"final_score":      breakdown.FinalScore,
	}, nil
}

// Status returns system status information.
func (s *AMMService) Status(ctx context.Context) (*core.StatusResult, error) {
	initialized, err := s.repo.IsInitialized(ctx)
	if err != nil {
		return nil, fmt.Errorf("check initialized: %w", err)
	}

	evtCount, err := s.repo.CountEvents(ctx)
	if err != nil {
		return nil, fmt.Errorf("count events: %w", err)
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

	return &core.StatusResult{
		DBPath:       s.dbPath,
		Initialized:  initialized,
		EventCount:   evtCount,
		MemoryCount:  memCount,
		SummaryCount: sumCount,
		EpisodeCount: epCount,
		EntityCount:  entCount,
	}, nil
}
