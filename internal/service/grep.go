package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func (s *AMMService) Grep(ctx context.Context, pattern string, opts core.GrepOptions) (*core.GrepResult, error) {
	slog.Debug("grep", "pattern", pattern, "session_id", opts.SessionID, "project_id", opts.ProjectID)

	if strings.TrimSpace(pattern) == "" {
		return nil, fmt.Errorf("%w: pattern is required", core.ErrInvalidInput)
	}
	if opts.MaxGroupDepth < 0 {
		opts.MaxGroupDepth = 0
	}
	if opts.GroupLimit <= 0 {
		opts.GroupLimit = 10
	}
	if opts.MatchesPerGroup <= 0 {
		opts.MatchesPerGroup = 5
	}

	searchLimit := opts.GroupLimit * opts.MatchesPerGroup * 2
	if searchLimit <= 0 {
		searchLimit = 100
	}
	hasScopeFilter := opts.SessionID != "" || opts.ProjectID != ""
	if hasScopeFilter {
		// Repository SearchEvents does not currently accept session/project filters.
		// Over-fetch to reduce false negatives after in-memory scope filtering.
		searchLimit *= 5
	}

	events, err := s.repo.SearchEvents(ctx, pattern, searchLimit)
	if err != nil {
		return nil, fmt.Errorf("search events: %w", err)
	}

	filtered := make([]core.Event, 0, len(events))
	for _, evt := range events {
		if opts.SessionID != "" && evt.SessionID != opts.SessionID {
			continue
		}
		if opts.ProjectID != "" && evt.ProjectID != opts.ProjectID {
			continue
		}
		filtered = append(filtered, evt)
	}

	type groupAccumulator struct {
		group   core.GrepGroup
		hitSeen int
	}

	groups := make(map[string]*groupAccumulator)
	groupOrder := make([]string, 0)

	for _, evt := range filtered {
		summary, err := s.findCoveringSummary(ctx, evt.ID, opts.MaxGroupDepth)
		if err != nil {
			return nil, fmt.Errorf("find covering summary for event %s: %w", evt.ID, err)
		}

		groupKey := ""
		groupSummaryID := ""
		groupSummaryText := ""
		if summary != nil {
			groupKey = summary.ID
			groupSummaryID = summary.ID
			groupSummaryText = summary.Body
		} else {
			groupKey = "__ungrouped__"
		}

		acc, ok := groups[groupKey]
		if !ok {
			acc = &groupAccumulator{group: core.GrepGroup{Summary: summary, SummaryID: groupSummaryID, SummaryText: groupSummaryText, Matches: make([]core.GrepMatch, 0, opts.MatchesPerGroup)}}
			groups[groupKey] = acc
			groupOrder = append(groupOrder, groupKey)
		}

		acc.hitSeen++
		if len(acc.group.Matches) < opts.MatchesPerGroup {
			match := core.GrepMatch{EventID: evt.ID, Kind: evt.Kind, Content: evt.Content}
			if !evt.OccurredAt.IsZero() {
				match.OccurredAt = evt.OccurredAt.Format(time.RFC3339)
			}
			acc.group.Matches = append(acc.group.Matches, match)
		}
	}

	sampleLimited := len(events) >= searchLimit
	result := &core.GrepResult{Pattern: pattern, TotalHits: len(filtered), SampleLimited: sampleLimited, Groups: make([]core.GrepGroup, 0, opts.GroupLimit)}
	for _, key := range groupOrder {
		if len(result.Groups) >= opts.GroupLimit {
			break
		}
		result.Groups = append(result.Groups, groups[key].group)
	}

	return result, nil
}
