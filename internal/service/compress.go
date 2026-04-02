package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

// jobFrontierSequenceID returns the max_sequence_id stored in the Result map
// of the most recently completed job of the given kind.  Returns 0 if no
// completed job exists or the key is absent.
func (s *AMMService) jobFrontierSequenceID(ctx context.Context, kind string) int64 {
	jobs, err := s.repo.ListJobs(ctx, core.ListJobsOptions{
		Kind:   kind,
		Status: "completed",
		Limit:  1,
	})
	if err != nil || len(jobs) == 0 {
		return 0
	}
	raw, ok := jobs[0].Result["max_sequence_id"]
	if !ok {
		return 0
	}
	v, _ := strconv.ParseInt(raw, 10, 64)
	return v
}

// lastCompletedJobTime returns the finish time of the most recent completed
// job of the given kind, or zero time if none exists.
func (s *AMMService) lastCompletedJobTime(ctx context.Context, kind string) time.Time {
	jobs, err := s.repo.ListJobs(ctx, core.ListJobsOptions{
		Kind:   kind,
		Status: "completed",
		Limit:  1,
	})
	if err != nil || len(jobs) == 0 || jobs[0].FinishedAt == nil {
		return time.Time{}
	}
	return *jobs[0].FinishedAt
}

func (s *AMMService) legacyCompressWatermark(ctx context.Context) string {
	jobs, err := s.repo.ListJobs(ctx, core.ListJobsOptions{
		Kind:   "compress",
		Status: "completed",
		Limit:  1,
	})
	if err != nil || len(jobs) == 0 || jobs[0].FinishedAt == nil {
		return ""
	}
	return jobs[0].FinishedAt.Format(time.RFC3339Nano)
}

// maxEventSequenceID returns the highest SequenceID in a slice of events.
func maxEventSequenceID(events []core.Event) int64 {
	var max int64
	for _, evt := range events {
		if evt.SequenceID > max {
			max = evt.SequenceID
		}
	}
	return max
}

const (
	defaultCompressChunkSize               = 10
	defaultCompressMaxEvents               = 200
	defaultCompressBatchSize               = 15
	defaultTopicBatchSize                  = 15
	leafBodyMaxChars                       = 1000
	sessionBodyMaxChars                    = 2000
	topicBodyMaxChars                      = 2000
	defaultEscalationDeterministicMaxChars = 2048
	defaultSessionIdleTimeout              = 15 * time.Minute
	defaultSummarizerContextWindow         = 128000 // tokens
	defaultCompressCooldown                = 24 * time.Hour
)

type compressEventChunkPlan struct {
	index      int
	chunk      []core.Event
	eventIDs   []string
	contents   []string
	joinedBody string
	firstTime  string
	lastTime   string
}

type topicSummaryPlan struct {
	index      int
	group      []core.Summary
	childIDs   []string
	contents   []string
	mergedBody string
	title      string
}

type sessionDerivedArtifacts struct {
	summaryIDs []string
	episodeIDs []string
}

// CompressHistory summarizes recent event chunks into leaf summaries and
// returns the number created.
func (s *AMMService) CompressHistory(ctx context.Context) (int, error) {
	slog.Debug("CompressHistory called")

	// Cooldown: skip if ran within the configured period (default 24h).
	cooldown := s.compressCooldown
	if cooldown <= 0 {
		cooldown = defaultCompressCooldown
	}
	if lastRun := s.lastCompletedJobTime(ctx, "compress"); !lastRun.IsZero() {
		if time.Since(lastRun) < cooldown {
			slog.Debug("CompressHistory skipped (cooldown)", "last_run", lastRun, "cooldown", cooldown)
			return 0, nil
		}
	}

	frontier := s.jobFrontierSequenceID(ctx, "compress")

	maxEvents := s.compressMaxEvents
	if maxEvents <= 0 {
		maxEvents = defaultCompressMaxEvents
	}
	opts := core.ListEventsOptions{Limit: maxEvents}
	if frontier > 0 {
		opts.AfterSequenceID = frontier
	} else if afterTime := s.legacyCompressWatermark(ctx); afterTime != "" {
		opts.After = afterTime
	}
	events, err := s.repo.ListEvents(ctx, opts)
	if err != nil {
		return 0, fmt.Errorf("list events for compress: %w", err)
	}

	if len(events) == 0 {
		return 0, nil
	}

	created := 0

	chunkSize := s.compressChunkSize
	if chunkSize <= 0 {
		chunkSize = defaultCompressChunkSize
	}
	plans := make([]compressEventChunkPlan, 0, (len(events)+chunkSize-1)/chunkSize)

	compressBatchSize := s.compressBatchSize
	if compressBatchSize <= 0 {
		compressBatchSize = defaultCompressBatchSize
	}
	for i := 0; i < len(events); i += chunkSize {
		end := i + chunkSize
		if end > len(events) {
			end = len(events)
		}
		chunk := events[i:end]
		if len(chunk) == 0 {
			continue
		}

		eventIDs := make([]string, 0, len(chunk))
		contents := make([]string, 0, len(chunk))
		var bodyBuilder strings.Builder
		for _, evt := range chunk {
			eventIDs = append(eventIDs, evt.ID)
			contents = append(contents, evt.Content)
			if bodyBuilder.Len() > 0 {
				bodyBuilder.WriteByte('\n')
			}
			bodyBuilder.WriteString(evt.Content)
		}

		plans = append(plans, compressEventChunkPlan{
			index:      len(plans) + 1,
			chunk:      chunk,
			eventIDs:   eventIDs,
			contents:   contents,
			joinedBody: bodyBuilder.String(),
			firstTime:  chunk[0].OccurredAt.Format(time.RFC3339),
			lastTime:   chunk[len(chunk)-1].OccurredAt.Format(time.RFC3339),
		})
	}

	batchResults := make(map[int]core.CompressionResult, len(plans))
	batchSucceeded := false
	if s.intelligence != nil && len(plans) > 0 {
		batchSucceeded = true
		for start := 0; start < len(plans); start += compressBatchSize {
			end := start + compressBatchSize
			if end > len(plans) {
				end = len(plans)
			}

			chunks := make([]core.EventChunk, 0, end-start)
			for _, plan := range plans[start:end] {
				chunks = append(chunks, core.EventChunk{
					Index:    plan.index,
					Contents: plan.contents,
				})
			}

			results, err := s.intelligence.CompressEventBatches(ctx, chunks)
			if err != nil {
				batchSucceeded = false
				break
			}
			for _, result := range results {
				batchResults[result.Index] = result
			}
		}
	}

	if !batchSucceeded {
		batchResults = map[int]core.CompressionResult{}
	}

	for _, plan := range plans {
		// Determine scope.
		scope, projectID := inferScopeFromEvents(plan.chunk)

		tightDesc := fmt.Sprintf("Summary of %d events from %s to %s", len(plan.chunk), plan.firstTime, plan.lastTime)
		body := ""
		if result, ok := batchResults[plan.index]; ok {
			if trimmedBody := strings.TrimSpace(result.Body); trimmedBody != "" && len(trimmedBody) < len(plan.joinedBody) {
				body = trimmedBody
			}
			if cleaned, ok := sanitizeTightDescription(result.TightDescription); ok {
				tightDesc = cleaned
			}
		}

		if body == "" {
			var err error
			body, err = s.escalate(ctx, plan.joinedBody, leafBodyMaxChars)
			if err != nil {
				return created, fmt.Errorf("summarize leaf body: %w", err)
			}
			if tightResult, err := s.intelligence.Summarize(ctx, body, 100); err == nil {
				if cleaned, ok := sanitizeTightDescription(tightResult); ok {
					tightDesc = cleaned
				}
			}
		}

		now := time.Now().UTC()
		summary := &core.Summary{
			ID:               core.GenerateID("sum_"),
			Kind:             "leaf",
			Depth:            0,
			Scope:            scope,
			ProjectID:        projectID,
			Title:            fmt.Sprintf("Events %s to %s", plan.firstTime, plan.lastTime),
			Body:             body,
			TightDescription: tightDesc,
			PrivacyLevel:     core.PrivacyPrivate,
			SourceSpan: core.SourceSpan{
				EventIDs: plan.eventIDs,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}

		if err := s.repo.InsertSummary(ctx, summary); err != nil {
			return created, fmt.Errorf("insert leaf summary: %w", err)
		}

		// Create edges linking summary to each event.
		for order, eid := range plan.eventIDs {
			edge := &core.SummaryEdge{
				ParentSummaryID: summary.ID,
				ChildKind:       "event",
				ChildID:         eid,
				EdgeOrder:       order,
			}
			if err := s.repo.InsertSummaryEdge(ctx, edge); err != nil {
				return created, fmt.Errorf("insert summary edge: %w", err)
			}
		}

		created++
	}

	now := time.Now().UTC()
	job := &core.Job{
		ID:         core.GenerateID("job_"),
		Kind:       "compress",
		Status:     "completed",
		StartedAt:  &now,
		FinishedAt: &now,
		Result: map[string]string{
			"created":         fmt.Sprintf("%d", created),
			"max_sequence_id": fmt.Sprintf("%d", maxEventSequenceID(events)),
		},
		CreatedAt: now,
	}
	if err := s.repo.InsertJob(ctx, job); err != nil {
		return created, fmt.Errorf("record compress job: %w", err)
	}

	return created, nil
}

// ConsolidateSessions creates session-level summaries from grouped session
// events and returns the number created.
func (s *AMMService) ConsolidateSessions(ctx context.Context) (int, error) {
	slog.Debug("ConsolidateSessions called")
	frontier := s.jobFrontierSequenceID(ctx, "consolidate_sessions")

	opts := core.ListEventsOptions{Limit: 500}
	if frontier > 0 {
		opts.AfterSequenceID = frontier
	}
	newEvents, err := s.repo.ListEvents(ctx, opts)
	if err != nil {
		return 0, fmt.Errorf("list events for consolidate: %w", err)
	}

	if len(newEvents) == 0 {
		return 0, nil
	}

	candidateSessionIDs := make(map[string]bool)
	for _, evt := range newEvents {
		if evt.SessionID != "" {
			candidateSessionIDs[evt.SessionID] = true
		}
	}

	if len(candidateSessionIDs) == 0 {
		// No session events in this batch — still advance the frontier so we
		// don't re-scan the same sessionless events on the next run.
		return 0, s.recordConsolidateFrontier(ctx, 0, maxEventSequenceID(newEvents))
	}

	created := 0

	idleTimeout := s.sessionIdleTimeout
	now := time.Now().UTC()

	// Track the max sequence ID of sessions we actually processed (not skipped),
	// and the minimum sequence ID of any skipped session so we don't advance
	// the frontier past it.
	var processedMaxSeq int64
	var skippedMinSeq int64

	for sessionID := range candidateSessionIDs {
		// Incremental consolidation: find unreflected events for this session.
		evts, err := s.repo.ListEvents(ctx, core.ListEventsOptions{
			SessionID:       sessionID,
			UnreflectedOnly: true,
			Limit:           10000,
		})
		if err != nil {
			return created, fmt.Errorf("list session events for consolidate %s: %w", sessionID, err)
		}
		if len(evts) == 0 {
			continue
		}

		// Idle-timeout check: skip sessions that are still active.
		// When idleTimeout is 0, skip the check (process immediately).
		// Bypass the idle gate if the session has an explicit session_stop event.
		hasStopEvent := false
		for _, evt := range evts {
			if evt.Kind == "session_stop" {
				hasStopEvent = true
				break
			}
		}
		if idleTimeout > 0 && !hasStopEvent {
			latestEvent := evts[len(evts)-1].OccurredAt
			for _, evt := range evts {
				if evt.OccurredAt.After(latestEvent) {
					latestEvent = evt.OccurredAt
				}
			}
			if now.Sub(latestEvent) < idleTimeout {
				slog.Debug("session still active, skipping consolidation",
					"session_id", sessionID,
					"latest_event", latestEvent,
					"idle_timeout", idleTimeout)
				// Track the minimum sequence of skipped sessions so the
				// frontier doesn't advance past them.
				minSeq := evts[0].SequenceID
				for _, evt := range evts[1:] {
					if evt.SequenceID < minSeq {
						minSeq = evt.SequenceID
					}
				}
				if skippedMinSeq == 0 || minSeq < skippedMinSeq {
					skippedMinSeq = minSeq
				}
				continue
			}
		}

		eventIDs := make([]string, 0, len(evts))
		var bodyBuilder strings.Builder
		eventContents := make([]core.EventContent, 0, len(evts))
		for _, evt := range evts {
			eventIDs = append(eventIDs, evt.ID)
			eventContents = append(eventContents, core.EventContent{
				Index:     len(eventContents) + 1,
				Content:   evt.Content,
				ProjectID: evt.ProjectID,
				SessionID: evt.SessionID,
			})
			if bodyBuilder.Len() > 0 {
				bodyBuilder.WriteByte('\n')
			}
			bodyBuilder.WriteString(evt.Content)
		}

		scope, projectID := inferScopeFromEvents(evts)

		existingMemories, linkedMemories, err := s.collectSessionMemoryContext(ctx, sessionID, eventIDs)
		if err != nil {
			return created, fmt.Errorf("collect session memory context: %w", err)
		}
		openLoops := s.activeOpenLoopsForScope(ctx, scope, projectID)
		narrativeMemoryContext := appendMemorySummaries(existingMemories, openLoops)

		// Fetch prior summary for context continuity (incremental consolidation).
		priorSummary := s.latestSessionSummary(ctx, sessionID)
		priorSummaryFallbackCount := currentSummaryFallbackCount(priorSummary)
		retryArtifacts := sessionDerivedArtifacts{}
		sessionRetryRun := summaryNeedsLLMRetry(priorSummary) || sessionHasRetryableMemories(linkedMemories)
		if sessionRetryRun {
			retryArtifacts, err = s.listSessionDerivedArtifacts(ctx, sessionID)
			if err != nil {
				return created, fmt.Errorf("list prior session artifacts for retry: %w", err)
			}
			priorSummary = nil
		}

		// Prepend prior summary as context for the narrative LLM.
		narrativeContents := eventContents
		narrativeJoined := bodyBuilder.String()
		if priorSummary != nil && priorSummary.Body != "" {
			contextPrefix := fmt.Sprintf("[Previously in this session]\n%s\n\n[New events in this activity burst]", priorSummary.Body)
			priorContent := core.EventContent{
				Index:     0, // synthetic, before real events
				Content:   contextPrefix,
				SessionID: sessionID,
				ProjectID: projectID,
			}
			narrativeContents = append([]core.EventContent{priorContent}, eventContents...)
			// Re-index so the LLM sees sequential indices
			for i := range narrativeContents {
				narrativeContents[i].Index = i + 1
			}
			narrativeJoined = contextPrefix + "\n" + bodyBuilder.String()
		}

		// Check if we need map-reduce chunking for large sessions.
		narrativeContents, narrativeJoined = s.chunkSessionIfNeeded(ctx, narrativeContents, narrativeJoined, existingMemories, sessionID)

		body, tightDesc, narrativeResult, usedNarrative, narrativeMethod, err := s.buildSessionNarrative(ctx, narrativeContents, narrativeJoined, evts, narrativeMemoryContext)
		if err != nil {
			return created, err
		}
		narrativeRetryable := s.intelligence != nil && s.intelligence.IsLLMBacked() && narrativeMethod == MethodHeuristic
		resolvedOpenLoopIDs := []string(nil)

		if usedNarrative {
			if err := s.insertNarrativeEpisode(ctx, narrativeResult, sessionID, scope, projectID, eventIDs, evts, narrativeMethod, narrativeRetryable, priorSummaryFallbackCount); err != nil {
				return created, fmt.Errorf("insert narrative episode: %w", err)
			}
			resolvedOpenLoopIDs = append(resolvedOpenLoopIDs, narrativeResult.ResolvedLoops...)
			openLoops = filterOpenLoopsByID(openLoops, narrativeResult.ResolvedLoops)

			// Extract memories from the narrative using the full extraction pipeline.
			// Include KeyDecisions/Unresolved and active open loops as
			// supplementary context so the extraction LLM can close resolved
			// loops and avoid re-creating existing ones.
			if s.intelligence != nil && narrativeResult.Summary != "" {
				extractionInput := buildExtractionInput(narrativeResult, openLoops)

				// Try AnalyzeEvents on the narrative for entities/relationships
				// alongside memory extraction (single LLM call when supported).
				var extracted []core.MemoryCandidate
				var analysisEntities []core.EntityCandidate
				var analysisRelationships []core.RelationshipCandidate
				usedAnalysis := false
				extractionMethod := narrativeMethod
				retryableHeuristic := narrativeRetryable

				if s.intelligence.IsLLMBacked() {
					narrativeEvent := []core.EventContent{{
						Index:     1,
						Content:   extractionInput,
						ProjectID: projectID,
						SessionID: sessionID,
					}}
					analysis, analysisMethod, analysisErr := analyzeEventsWithMethod(ctx, s.intelligence, narrativeEvent)
					if analysisErr == nil && analysis != nil && len(analysis.Memories) > 0 {
						extracted = analysis.Memories
						analysisEntities = analysis.Entities
						analysisRelationships = analysis.Relationships
						usedAnalysis = true
						extractionMethod = analysisMethod
						retryableHeuristic = analysisMethod == MethodHeuristic
					}
				}

				// Fall back to extraction-only if analysis didn't produce memories.
				if len(extracted) == 0 {
					var err error
					extracted, extractionMethod, err = extractBatchWithMethod(ctx, s.intelligence, []string{extractionInput})
					if err != nil {
						slog.Warn("narrative memory extraction failed, skipping",
							"session_id", sessionID, "error", err)
					}
					retryableHeuristic = extractionMethod == MethodHeuristic && s.intelligence.IsLLMBacked()
				}

				if len(extracted) > 0 {
					scopeVal := scope
					memCreated, err := s.processMemoryCandidates(ctx, candidateProcessingInput{
						candidates:            extracted,
						sourceEvents:          evts,
						sourceSystem:          "consolidate_sessions",
						scopeOverride:         &scopeVal,
						projectOverride:       projectID,
						sessionID:             sessionID,
						analysisEntities:      analysisEntities,
						analysisRelationships: analysisRelationships,
						usedAnalysis:          usedAnalysis,
						extractionMethod:      extractionMethod,
						retryableHeuristic:    retryableHeuristic,
					})
					if err != nil {
						return created, fmt.Errorf("process narrative memory candidates: %w", err)
					}
					slog.Debug("extracted memories from session narrative",
						"session_id", sessionID, "created", memCreated)
				}
			}
		}

		// Use earliest source event time as the summary's CreatedAt so that
		// temporal session recall filters by when the session happened, not
		// when consolidation ran.
		summaryCreatedAt := time.Now().UTC()
		if len(evts) > 0 {
			earliest := evts[0].OccurredAt
			for _, evt := range evts[1:] {
				if !evt.OccurredAt.IsZero() && evt.OccurredAt.Before(earliest) {
					earliest = evt.OccurredAt
				}
			}
			if !earliest.IsZero() {
				summaryCreatedAt = earliest
			}
		}
		summaryUpdatedAt := time.Now().UTC()

		// Use LLM-generated title if available; fall back to session ID.
		summaryTitle := fmt.Sprintf("Session %s", sessionID)
		if narrativeResult != nil && strings.TrimSpace(narrativeResult.Title) != "" {
			summaryTitle = strings.TrimSpace(narrativeResult.Title)
		} else if priorSummary != nil {
			summaryTitle = fmt.Sprintf("Session %s (continued)", sessionID)
		}

		summary := &core.Summary{
			ID:               core.GenerateID("sum_"),
			Kind:             "session",
			Depth:            0,
			Scope:            scope,
			ProjectID:        projectID,
			SessionID:        sessionID,
			Title:            summaryTitle,
			Body:             body,
			TightDescription: tightDesc,
			PrivacyLevel:     core.PrivacyPrivate,
			SourceSpan: core.SourceSpan{
				EventIDs: eventIDs,
			},
			CreatedAt: summaryCreatedAt,
			UpdatedAt: summaryUpdatedAt,
		}
		summary.Metadata = applyExtractionMetadata(summary.Metadata, narrativeMethod, s.extractionModelName(), narrativeRetryable, priorSummaryFallbackCount)

		if err := s.repo.InsertSummary(ctx, summary); err != nil {
			return created, fmt.Errorf("insert session summary: %w", err)
		}

		for order, eid := range eventIDs {
			edge := &core.SummaryEdge{
				ParentSummaryID: summary.ID,
				ChildKind:       "event",
				ChildID:         eid,
				EdgeOrder:       order,
			}
			if err := s.repo.InsertSummaryEdge(ctx, edge); err != nil {
				return created, fmt.Errorf("insert session summary edge: %w", err)
			}
		}

		if err := s.markMemoriesNarrativeIncluded(ctx, linkedMemories); err != nil {
			return created, fmt.Errorf("mark narrative included metadata: %w", err)
		}
		if err := s.archiveResolvedOpenLoops(ctx, resolvedOpenLoopIDs); err != nil {
			return created, fmt.Errorf("archive resolved open loops: %w", err)
		}
		if sessionRetryRun {
			if err := s.deleteSessionDerivedArtifacts(ctx, retryArtifacts); err != nil {
				return created, fmt.Errorf("delete prior session artifacts for retry: %w", err)
			}
		}

		shouldRetrySession, err := s.shouldRetrySourceEvents(ctx, eventIDs)
		if err != nil {
			return created, fmt.Errorf("check retryable session events: %w", err)
		}
		if narrativeRetryable && priorSummaryFallbackCount+1 < maxHeuristicFallbackRetries {
			shouldRetrySession = true
		}

		// Mark events reflected AFTER summary persistence succeeds.
		// If we mark before and the summary write fails, the burst is lost.
		if shouldRetrySession {
			if err := s.clearEventsReflected(ctx, evts); err != nil {
				return created, fmt.Errorf("clear session events reflected for retry: %w", err)
			}
			minSeq := evts[0].SequenceID
			for _, evt := range evts[1:] {
				if evt.SequenceID < minSeq {
					minSeq = evt.SequenceID
				}
			}
			if skippedMinSeq == 0 || minSeq < skippedMinSeq {
				skippedMinSeq = minSeq
			}
		} else {
			if err := s.markEventsReflected(ctx, evts); err != nil {
				return created, fmt.Errorf("mark session events reflected: %w", err)
			}
		}

		// Only advance frontier past sessions we actually completed.
		if !shouldRetrySession {
			if seq := maxEventSequenceID(evts); seq > processedMaxSeq {
				processedMaxSeq = seq
			}
		}

		created++
	}

	// Record frontier. If any sessions were skipped (idle-timeout), cap the
	// frontier just below their earliest event so they're re-discovered.
	frontierSeq := processedMaxSeq
	if frontierSeq == 0 {
		frontierSeq = maxEventSequenceID(newEvents)
	}
	if skippedMinSeq > 0 && skippedMinSeq-1 < frontierSeq {
		frontierSeq = skippedMinSeq - 1
	}
	if err := s.recordConsolidateFrontier(ctx, created, frontierSeq); err != nil {
		return created, err
	}

	return created, nil
}

func (s *AMMService) BuildTopicSummaries(ctx context.Context) (int, error) {
	slog.Debug("BuildTopicSummaries called")
	hasWork, err := s.hasNewLeafSummariesSinceLastTopicJob(ctx)
	if err != nil {
		return 0, fmt.Errorf("check for new leaf summaries: %w", err)
	}
	if !hasWork {
		return 0, nil
	}

	allLeafSummaries, err := s.repo.ListSummaries(ctx, core.ListSummariesOptions{
		Kind:  "leaf",
		Limit: 50000,
	})
	if err != nil {
		return 0, fmt.Errorf("list leaf summaries: %w", err)
	}
	if len(allLeafSummaries) == 0 {
		return 0, nil
	}

	recordTopicJob := func(created int) error {
		now := time.Now().UTC()
		topicJob := &core.Job{
			ID:         core.GenerateID("job_"),
			Kind:       "build_topic_summaries",
			Status:     "completed",
			StartedAt:  &now,
			FinishedAt: &now,
			Result:     map[string]string{"created": fmt.Sprintf("%d", created)},
			CreatedAt:  now,
		}
		return s.repo.InsertJob(ctx, topicJob)
	}

	parentedLeafIDs, err := s.collectParentedLeafSummaryIDs(ctx)
	if err != nil {
		return 0, err
	}

	unparentedLeafs := make([]core.Summary, 0, len(allLeafSummaries))
	for _, leaf := range allLeafSummaries {
		if _, ok := parentedLeafIDs[leaf.ID]; ok {
			continue
		}
		unparentedLeafs = append(unparentedLeafs, leaf)
	}
	if len(unparentedLeafs) == 0 {
		return 0, nil
	}

	groups := groupLeafSummariesByEntities(unparentedLeafs)
	if len(groups) == 0 {
		if err := recordTopicJob(0); err != nil {
			return 0, fmt.Errorf("record build_topic_summaries job: %w", err)
		}
		return 0, nil
	}

	plans := make([]topicSummaryPlan, 0, len(groups))
	for _, group := range groups {
		if len(group) < 3 {
			continue
		}

		childIDs := make([]string, 0, len(group))
		contents := make([]string, 0, len(group))
		var mergedBodyBuilder strings.Builder
		for i, summary := range group {
			childIDs = append(childIDs, summary.ID)
			contents = append(contents, summary.Body)
			if i > 0 {
				mergedBodyBuilder.WriteString("\n\n")
			}
			mergedBodyBuilder.WriteString(summary.Body)
		}

		plans = append(plans, topicSummaryPlan{
			index:      len(plans) + 1,
			group:      group,
			childIDs:   childIDs,
			contents:   contents,
			mergedBody: mergedBodyBuilder.String(),
			title:      fmt.Sprintf("Topic summary over %d leaf summaries", len(group)),
		})
	}

	if len(plans) == 0 {
		return 0, nil
	}

	batchResults := make(map[int]core.CompressionResult, len(plans))
	batchSucceeded := false
	topicBatchSize := s.topicBatchSize
	if topicBatchSize <= 0 {
		topicBatchSize = defaultTopicBatchSize
	}
	if s.intelligence != nil {
		batchSucceeded = true
		for start := 0; start < len(plans); start += topicBatchSize {
			end := start + topicBatchSize
			if end > len(plans) {
				end = len(plans)
			}

			topics := make([]core.TopicChunk, 0, end-start)
			for _, plan := range plans[start:end] {
				topics = append(topics, core.TopicChunk{
					Index:    plan.index,
					Contents: plan.contents,
					Title:    plan.title,
				})
			}

			results, err := s.intelligence.SummarizeTopicBatches(ctx, topics)
			if err != nil {
				batchSucceeded = false
				break
			}
			for _, result := range results {
				batchResults[result.Index] = result
			}
		}
	}
	if !batchSucceeded {
		batchResults = map[int]core.CompressionResult{}
	}

	created := 0
	for _, plan := range plans {
		body := ""
		tightDesc := extractTightDescription(plan.mergedBody, 100)
		if result, ok := batchResults[plan.index]; ok {
			if trimmedBody := strings.TrimSpace(result.Body); trimmedBody != "" && len(trimmedBody) < len(plan.mergedBody) {
				body = trimmedBody
			}
			if cleaned, ok := sanitizeTightDescription(result.TightDescription); ok {
				tightDesc = cleaned
			}
		}
		if body == "" {
			var err error
			body, tightDesc, err = s.summarizeTopicGroup(ctx, plan.mergedBody)
			if err != nil {
				return created, err
			}
		}

		scope, projectID := inferScopeFromSummaries(plan.group)
		now := time.Now().UTC()
		topicSummary := &core.Summary{
			ID:               core.GenerateID("sum_"),
			Kind:             "topic",
			Depth:            1,
			Scope:            scope,
			ProjectID:        projectID,
			Title:            plan.title,
			Body:             body,
			TightDescription: tightDesc,
			PrivacyLevel:     core.PrivacyPrivate,
			SourceSpan: core.SourceSpan{
				SummaryIDs: plan.childIDs,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := s.repo.InsertSummary(ctx, topicSummary); err != nil {
			return created, fmt.Errorf("insert topic summary: %w", err)
		}

		for order, childID := range plan.childIDs {
			edge := &core.SummaryEdge{
				ParentSummaryID: topicSummary.ID,
				ChildKind:       "summary",
				ChildID:         childID,
				EdgeOrder:       order,
			}
			if err := s.repo.InsertSummaryEdge(ctx, edge); err != nil {
				return created, fmt.Errorf("insert topic summary edge: %w", err)
			}
		}

		created++
	}

	if err := recordTopicJob(created); err != nil {
		return created, fmt.Errorf("record build_topic_summaries job: %w", err)
	}

	return created, nil
}

func (s *AMMService) hasNewLeafSummariesSinceLastTopicJob(ctx context.Context) (bool, error) {
	allLeaves, err := s.repo.ListSummaries(ctx, core.ListSummariesOptions{
		Kind:  "leaf",
		Limit: 50000,
	})
	if err != nil {
		return false, fmt.Errorf("list leaf summaries for topic gate: %w", err)
	}
	if len(allLeaves) == 0 {
		return false, nil
	}

	parented, err := s.collectParentedLeafSummaryIDs(ctx)
	if err != nil {
		return false, fmt.Errorf("collect parented leaf IDs for topic gate: %w", err)
	}

	for _, leaf := range allLeaves {
		if _, ok := parented[leaf.ID]; !ok {
			return true, nil
		}
	}

	return false, nil
}

func (s *AMMService) collectParentedLeafSummaryIDs(ctx context.Context) (map[string]struct{}, error) {
	parented, err := s.repo.ListParentedSummaryIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list parented summary ids: %w", err)
	}

	parentedLeafIDs := make(map[string]struct{}, len(parented))
	for id := range parented {
		parentedLeafIDs[id] = struct{}{}
	}
	return parentedLeafIDs, nil
}

func (s *AMMService) summarizeTopicGroup(ctx context.Context, mergedBody string) (string, string, error) {
	body := strings.TrimSpace(mergedBody)
	tightDesc := extractTightDescription(mergedBody, 100)

	summaryBody, err := s.escalate(ctx, mergedBody, topicBodyMaxChars)
	if err != nil {
		return "", "", fmt.Errorf("summarize topic body: %w", err)
	}
	if strings.TrimSpace(summaryBody) != "" {
		body = summaryBody
	}
	if tight, err := s.escalate(ctx, mergedBody, 100); err == nil {
		if cleaned, ok := sanitizeTightDescription(tight); ok {
			tightDesc = cleaned
		}
	}

	return body, tightDesc, nil
}

func (s *AMMService) escalate(ctx context.Context, text string, maxChars int) (string, error) {
	deterministicMax := s.escalationDeterministicMaxChars
	if deterministicMax <= 0 {
		deterministicMax = defaultEscalationDeterministicMaxChars
	}
	return summarizeWithEscalation(ctx, s.intelligence, text, maxChars, deterministicMax)
}

func summarizeWithEscalation(ctx context.Context, summarizer core.Summarizer, text string, maxChars int, deterministicMax int) (string, error) {
	if text == "" {
		return "", nil
	}

	if summary, err := summarizer.Summarize(ctx, text, maxChars); err == nil && summary != "" && len(summary) < len(text) {
		return summary, nil
	}

	aggressiveMax := maxChars / 2
	if aggressiveMax <= 0 {
		aggressiveMax = 1
	}
	if summary, err := summarizer.Summarize(ctx, text, aggressiveMax); err == nil && summary != "" && len(summary) < len(text) {
		return summary, nil
	}

	truncateLen := len(text)
	if maxChars > 0 && maxChars < truncateLen {
		truncateLen = maxChars
	}
	if deterministicMax > 0 && truncateLen > deterministicMax {
		truncateLen = deterministicMax
	}

	fallback := text[:truncateLen] + fmt.Sprintf(" [Truncated from %d chars]", len(text))
	if len(fallback) >= len(text) {
		if len(text) <= 1 {
			return "", nil
		}
		return fallback[:len(text)-1], nil
	}

	return fallback, nil
}

func groupLeafSummariesByEntities(leafSummaries []core.Summary) [][]core.Summary {
	if len(leafSummaries) == 0 {
		return nil
	}

	entitySets := make([]map[string]struct{}, len(leafSummaries))
	for i, summary := range leafSummaries {
		entities := ExtractEntities(summary.Body)
		set := make(map[string]struct{}, len(entities))
		for _, entity := range entities {
			normalized := strings.ToLower(strings.TrimSpace(entity))
			if normalized == "" {
				continue
			}
			set[normalized] = struct{}{}
		}
		entitySets[i] = set
	}

	adj := make([][]int, len(leafSummaries))
	for i := 0; i < len(leafSummaries); i++ {
		for j := i + 1; j < len(leafSummaries); j++ {
			if sharedEntityCount(entitySets[i], entitySets[j]) < 2 {
				continue
			}
			adj[i] = append(adj[i], j)
			adj[j] = append(adj[j], i)
		}
	}

	visited := make([]bool, len(leafSummaries))
	groups := make([][]core.Summary, 0)
	for i := range leafSummaries {
		if visited[i] {
			continue
		}
		queue := []int{i}
		visited[i] = true
		component := make([]core.Summary, 0)
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			component = append(component, leafSummaries[current])
			for _, next := range adj[current] {
				if visited[next] {
					continue
				}
				visited[next] = true
				queue = append(queue, next)
			}
		}
		if len(component) >= 3 {
			groups = append(groups, component)
		}
	}

	return groups
}

func sharedEntityCount(a, b map[string]struct{}) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	if len(a) > len(b) {
		a, b = b, a
	}
	shared := 0
	for entity := range a {
		if _, ok := b[entity]; ok {
			shared++
		}
	}
	return shared
}

func inferScopeFromSummaries(summaries []core.Summary) (core.Scope, string) {
	if len(summaries) == 0 {
		return core.ScopeGlobal, ""
	}
	projectID := summaries[0].ProjectID
	if projectID == "" || summaries[0].Scope != core.ScopeProject {
		return core.ScopeGlobal, ""
	}
	for _, summary := range summaries[1:] {
		if summary.Scope != core.ScopeProject || summary.ProjectID != projectID {
			return core.ScopeGlobal, ""
		}
	}
	return core.ScopeProject, projectID
}

func (s *AMMService) buildSessionNarrative(
	ctx context.Context,
	eventContents []core.EventContent,
	joinedContent string,
	evts []core.Event,
	existingMemories []core.MemorySummary,
) (string, string, *core.NarrativeResult, bool, string, error) {
	if s.intelligence != nil {
		result, method, err := consolidateNarrativeWithMethod(ctx, s.intelligence, eventContents, existingMemories)
		if err == nil && result != nil {
			body := strings.TrimSpace(result.Summary)
			if body == "" {
				body = joinedContent
			}
			if len(body) > sessionBodyMaxChars {
				body, err = s.escalate(ctx, body, sessionBodyMaxChars)
				if err != nil {
					return "", "", nil, false, method, fmt.Errorf("escalate session body: %w", err)
				}
			}
			tightDesc := fallbackSessionTightDesc(evts)
			if cleaned, ok := sanitizeTightDescription(result.TightDesc); ok {
				tightDesc = cleaned
			}
			return body, tightDesc, result, true, method, nil
		}
	}

	body, err := s.escalate(ctx, joinedContent, sessionBodyMaxChars)
	if err != nil {
		return "", "", nil, false, MethodHeuristic, fmt.Errorf("summarize session body: %w", err)
	}

	tightDesc := fallbackSessionTightDesc(evts)
	if tightResult, err := s.intelligence.Summarize(ctx, body, 100); err == nil {
		if cleaned, ok := sanitizeTightDescription(tightResult); ok {
			tightDesc = cleaned
		}
	}

	return body, tightDesc, nil, false, MethodHeuristic, nil
}

func fallbackSessionTightDesc(evts []core.Event) string {
	snippets := buildTopicSnippets(evts, 3)
	return fmt.Sprintf("Session summary: %d events, topics: %s", len(evts), snippets)
}

func (s *AMMService) insertNarrativeEpisode(
	ctx context.Context,
	result *core.NarrativeResult,
	sessionID string,
	scope core.Scope,
	projectID string,
	eventIDs []string,
	evts []core.Event,
	extractionMethod string,
	retryable bool,
	priorFallbackCount int,
) error {
	if result == nil || result.Episode == nil {
		return nil
	}

	title := strings.TrimSpace(result.Episode.Title)
	if title == "" {
		title = fmt.Sprintf("Session %s", sessionID)
	}
	summary := strings.TrimSpace(result.Episode.Body)
	if summary == "" {
		return nil
	}

	now := time.Now().UTC()
	startedAt, endedAt := eventTimeBounds(evts)
	episode := &core.Episode{
		ID:      core.GenerateID("ep_"),
		Title:   title,
		Summary: summary,
		TightDescription: func() string {
			if td, ok := sanitizeTightDescription(extractTightDescription(summary, 160)); ok {
				return td
			}
			return fallbackSessionTightDesc(evts)
		}(),
		Scope:        scope,
		ProjectID:    projectID,
		SessionID:    sessionID,
		Importance:   0.6,
		PrivacyLevel: core.PrivacyPrivate,
		StartedAt:    startedAt,
		EndedAt:      endedAt,
		SourceSpan: core.SourceSpan{
			EventIDs: eventIDs,
		},
		Participants:    result.Episode.Participants,
		Outcomes:        result.Episode.Outcomes,
		UnresolvedItems: result.Episode.Unresolved,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	episode.Metadata = applyExtractionMetadata(episode.Metadata, extractionMethod, s.extractionModelName(), retryable, priorFallbackCount)

	return s.repo.InsertEpisode(ctx, episode)
}

func eventTimeBounds(events []core.Event) (*time.Time, *time.Time) {
	if len(events) == 0 {
		return nil, nil
	}
	minTime := events[0].OccurredAt
	maxTime := events[0].OccurredAt
	for i := 1; i < len(events); i++ {
		if events[i].OccurredAt.Before(minTime) {
			minTime = events[i].OccurredAt
		}
		if events[i].OccurredAt.After(maxTime) {
			maxTime = events[i].OccurredAt
		}
	}
	return &minTime, &maxTime
}

func (s *AMMService) collectSessionMemoryContext(
	ctx context.Context,
	sessionID string,
	eventIDs []string,
) ([]core.MemorySummary, []*core.Memory, error) {
	allMemories, err := s.repo.ListMemoriesBySourceEventIDs(ctx, eventIDs)
	if err != nil {
		return nil, nil, err
	}

	eventSet := make(map[string]struct{}, len(eventIDs))
	for _, eventID := range eventIDs {
		eventSet[eventID] = struct{}{}
	}

	summaries := make([]core.MemorySummary, 0)
	linked := make([]*core.Memory, 0)
	for i := range allMemories {
		mem := &allMemories[i]
		if !isMemoryLinkedToSession(mem, sessionID, eventSet) {
			continue
		}
		linked = append(linked, mem)
		summaries = append(summaries, core.MemorySummary{
			ID:               mem.ID,
			Type:             string(mem.Type),
			Subject:          mem.Subject,
			TightDescription: mem.TightDescription,
		})
	}

	return summaries, linked, nil
}

func appendMemorySummaries(existing []core.MemorySummary, memories []core.Memory) []core.MemorySummary {
	if len(memories) == 0 {
		return existing
	}
	seen := make(map[string]bool, len(existing)+len(memories))
	summaries := make([]core.MemorySummary, 0, len(existing)+len(memories))
	for _, summary := range existing {
		summaries = append(summaries, summary)
		if summary.ID != "" {
			seen[summary.ID] = true
		}
	}
	for _, mem := range memories {
		if mem.ID != "" && seen[mem.ID] {
			continue
		}
		summaries = append(summaries, core.MemorySummary{
			ID:               mem.ID,
			Type:             string(mem.Type),
			Subject:          mem.Subject,
			TightDescription: mem.TightDescription,
		})
		if mem.ID != "" {
			seen[mem.ID] = true
		}
	}
	return summaries
}

func filterOpenLoopsByID(openLoops []core.Memory, excludeIDs []string) []core.Memory {
	if len(openLoops) == 0 || len(excludeIDs) == 0 {
		return openLoops
	}
	excluded := make(map[string]bool, len(excludeIDs))
	for _, id := range excludeIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		excluded[id] = true
	}
	filtered := make([]core.Memory, 0, len(openLoops))
	for _, mem := range openLoops {
		if excluded[mem.ID] {
			continue
		}
		filtered = append(filtered, mem)
	}
	return filtered
}

func sessionHasRetryableMemories(memories []*core.Memory) bool {
	for _, mem := range memories {
		if shouldRetryHeuristicMemory(mem) {
			return true
		}
	}
	return false
}

func (s *AMMService) archiveResolvedOpenLoops(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC()
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		mem, err := s.repo.GetMemory(ctx, id)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				continue
			}
			return fmt.Errorf("load memory %s: %w", id, err)
		}
		if mem == nil || mem.Type != core.MemoryTypeOpenLoop || mem.Status != core.MemoryStatusActive {
			continue
		}
		mem.Status = core.MemoryStatusArchived
		mem.UpdatedAt = now
		if err := s.repo.UpdateMemory(ctx, mem); err != nil {
			return fmt.Errorf("archive memory %s: %w", id, err)
		}
	}
	return nil
}

func (s *AMMService) listSessionDerivedArtifacts(ctx context.Context, sessionID string) (sessionDerivedArtifacts, error) {
	artifacts := sessionDerivedArtifacts{}
	summaries, err := s.repo.ListSummaries(ctx, core.ListSummariesOptions{
		Kind:      "session",
		SessionID: sessionID,
		Limit:     100,
	})
	if err != nil {
		return artifacts, fmt.Errorf("list session summaries: %w", err)
	}
	for _, summary := range summaries {
		artifacts.summaryIDs = append(artifacts.summaryIDs, summary.ID)
	}

	episodes, err := s.repo.ListEpisodes(ctx, core.ListEpisodesOptions{
		SessionID: sessionID,
		Limit:     100,
	})
	if err != nil {
		return artifacts, fmt.Errorf("list session episodes: %w", err)
	}
	for _, episode := range episodes {
		artifacts.episodeIDs = append(artifacts.episodeIDs, episode.ID)
	}

	return artifacts, nil
}

func (s *AMMService) deleteSessionDerivedArtifacts(ctx context.Context, artifacts sessionDerivedArtifacts) error {
	for _, summaryID := range artifacts.summaryIDs {
		if strings.TrimSpace(summaryID) == "" {
			continue
		}
		if err := s.repo.DeleteSummary(ctx, summaryID); err != nil {
			return fmt.Errorf("delete session summary %s: %w", summaryID, err)
		}
	}
	for _, episodeID := range artifacts.episodeIDs {
		if strings.TrimSpace(episodeID) == "" {
			continue
		}
		if err := s.repo.DeleteEpisode(ctx, episodeID); err != nil {
			return fmt.Errorf("delete session episode %s: %w", episodeID, err)
		}
	}

	return nil
}

func isMemoryLinkedToSession(mem *core.Memory, sessionID string, eventSet map[string]struct{}) bool {
	if mem == nil {
		return false
	}
	if sessionID != "" && mem.SessionID == sessionID {
		return true
	}
	for _, eventID := range mem.SourceEventIDs {
		if _, ok := eventSet[eventID]; ok {
			return true
		}
	}
	return false
}

func (s *AMMService) markMemoriesNarrativeIncluded(ctx context.Context, memories []*core.Memory) error {
	if len(memories) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(memories))
	now := time.Now().UTC()
	for _, mem := range memories {
		if mem == nil || mem.ID == "" {
			continue
		}
		if _, ok := seen[mem.ID]; ok {
			continue
		}
		seen[mem.ID] = struct{}{}
		current, err := s.repo.GetMemory(ctx, mem.ID)
		if err != nil {
			return fmt.Errorf("load memory %s for narrative metadata: %w", mem.ID, err)
		}
		if getProcessingMeta(current, MetaNarrativeIncluded) == "true" {
			continue
		}
		setProcessingMeta(current, MetaNarrativeIncluded, "true")
		current.UpdatedAt = now
		if err := s.repo.UpdateMemory(ctx, current); err != nil {
			return err
		}
	}

	return nil
}

// inferScopeFromEvents returns the scope and project ID based on events.
// If all events share the same project_id, scope is "project"; otherwise "global".
func inferScopeFromEvents(events []core.Event) (core.Scope, string) {
	if len(events) == 0 {
		return core.ScopeGlobal, ""
	}
	projectID := events[0].ProjectID
	if projectID == "" {
		return core.ScopeGlobal, ""
	}
	for _, evt := range events[1:] {
		if evt.ProjectID != projectID {
			return core.ScopeGlobal, ""
		}
	}
	return core.ScopeProject, projectID
}

// recordConsolidateFrontier persists a consolidate_sessions job with the given
// frontier sequence ID so future runs start scanning after it.
func (s *AMMService) recordConsolidateFrontier(ctx context.Context, created int, maxSeq int64) error {
	now := time.Now().UTC()
	job := &core.Job{
		ID:         core.GenerateID("job_"),
		Kind:       "consolidate_sessions",
		Status:     "completed",
		StartedAt:  &now,
		FinishedAt: &now,
		Result: map[string]string{
			"created":         fmt.Sprintf("%d", created),
			"max_sequence_id": fmt.Sprintf("%d", maxSeq),
		},
		CreatedAt: now,
	}
	if err := s.repo.InsertJob(ctx, job); err != nil {
		return fmt.Errorf("record consolidate_sessions job: %w", err)
	}
	return nil
}

// buildExtractionInput combines the narrative summary with any structured
// KeyDecisions/Unresolved fields so the extraction LLM sees everything the
// narrative provider returned.
func buildExtractionInput(result *core.NarrativeResult, openLoops []core.Memory) string {
	if result == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(result.Summary)
	if len(result.KeyDecisions) > 0 {
		b.WriteString("\n\nKey decisions made in this session:\n")
		for _, d := range result.KeyDecisions {
			b.WriteString("- ")
			b.WriteString(d)
			b.WriteByte('\n')
		}
	}
	if len(result.Unresolved) > 0 {
		b.WriteString("\n\nUnresolved items from this session:\n")
		for _, u := range result.Unresolved {
			b.WriteString("- ")
			b.WriteString(u)
			b.WriteByte('\n')
		}
	}
	if len(openLoops) > 0 {
		b.WriteString("\n\nActive open loops from prior sessions (close if resolved, don't re-create):\n")
		for _, ol := range openLoops {
			b.WriteString("- ")
			b.WriteString(ol.TightDescription)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// activeOpenLoopsForScope returns the most recent active open_loop memories
// for the given scope, capped at 10 to avoid bloating the extraction prompt.
func (s *AMMService) activeOpenLoopsForScope(ctx context.Context, scope core.Scope, projectID string) []core.Memory {
	mems, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Type:      core.MemoryTypeOpenLoop,
		Scope:     scope,
		ProjectID: projectID,
		Status:    core.MemoryStatusActive,
		Limit:     10,
	})
	if err != nil {
		return nil
	}
	return mems
}

// estimateTokens provides a rough chars-to-tokens estimate.
func estimateTokens(text string) int {
	return len(text) / 4
}

// chunkSessionIfNeeded checks if the session content exceeds the summarizer's
// context window and applies map-reduce chunking if needed. Returns the
// (possibly reduced) event contents and joined body.
func (s *AMMService) chunkSessionIfNeeded(
	ctx context.Context,
	eventContents []core.EventContent,
	joinedBody string,
	existingMemories []core.MemorySummary,
	sessionID string,
) ([]core.EventContent, string) {
	contextWindow := s.summarizerContextWindow
	if contextWindow <= 0 {
		contextWindow = defaultSummarizerContextWindow
	}

	// Reserve space for prompt template and prior summary.
	promptReserve := 4000
	availableTokens := contextWindow - promptReserve
	if availableTokens <= 0 {
		return eventContents, joinedBody
	}

	totalTokens := estimateTokens(joinedBody)
	if totalTokens <= availableTokens {
		return eventContents, joinedBody
	}

	if s.intelligence == nil {
		return eventContents, joinedBody
	}

	slog.Info("session exceeds context window, chunking",
		"session_id", sessionID,
		"estimated_tokens", totalTokens,
		"context_window", contextWindow,
		"num_events", len(eventContents))

	// Calculate chunk sizing.
	eventsPerChunk := len(eventContents) * availableTokens / totalTokens
	if eventsPerChunk < 5 {
		eventsPerChunk = 5
	}
	overlap := eventsPerChunk / 10
	if overlap < 1 {
		overlap = 1
	}

	// Split into overlapping chunks and summarize each.
	chunkSummaries := make([]string, 0)
	for start := 0; start < len(eventContents); {
		end := start + eventsPerChunk
		if end > len(eventContents) {
			end = len(eventContents)
		}
		chunk := eventContents[start:end]

		// Re-index the chunk for the LLM.
		reindexed := make([]core.EventContent, len(chunk))
		for i, ec := range chunk {
			reindexed[i] = ec
			reindexed[i].Index = i + 1
		}

		result, err := s.intelligence.ConsolidateNarrative(ctx, reindexed, existingMemories)
		if err != nil {
			slog.Warn("chunk summarization failed, falling back to single pass",
				"session_id", sessionID, "error", err)
			return eventContents, joinedBody
		}
		if result != nil && result.Summary != "" {
			chunkSummaries = append(chunkSummaries, result.Summary)
		}

		// Advance past the non-overlapping portion.
		start = end - overlap
		if start <= end-eventsPerChunk {
			start = end // prevent infinite loop on tiny overlaps
		}
		// If we've reached the end, break.
		if end >= len(eventContents) {
			break
		}
	}

	if len(chunkSummaries) == 0 {
		return eventContents, joinedBody
	}

	// Feed chunk summaries back as synthetic events for the final consolidation.
	finalContents := make([]core.EventContent, len(chunkSummaries))
	var finalJoined strings.Builder
	for i, summary := range chunkSummaries {
		finalContents[i] = core.EventContent{
			Index:     i + 1,
			Content:   fmt.Sprintf("[Session chunk %d/%d summary]\n%s", i+1, len(chunkSummaries), summary),
			SessionID: sessionID,
		}
		if i > 0 {
			finalJoined.WriteByte('\n')
		}
		finalJoined.WriteString(summary)
	}

	slog.Info("chunked session into summaries",
		"session_id", sessionID,
		"chunks", len(chunkSummaries),
		"original_events", len(eventContents))

	return finalContents, finalJoined.String()
}

// latestSessionSummary returns the most recently consolidated session summary
// for a given session, or nil if none exists. Uses UpdatedAt (consolidation
// time) rather than CreatedAt (event time) so that incremental bursts with
// out-of-order event timestamps still pick up the latest narrative context.
func (s *AMMService) latestSessionSummary(ctx context.Context, sessionID string) *core.Summary {
	summaries, err := s.repo.ListSummaries(ctx, core.ListSummariesOptions{
		Kind:      "session",
		SessionID: sessionID,
		Limit:     10, // fetch all bursts, pick latest by UpdatedAt
	})
	if err != nil || len(summaries) == 0 {
		return nil
	}
	latest := &summaries[0]
	for i := 1; i < len(summaries); i++ {
		if summaries[i].UpdatedAt.After(latest.UpdatedAt) {
			latest = &summaries[i]
		}
	}
	return latest
}

// markEventsReflected sets reflected_at on events that haven't been reflected yet.
func (s *AMMService) markEventsReflected(ctx context.Context, events []core.Event) error {
	now := time.Now().UTC()
	for i := range events {
		if events[i].ReflectedAt != nil {
			continue
		}
		events[i].ReflectedAt = &now
		if err := s.repo.UpdateEvent(ctx, &events[i]); err != nil {
			return fmt.Errorf("mark event %s reflected: %w", events[i].ID, err)
		}
	}
	return nil
}

func (s *AMMService) clearEventsReflected(ctx context.Context, events []core.Event) error {
	for i := range events {
		if events[i].ReflectedAt == nil {
			continue
		}
		events[i].ReflectedAt = nil
		if err := s.repo.UpdateEvent(ctx, &events[i]); err != nil {
			return fmt.Errorf("clear reflected_at for event %s: %w", events[i].ID, err)
		}
	}
	return nil
}

func applyExtractionMetadata(metadata map[string]string, method, model string, retryable bool, priorFallbackCount int) map[string]string {
	if metadata == nil {
		metadata = make(map[string]string)
	}
	if method == "" {
		method = MethodHeuristic
	}
	metadata[MetaExtractionMethod] = method
	if method == MethodLLM {
		metadata[MetaExtractionQuality] = QualityVerified
		delete(metadata, MetaFallbackCount)
	} else {
		metadata[MetaExtractionQuality] = QualityProvisional
		if retryable {
			metadata[MetaFallbackCount] = strconv.Itoa(priorFallbackCount + 1)
		} else {
			delete(metadata, MetaFallbackCount)
		}
	}
	metadata[MetaExtractedAt] = time.Now().UTC().Format(time.RFC3339)
	if model != "" {
		metadata[MetaExtractedModel] = model
	}
	return metadata
}

func buildTopicSnippets(events []core.Event, n int) string {
	snippets := make([]string, 0, n)
	for i := 0; i < len(events) && len(snippets) < n; i++ {
		s := sanitizeSnippet(events[i].Content)
		if s == "" {
			continue
		}
		if len(s) > 40 {
			s = s[:40] + "..."
		}
		snippets = append(snippets, s)
	}
	return strings.Join(snippets, ", ")
}
