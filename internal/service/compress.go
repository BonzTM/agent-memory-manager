package service

import (
	"context"
	"fmt"
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
	defaultCompressChunkSize              = 10
	defaultCompressMaxEvents              = 200
	defaultCompressBatchSize              = 15
	defaultTopicBatchSize                 = 15
	leafBodyMaxChars                      = 1000
	sessionBodyMaxChars                   = 2000
	topicBodyMaxChars                     = 2000
	defaultEscalationDeterministicMaxChars = 2048
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

// CompressHistory summarizes recent event chunks into leaf summaries and
// returns the number created.
func (s *AMMService) CompressHistory(ctx context.Context) (int, error) {
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
			ID:               generateID("sum_"),
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
		ID:         generateID("job_"),
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

	recordFrontier := func(created int) error {
		maxSeq := maxEventSequenceID(newEvents)
		now := time.Now().UTC()
		job := &core.Job{
			ID:         generateID("job_"),
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
		return s.repo.InsertJob(ctx, job)
	}

	if len(candidateSessionIDs) == 0 {
		if err := recordFrontier(0); err != nil {
			return 0, fmt.Errorf("record consolidate_sessions job: %w", err)
		}
		return 0, nil
	}

	created := 0

	for sessionID := range candidateSessionIDs {
		existing, err := s.repo.ListSummaries(ctx, core.ListSummariesOptions{
			Kind:      "session",
			SessionID: sessionID,
			Limit:     1,
		})
		if err == nil && len(existing) > 0 {
			continue
		}

		evts, err := s.repo.ListEvents(ctx, core.ListEventsOptions{
			SessionID: sessionID,
			Limit:     500,
		})
		if err != nil {
			return created, fmt.Errorf("list session events for consolidate %s: %w", sessionID, err)
		}
		if len(evts) == 0 {
			continue
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

		body, tightDesc, narrativeResult, usedNarrative, err := s.buildSessionNarrative(ctx, eventContents, bodyBuilder.String(), evts, existingMemories)
		if err != nil {
			return created, err
		}

		if usedNarrative {
			if err := s.insertNarrativeEpisode(ctx, narrativeResult, sessionID, scope, projectID, eventIDs, evts); err != nil {
				return created, fmt.Errorf("insert narrative episode: %w", err)
			}

			autoMemories, err := s.insertNarrativeMemories(ctx, narrativeResult, scope, projectID, sessionID, eventIDs)
			if err != nil {
				return created, fmt.Errorf("insert narrative memories: %w", err)
			}
			linkedMemories = append(linkedMemories, autoMemories...)
		}

		now := time.Now().UTC()
		summary := &core.Summary{
			ID:               generateID("sum_"),
			Kind:             "session",
			Depth:            0,
			Scope:            scope,
			ProjectID:        projectID,
			SessionID:        sessionID,
			Title:            fmt.Sprintf("Session %s", sessionID),
			Body:             body,
			TightDescription: tightDesc,
			PrivacyLevel:     core.PrivacyPrivate,
			SourceSpan: core.SourceSpan{
				EventIDs: eventIDs,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}

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

		created++
	}

	if err := recordFrontier(created); err != nil {
		return created, fmt.Errorf("record consolidate_sessions job: %w", err)
	}

	return created, nil
}

func (s *AMMService) BuildTopicSummaries(ctx context.Context) (int, error) {
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
			ID:         generateID("job_"),
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
			ID:               generateID("sum_"),
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
) (string, string, *core.NarrativeResult, bool, error) {
	if s.intelligence != nil {
		result, err := s.intelligence.ConsolidateNarrative(ctx, eventContents, existingMemories)
		if err == nil && result != nil {
			body := strings.TrimSpace(result.Summary)
			if body == "" {
				body = joinedContent
			}
			if len(body) > sessionBodyMaxChars {
				body, err = s.escalate(ctx, body, sessionBodyMaxChars)
				if err != nil {
					return "", "", nil, false, fmt.Errorf("escalate session body: %w", err)
				}
			}
			tightDesc := fallbackSessionTightDesc(evts)
			if cleaned, ok := sanitizeTightDescription(result.TightDesc); ok {
				tightDesc = cleaned
			}
			return body, tightDesc, result, true, nil
		}
	}

	body, err := s.escalate(ctx, joinedContent, sessionBodyMaxChars)
	if err != nil {
		return "", "", nil, false, fmt.Errorf("summarize session body: %w", err)
	}

	tightDesc := fallbackSessionTightDesc(evts)
	if tightResult, err := s.intelligence.Summarize(ctx, body, 100); err == nil {
		if cleaned, ok := sanitizeTightDescription(tightResult); ok {
			tightDesc = cleaned
		}
	}

	return body, tightDesc, nil, false, nil
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
		ID:               generateID("ep_"),
		Title:            title,
		Summary:          summary,
		TightDescription: func() string {
			if td, ok := sanitizeTightDescription(extractTightDescription(summary, 160)); ok {
				return td
			}
			return fallbackSessionTightDesc(evts)
		}(),
		Scope:            scope,
		ProjectID:        projectID,
		SessionID:        sessionID,
		Importance:       0.6,
		PrivacyLevel:     core.PrivacyPrivate,
		StartedAt:        startedAt,
		EndedAt:          endedAt,
		SourceSpan: core.SourceSpan{
			EventIDs: eventIDs,
		},
		Participants:    result.Episode.Participants,
		Outcomes:        result.Episode.Outcomes,
		UnresolvedItems: result.Episode.Unresolved,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

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

func (s *AMMService) insertNarrativeMemories(
	ctx context.Context,
	result *core.NarrativeResult,
	scope core.Scope,
	projectID string,
	sessionID string,
	eventIDs []string,
) ([]*core.Memory, error) {
	if result == nil {
		return nil, nil
	}

	created := make([]*core.Memory, 0, len(result.KeyDecisions)+len(result.Unresolved))
	for _, decision := range result.KeyDecisions {
		mem, err := s.insertNarrativeMemoryIfNotDuplicate(ctx, core.MemoryTypeDecision, decision, scope, projectID, sessionID, eventIDs)
		if err != nil {
			return nil, err
		}
		if mem != nil {
			created = append(created, mem)
		}
	}
	for _, unresolved := range result.Unresolved {
		mem, err := s.insertNarrativeMemoryIfNotDuplicate(ctx, core.MemoryTypeOpenLoop, unresolved, scope, projectID, sessionID, eventIDs)
		if err != nil {
			return nil, err
		}
		if mem != nil {
			created = append(created, mem)
		}
	}

	return created, nil
}

func (s *AMMService) insertNarrativeMemoryIfNotDuplicate(
	ctx context.Context,
	memoryType core.MemoryType,
	body string,
	scope core.Scope,
	projectID string,
	sessionID string,
	eventIDs []string,
) (*core.Memory, error) {
	trimmedBody := strings.TrimSpace(body)
	if trimmedBody == "" {
		return nil, nil
	}

	now := time.Now().UTC()
	candidate := core.Memory{
		Type:             memoryType,
		Scope:            scope,
		ProjectID:        projectID,
		SessionID:        sessionID,
		Body:             trimmedBody,
		TightDescription: func() string {
			td := extractTightDescription(trimmedBody, 120)
			if _, ok := sanitizeTightDescription(td); !ok {
				if len(trimmedBody) > 120 {
					return trimmedBody[:120]
				}
				return trimmedBody
			}
			return td
		}(),
		Confidence:       0.85,
		Importance:       importanceForCandidate(core.MemoryCandidate{Type: memoryType}),
		Status:           core.MemoryStatusActive,
		SourceEventIDs:   eventIDs,
	}

	queryText := narrativeMemorySearchQuery(candidate)
	existing, err := s.repo.SearchMemoriesFuzzy(ctx, queryText, core.ListMemoriesOptions{
		Type:      memoryType,
		Scope:     scope,
		ProjectID: projectID,
		Status:    core.MemoryStatusActive,
		Limit:     100,
	})
	if err != nil {
		return nil, fmt.Errorf("search memories for narrative duplicate detection: %w", err)
	}
	active := make([]*core.Memory, 0, len(existing))
	for i := range existing {
		active = append(active, &existing[i])
	}

	duplicates := findDuplicateActiveMemories(active, candidate)
	if len(duplicates) > 0 {
		return nil, nil
	}

	mem := &core.Memory{
		ID:               generateID("mem_"),
		Type:             candidate.Type,
		Scope:            candidate.Scope,
		ProjectID:        candidate.ProjectID,
		SessionID:        candidate.SessionID,
		Body:             candidate.Body,
		TightDescription: candidate.TightDescription,
		Confidence:       candidate.Confidence,
		Importance:       candidate.Importance,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		SourceEventIDs:   candidate.SourceEventIDs,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	markExtracted(mem, s.extractionMethod(), s.extractionModelName())
	setProcessingMeta(mem, MetaNarrativeIncluded, "true")

	if err := s.repo.InsertMemory(ctx, mem); err != nil {
		return nil, fmt.Errorf("insert narrative memory: %w", err)
	}

	return mem, nil
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
			Type:             string(mem.Type),
			Subject:          mem.Subject,
			TightDescription: mem.TightDescription,
		})
	}

	return summaries, linked, nil
}

func narrativeMemorySearchQuery(candidate core.Memory) string {
	combined := strings.TrimSpace(strings.Join([]string{candidate.Subject, candidate.TightDescription, candidate.Body}, " "))
	if combined == "" {
		return ""
	}
	tokens := strings.Fields(strings.ToLower(combined))
	seen := make(map[string]struct{}, len(tokens))
	terms := make([]string, 0, 12)
	for _, token := range tokens {
		clean := strings.Trim(token, "\"'.,!?;:()[]{}<>|/\\+-=*`")
		if len(clean) < 3 {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		terms = append(terms, clean)
		if len(terms) >= 12 {
			break
		}
	}
	if len(terms) == 0 {
		return combined
	}
	return strings.Join(terms, " ")
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
		if getProcessingMeta(mem, MetaNarrativeIncluded) == "true" {
			continue
		}
		setProcessingMeta(mem, MetaNarrativeIncluded, "true")
		mem.UpdatedAt = now
		if err := s.repo.UpdateMemory(ctx, mem); err != nil {
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
