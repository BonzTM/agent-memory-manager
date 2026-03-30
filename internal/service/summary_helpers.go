package service

import (
	"context"
	"fmt"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func (s *AMMService) findCoveringSummary(ctx context.Context, eventID string, maxDepth int) (*core.Summary, error) {
	if maxDepth < 0 {
		maxDepth = 0
	}

	parents, err := s.repo.GetSummaryParents(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("get summary parents for event: %w", err)
	}
	if len(parents) == 0 {
		return nil, nil
	}

	currentSummaryID := parents[0].ParentSummaryID
	current, err := s.repo.GetSummary(ctx, currentSummaryID)
	if err != nil {
		return nil, fmt.Errorf("get covering summary: %w", err)
	}

	visited := map[string]bool{current.ID: true}
	for depth := 0; depth < maxDepth; depth++ {
		ancestorEdges, err := s.repo.GetSummaryParents(ctx, current.ID)
		if err != nil {
			return nil, fmt.Errorf("get summary parent while walking depth: %w", err)
		}
		if len(ancestorEdges) == 0 {
			break
		}

		nextID := ancestorEdges[0].ParentSummaryID
		if visited[nextID] {
			break
		}
		next, err := s.repo.GetSummary(ctx, nextID)
		if err != nil {
			return nil, fmt.Errorf("get ancestor summary: %w", err)
		}
		current = next
		visited[nextID] = true
	}

	return current, nil
}
