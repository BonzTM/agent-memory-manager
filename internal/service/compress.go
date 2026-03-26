package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

const (
	compressChunkSize   = 10
	compressMaxEvents   = 200
	leafBodyMaxChars    = 1000
	sessionBodyMaxChars = 2000
	topicBodyMaxChars   = 2000
)

// CompressHistory summarizes recent event chunks into leaf summaries and
// returns the number created.
func (s *AMMService) CompressHistory(ctx context.Context) (int, error) {
	// Determine watermark from last compress job.
	var afterTime string
	jobs, err := s.repo.ListJobs(ctx, core.ListJobsOptions{
		Kind:   "compress",
		Status: "completed",
		Limit:  1,
	})
	if err == nil && len(jobs) > 0 && jobs[0].FinishedAt != nil {
		afterTime = jobs[0].FinishedAt.Format(time.RFC3339Nano)
	}

	// List events to compress.
	var events []core.Event
	if afterTime != "" {
		events, err = s.repo.ListEvents(ctx, core.ListEventsOptions{
			After: afterTime,
			Limit: compressMaxEvents,
		})
	} else {
		events, err = s.repo.ListEvents(ctx, core.ListEventsOptions{
			Limit: compressMaxEvents,
		})
	}
	if err != nil {
		return 0, fmt.Errorf("list events for compress: %w", err)
	}

	if len(events) == 0 {
		return 0, nil
	}

	created := 0

	// Process events in chunks.
	for i := 0; i < len(events); i += compressChunkSize {
		end := i + compressChunkSize
		if end > len(events) {
			end = len(events)
		}
		chunk := events[i:end]

		if len(chunk) == 0 {
			continue
		}

		// Collect event IDs and build body.
		eventIDs := make([]string, 0, len(chunk))
		var bodyBuilder strings.Builder
		for _, evt := range chunk {
			eventIDs = append(eventIDs, evt.ID)
			if bodyBuilder.Len() > 0 {
				bodyBuilder.WriteByte('\n')
			}
			bodyBuilder.WriteString(evt.Content)
		}

		body, err := s.summarizer.Summarize(ctx, bodyBuilder.String(), leafBodyMaxChars)
		if err != nil {
			return created, fmt.Errorf("summarize leaf body: %w", err)
		}

		// Determine scope.
		scope, projectID := inferScopeFromEvents(chunk)

		firstTime := chunk[0].OccurredAt.Format(time.RFC3339)
		lastTime := chunk[len(chunk)-1].OccurredAt.Format(time.RFC3339)
		tightDesc := fmt.Sprintf("Summary of %d events from %s to %s", len(chunk), firstTime, lastTime)
		if tightResult, err := s.summarizer.Summarize(ctx, body, 100); err == nil && strings.TrimSpace(tightResult) != "" {
			tightDesc = tightResult
		}

		now := time.Now().UTC()
		summary := &core.Summary{
			ID:               generateID("sum_"),
			Kind:             "leaf",
			Scope:            scope,
			ProjectID:        projectID,
			Title:            fmt.Sprintf("Events %s to %s", firstTime, lastTime),
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
			return created, fmt.Errorf("insert leaf summary: %w", err)
		}
		s.upsertSummaryEmbeddingBestEffort(ctx, summary)

		// Create edges linking summary to each event.
		for order, eid := range eventIDs {
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

	// Record job for watermarking.
	now := time.Now().UTC()
	job := &core.Job{
		ID:         generateID("job_"),
		Kind:       "compress",
		Status:     "completed",
		StartedAt:  &now,
		FinishedAt: &now,
		Result:     map[string]string{"created": fmt.Sprintf("%d", created)},
		CreatedAt:  now,
	}
	if err := s.repo.InsertJob(ctx, job); err != nil {
		return created, fmt.Errorf("record compress job: %w", err)
	}

	return created, nil
}

// ConsolidateSessions creates session-level summaries from grouped session
// events and returns the number created.
func (s *AMMService) ConsolidateSessions(ctx context.Context) (int, error) {
	// List recent events (up to 500) and group by session_id.
	events, err := s.repo.ListEvents(ctx, core.ListEventsOptions{
		Limit: 500,
	})
	if err != nil {
		return 0, fmt.Errorf("list events for consolidate: %w", err)
	}

	// Group by session_id.
	sessionEvents := make(map[string][]core.Event)
	for _, evt := range events {
		if evt.SessionID == "" {
			continue
		}
		sessionEvents[evt.SessionID] = append(sessionEvents[evt.SessionID], evt)
	}

	if len(sessionEvents) == 0 {
		return 0, nil
	}

	created := 0

	for sessionID, evts := range sessionEvents {
		// Check if a session summary already exists.
		existing, err := s.repo.ListSummaries(ctx, core.ListSummariesOptions{
			Kind:      "session",
			SessionID: sessionID,
			Limit:     1,
		})
		if err == nil && len(existing) > 0 {
			continue
		}

		// Collect event IDs and build body.
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
		s.upsertSummaryEmbeddingBestEffort(ctx, summary)

		// Create edges.
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

	return created, nil
}

func (s *AMMService) BuildTopicSummaries(ctx context.Context) (int, error) {
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
		return 0, nil
	}

	created := 0
	for _, group := range groups {
		if len(group) < 3 {
			continue
		}

		childIDs := make([]string, 0, len(group))
		var mergedBodyBuilder strings.Builder
		for i, summary := range group {
			childIDs = append(childIDs, summary.ID)
			if i > 0 {
				mergedBodyBuilder.WriteString("\n\n")
			}
			mergedBodyBuilder.WriteString(summary.Body)
		}
		mergedBody := mergedBodyBuilder.String()

		body, tightDesc, err := s.summarizeTopicGroup(ctx, mergedBody)
		if err != nil {
			return created, err
		}

		scope, projectID := inferScopeFromSummaries(group)
		now := time.Now().UTC()
		topicSummary := &core.Summary{
			ID:               generateID("sum_"),
			Kind:             "topic",
			Scope:            scope,
			ProjectID:        projectID,
			Title:            fmt.Sprintf("Topic summary over %d leaf summaries", len(group)),
			Body:             body,
			TightDescription: tightDesc,
			PrivacyLevel:     core.PrivacyPrivate,
			SourceSpan: core.SourceSpan{
				SummaryIDs: childIDs,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := s.repo.InsertSummary(ctx, topicSummary); err != nil {
			return created, fmt.Errorf("insert topic summary: %w", err)
		}
		s.upsertSummaryEmbeddingBestEffort(ctx, topicSummary)

		for order, childID := range childIDs {
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

	return created, nil
}

func (s *AMMService) collectParentedLeafSummaryIDs(ctx context.Context) (map[string]struct{}, error) {
	allSummaries, err := s.repo.ListSummaries(ctx, core.ListSummariesOptions{Limit: 50000})
	if err != nil {
		return nil, fmt.Errorf("list summaries for parent linkage: %w", err)
	}

	parentedLeafIDs := make(map[string]struct{})
	for _, summary := range allSummaries {
		edges, err := s.repo.GetSummaryChildren(ctx, summary.ID)
		if err != nil {
			return nil, fmt.Errorf("list summary children for %s: %w", summary.ID, err)
		}
		for _, edge := range edges {
			if edge.ChildKind == "summary" && strings.HasPrefix(edge.ChildID, "sum_") {
				parentedLeafIDs[edge.ChildID] = struct{}{}
			}
		}
	}

	return parentedLeafIDs, nil
}

func (s *AMMService) summarizeTopicGroup(ctx context.Context, mergedBody string) (string, string, error) {
	body := strings.TrimSpace(mergedBody)
	tightDesc := extractTightDescription(mergedBody, 100)

	if s.intelligence != nil {
		summaryBody, err := s.intelligence.Summarize(ctx, mergedBody, topicBodyMaxChars)
		if err == nil && strings.TrimSpace(summaryBody) != "" {
			body = summaryBody
			tight, tightErr := s.intelligence.Summarize(ctx, mergedBody, 100)
			if tightErr == nil && strings.TrimSpace(tight) != "" {
				tightDesc = tight
			}
			return body, tightDesc, nil
		}
	}

	summaryBody, err := s.summarizer.Summarize(ctx, mergedBody, topicBodyMaxChars)
	if err != nil {
		return "", "", fmt.Errorf("summarize topic body: %w", err)
	}
	if strings.TrimSpace(summaryBody) != "" {
		body = summaryBody
	}
	tight, err := s.summarizer.Summarize(ctx, mergedBody, 100)
	if err == nil && strings.TrimSpace(tight) != "" {
		tightDesc = tight
	}

	return body, tightDesc, nil
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
			tightDesc := strings.TrimSpace(result.TightDesc)
			if tightDesc == "" {
				tightDesc = fallbackSessionTightDesc(evts)
			}
			return body, tightDesc, result, true, nil
		}
	}

	body, err := s.summarizer.Summarize(ctx, joinedContent, sessionBodyMaxChars)
	if err != nil {
		return "", "", nil, false, fmt.Errorf("summarize session body: %w", err)
	}

	tightDesc := fallbackSessionTightDesc(evts)
	if tightResult, err := s.summarizer.Summarize(ctx, body, 100); err == nil && strings.TrimSpace(tightResult) != "" {
		tightDesc = tightResult
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
		TightDescription: extractTightDescription(summary, 160),
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
		TightDescription: extractTightDescription(trimmedBody, 120),
		Confidence:       0.85,
		Importance:       importanceForCandidate(core.MemoryCandidate{Type: memoryType}),
		Status:           core.MemoryStatusActive,
		SourceEventIDs:   eventIDs,
	}

	existing, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Type:      memoryType,
		Scope:     scope,
		ProjectID: projectID,
		Status:    core.MemoryStatusActive,
		Limit:     50000,
	})
	if err != nil {
		return nil, fmt.Errorf("list memories for narrative duplicate detection: %w", err)
	}
	active := make([]*core.Memory, 0, len(existing))
	for i := range existing {
		active = append(active, &existing[i])
	}

	duplicates := findDuplicateActiveMemories(active, candidate)
	if len(duplicates) == 0 {
		duplicates = s.findDuplicatesByEmbedding(ctx, candidate, active)
	}
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
	s.upsertMemoryEmbeddingBestEffort(ctx, mem)

	return mem, nil
}

func (s *AMMService) collectSessionMemoryContext(
	ctx context.Context,
	sessionID string,
	eventIDs []string,
) ([]core.MemorySummary, []*core.Memory, error) {
	allMemories, err := s.repo.ListMemories(ctx, core.ListMemoriesOptions{Limit: 50000})
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

// buildTopicSnippets extracts the first n content snippets from events.
func buildTopicSnippets(events []core.Event, n int) string {
	if n > len(events) {
		n = len(events)
	}
	snippets := make([]string, 0, n)
	for i := 0; i < n; i++ {
		s := events[i].Content
		if len(s) > 40 {
			s = s[:40] + "..."
		}
		snippets = append(snippets, s)
	}
	result := ""
	for i, s := range snippets {
		if i > 0 {
			result += ", "
		}
		result += s
	}
	return result
}
