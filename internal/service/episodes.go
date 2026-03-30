package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

// FormEpisodes groups session events into episode records and returns the
// number created.
func (s *AMMService) FormEpisodes(ctx context.Context) (int, error) {
	slog.Debug("FormEpisodes called")
	// List recent events grouped by session_id.
	events, err := s.repo.ListEvents(ctx, core.ListEventsOptions{
		Limit: 500,
	})
	if err != nil {
		return 0, fmt.Errorf("list events for episode formation: %w", err)
	}

	// Group by session_id (skip events without a session).
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
		// Check if an episode already exists for this session.
		// Use quoted FTS query for exact matching, then verify the session_id field
		// to avoid false positives from partial FTS matches.
		existing, err := s.repo.SearchEpisodes(ctx, fmt.Sprintf("%q", sessionID), 10)
		if err == nil && len(existing) > 0 {
			skip := false
			for _, ep := range existing {
				if ep.SessionID == sessionID {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
		}

		// Build summary from first 5 event contents, truncated to 500 chars total.
		summary := buildEpisodeSummary(evts, 5, 500)

		// Build topic snippets for the tight description.
		snippets := buildTopicSnippets(evts, 3)

		// Collect event IDs.
		eventIDs := make([]string, 0, len(evts))
		for _, evt := range evts {
			eventIDs = append(eventIDs, evt.ID)
		}

		// Collect unique participants (actor_ids).
		participantSet := make(map[string]bool)
		for _, evt := range evts {
			if evt.ActorID != "" {
				participantSet[evt.ActorID] = true
			}
		}
		participants := make([]string, 0, len(participantSet))
		for p := range participantSet {
			participants = append(participants, p)
		}

		// Extract related entities from combined content.
		var combinedContent strings.Builder
		for _, evt := range evts {
			if combinedContent.Len() > 0 {
				combinedContent.WriteString(" ")
			}
			combinedContent.WriteString(evt.Content)
		}
		relatedEntities := ExtractEntities(combinedContent.String())

		// Determine scope.
		scope, projectID := inferScopeFromEvents(evts)

		// Timestamps: events come back from ListEvents in DESC order (newest first),
		// so evts[0] is the newest and evts[len-1] is the oldest.
		startedAt := evts[len(evts)-1].OccurredAt // oldest
		endedAt := evts[0].OccurredAt             // newest

		episode := &core.Episode{
		ID:      core.GenerateID("ep_"),
			Title:   fmt.Sprintf("Session %s", sessionID),
			Summary: summary,
			TightDescription: fmt.Sprintf("Episode from session %s: %d events covering %s",
				sessionID, len(evts), snippets),
			Scope:        scope,
			ProjectID:    projectID,
			SessionID:    sessionID,
			Importance:   0.5,
			PrivacyLevel: core.PrivacyPrivate,
			StartedAt:    &startedAt,
			EndedAt:      &endedAt,
			SourceSpan: core.SourceSpan{
				EventIDs: eventIDs,
			},
			Participants:    participants,
			RelatedEntities: relatedEntities,
		}

		if err := s.repo.InsertEpisode(ctx, episode); err != nil {
			return created, fmt.Errorf("insert episode: %w", err)
		}
		created++
	}

	return created, nil
}

// buildEpisodeSummary concatenates the first n event contents, truncated to maxChars total.
func buildEpisodeSummary(events []core.Event, n int, maxChars int) string {
	if n > len(events) {
		n = len(events)
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(events[i].Content)
		if b.Len() >= maxChars {
			return b.String()[:maxChars]
		}
	}
	return b.String()
}
