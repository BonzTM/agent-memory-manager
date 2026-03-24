package service

import (
	"context"
	"fmt"
	"time"

	"github.com/joshd-04/agent-memory-manager/internal/core"
)

// CheckIngestionPolicy checks if an event should be ingested based on matching policies.
// Returns the matching policy mode ("full", "read_only", "ignore") or "full" if no policy matches.
func (s *AMMService) CheckIngestionPolicy(ctx context.Context, event *core.Event) (string, error) {
	// Check policies in priority order: session_id, project_id, agent_id, source_system, surface.
	checks := []struct {
		patternType string
		value       string
	}{
		{"session", event.SessionID},
		{"project", event.ProjectID},
		{"agent", event.AgentID},
		{"source", event.SourceSystem},
		{"surface", event.Surface},
	}

	for _, c := range checks {
		if c.value == "" {
			continue
		}
		policy, err := s.repo.MatchIngestionPolicy(ctx, c.patternType, c.value)
		if err != nil {
			continue // No match for this pattern type; try the next.
		}
		if policy != nil {
			return policy.Mode, nil
		}
	}

	return "full", nil
}

func (s *AMMService) ListPolicies(ctx context.Context) ([]core.IngestionPolicy, error) {
	return s.repo.ListIngestionPolicies(ctx)
}

func (s *AMMService) AddPolicy(ctx context.Context, policy *core.IngestionPolicy) (*core.IngestionPolicy, error) {
	if policy.ID == "" {
		policy.ID = generateID("pol_")
	}
	now := time.Now().UTC()
	if policy.CreatedAt.IsZero() {
		policy.CreatedAt = now
	}
	policy.UpdatedAt = now

	if err := s.repo.InsertIngestionPolicy(ctx, policy); err != nil {
		return nil, fmt.Errorf("insert ingestion policy: %w", err)
	}
	return policy, nil
}

func (s *AMMService) RemovePolicy(ctx context.Context, id string) error {
	if err := s.repo.DeleteIngestionPolicy(ctx, id); err != nil {
		return fmt.Errorf("delete ingestion policy: %w", err)
	}
	return nil
}

// ShouldIngest returns whether the event should be written and whether it should trigger
// memory creation, based on ingestion policy.
// Returns false for "ignore" mode, true for "full" and "read_only" modes.
// For "read_only", events are still stored in history but won't trigger memory creation.
func (s *AMMService) ShouldIngest(ctx context.Context, event *core.Event) (ingest bool, createMemory bool, err error) {
	mode, err := s.CheckIngestionPolicy(ctx, event)
	if err != nil {
		return false, false, err
	}

	switch mode {
	case "ignore":
		return false, false, nil
	case "read_only":
		return true, false, nil
	default: // "full"
		return true, true, nil
	}
}
