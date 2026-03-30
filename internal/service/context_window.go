package service

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"sort"
	"strings"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func (s *AMMService) FormatContextWindow(ctx context.Context, opts core.FormatContextWindowOptions) (*core.ContextWindowResult, error) {
	slog.Debug("format context window", "session_id", opts.SessionID, "project_id", opts.ProjectID, "fresh_tail", opts.FreshTailCount)

	if strings.TrimSpace(opts.SessionID) == "" && strings.TrimSpace(opts.ProjectID) == "" {
		return nil, fmt.Errorf("%w: session_id or project_id is required", core.ErrInvalidInput)
	}
	if opts.FreshTailCount <= 0 {
		opts.FreshTailCount = 32
	}

	events, err := s.repo.ListEvents(ctx, core.ListEventsOptions{
		SessionID: opts.SessionID,
		ProjectID: opts.ProjectID,
		Limit:     opts.FreshTailCount * 2,
	})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}

	freshEnd := opts.FreshTailCount
	if freshEnd > len(events) {
		freshEnd = len(events)
	}
	freshEvents := append([]core.Event(nil), events[:freshEnd]...)
	sort.SliceStable(freshEvents, func(i, j int) bool {
		if freshEvents[i].OccurredAt.Equal(freshEvents[j].OccurredAt) {
			if freshEvents[i].SequenceID != freshEvents[j].SequenceID {
				return freshEvents[i].SequenceID < freshEvents[j].SequenceID
			}
			return freshEvents[i].ID < freshEvents[j].ID
		}
		return freshEvents[i].OccurredAt.Before(freshEvents[j].OccurredAt)
	})

	summaries, err := s.repo.ListSummaries(ctx, core.ListSummariesOptions{
		SessionID: opts.SessionID,
		ProjectID: opts.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("list summaries: %w", err)
	}

	sort.SliceStable(summaries, func(i, j int) bool {
		if summaries[i].CreatedAt.Equal(summaries[j].CreatedAt) {
			return summaries[i].ID < summaries[j].ID
		}
		return summaries[i].CreatedAt.Before(summaries[j].CreatedAt)
	})

	filteredSummaries := make([]core.Summary, 0, len(summaries))
	for _, summary := range summaries {
		if opts.MaxSummaryDepth > 0 && summary.Depth > opts.MaxSummaryDepth {
			continue
		}
		filteredSummaries = append(filteredSummaries, summary)
	}

	manifest := make([]core.ContextWindowManifestEntry, 0, len(filteredSummaries)+len(freshEvents))
	lines := make([]string, 0, len(filteredSummaries)+len(freshEvents))

	for _, summary := range filteredSummaries {
		openTag := fmt.Sprintf(
			`<summary id="%s" depth="%s"`,
			html.EscapeString(summary.ID),
			html.EscapeString(fmt.Sprintf("%d", summary.Depth)),
		)
		if opts.IncludeParentRefs {
			parentEdges, perr := s.repo.GetSummaryParents(ctx, summary.ID)
			if perr != nil {
				return nil, fmt.Errorf("get summary parents for %s: %w", summary.ID, perr)
			}
			if len(parentEdges) > 0 {
				parentIDs := make([]string, 0, len(parentEdges))
				for _, edge := range parentEdges {
					parentIDs = append(parentIDs, edge.ParentSummaryID)
				}
				sort.Strings(parentIDs)
				openTag += fmt.Sprintf(` parent_refs="%s"`, html.EscapeString(strings.Join(parentIDs, ",")))
			}
		}
		lines = append(lines, fmt.Sprintf(`%s>%s</summary>`, openTag, html.EscapeString(summary.Body)))
		manifest = append(manifest, core.ContextWindowManifestEntry{
			ID:        summary.ID,
			Kind:      "summary",
			StableRef: "summary:" + summary.ID,
			Depth:     summary.Depth,
		})
	}

	for _, event := range freshEvents {
		lines = append(lines, fmt.Sprintf(
			`<event id="%s" kind="%s">%s</event>`,
			html.EscapeString(event.ID),
			html.EscapeString(event.Kind),
			html.EscapeString(event.Content),
		))
		manifest = append(manifest, core.ContextWindowManifestEntry{
			ID:        event.ID,
			Kind:      "event",
			StableRef: "event:" + event.ID,
		})
	}

	content := strings.Join(lines, "\n")
	result := &core.ContextWindowResult{
		Content:      content,
		SummaryCount: len(filteredSummaries),
		FreshCount:   len(freshEvents),
		EstTokens:    len(content) / 4,
		Manifest:     manifest,
	}
	return result, nil
}
