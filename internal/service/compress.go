package service

import (
	"context"
	"fmt"
	"time"

	"github.com/joshd-04/agent-memory-manager/internal/core"
)

const (
	compressChunkSize     = 10
	compressMaxEvents     = 200
	leafBodyMaxChars      = 1000
	sessionBodyMaxChars   = 2000
)

// CompressHistory creates leaf summaries over recent event spans.
// Returns the number of summaries created.
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
		var bodyBuilder []byte
		for _, evt := range chunk {
			eventIDs = append(eventIDs, evt.ID)
			if len(bodyBuilder) > 0 {
				bodyBuilder = append(bodyBuilder, '\n')
			}
			bodyBuilder = append(bodyBuilder, evt.Content...)
			if len(bodyBuilder) > leafBodyMaxChars {
				bodyBuilder = bodyBuilder[:leafBodyMaxChars]
				break
			}
		}

		// Determine scope.
		scope, projectID := inferScopeFromEvents(chunk)

		firstTime := chunk[0].OccurredAt.Format(time.RFC3339)
		lastTime := chunk[len(chunk)-1].OccurredAt.Format(time.RFC3339)

		now := time.Now().UTC()
		summary := &core.Summary{
			ID:    generateID("sum_"),
			Kind:  "leaf",
			Scope: scope,
			ProjectID:    projectID,
			Title:            fmt.Sprintf("Events %s to %s", firstTime, lastTime),
			Body:             string(bodyBuilder),
			TightDescription: fmt.Sprintf("Summary of %d events from %s to %s", len(chunk), firstTime, lastTime),
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

// ConsolidateSessions creates session-level summaries from events grouped by session_id.
// Returns the number of session summaries created.
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
		var bodyBuilder []byte
		for _, evt := range evts {
			eventIDs = append(eventIDs, evt.ID)
			if len(bodyBuilder) > 0 {
				bodyBuilder = append(bodyBuilder, '\n')
			}
			bodyBuilder = append(bodyBuilder, evt.Content...)
			if len(bodyBuilder) > sessionBodyMaxChars {
				bodyBuilder = bodyBuilder[:sessionBodyMaxChars]
				break
			}
		}

		// Build topic snippets for tight description.
		snippets := buildTopicSnippets(evts, 3)
		scope, projectID := inferScopeFromEvents(evts)

		now := time.Now().UTC()
		summary := &core.Summary{
			ID:               generateID("sum_"),
			Kind:             "session",
			Scope:            scope,
			ProjectID:        projectID,
			SessionID:        sessionID,
			Title:            fmt.Sprintf("Session %s", sessionID),
			Body:             string(bodyBuilder),
			TightDescription: fmt.Sprintf("Session summary: %d events, topics: %s", len(evts), snippets),
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

		created++
	}

	return created, nil
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
