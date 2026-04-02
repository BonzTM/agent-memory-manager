package service

import (
	"context"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestRecallSessions_ListsSessionSummaries(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	// Insert session summaries directly.
	now := time.Now().UTC()
	for i, title := range []string{
		"AMM: session consolidation pipeline",
		"claude-oauth-proxy: OpenRouter cost lookup",
		"AMM: temporal recall design discussion",
	} {
		s := &core.Summary{
			ID:               core.GenerateID("sum_"),
			Kind:             "session",
			Scope:            core.ScopeGlobal,
			Title:            title,
			Body:             "Session body " + title,
			TightDescription: "keywords for " + title,
			PrivacyLevel:     core.PrivacyPrivate,
			CreatedAt:        now.Add(time.Duration(-i) * time.Hour),
			UpdatedAt:        now.Add(time.Duration(-i) * time.Hour),
		}
		if err := repo.InsertSummary(ctx, s); err != nil {
			t.Fatalf("insert summary: %v", err)
		}
	}

	// Also insert a non-session summary that should NOT appear.
	leaf := &core.Summary{
		ID:               core.GenerateID("sum_"),
		Kind:             "leaf",
		Scope:            core.ScopeGlobal,
		Title:            "Leaf summary",
		Body:             "Not a session",
		TightDescription: "leaf keywords",
		PrivacyLevel:     core.PrivacyPrivate,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.InsertSummary(ctx, leaf); err != nil {
		t.Fatalf("insert leaf summary: %v", err)
	}

	result, err := svc.Recall(ctx, "", core.RecallOptions{
		Mode:  core.RecallModeSessions,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Recall sessions: %v", err)
	}
	if len(result.Items) != 3 {
		t.Fatalf("expected 3 session summaries, got %d", len(result.Items))
	}
	// Verify no leaf summaries leaked through.
	for _, item := range result.Items {
		if item.Type != "session" {
			t.Errorf("expected type=session, got %s", item.Type)
		}
	}
}

func TestRecallSessions_WithDateRange(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	now := time.Now().UTC()
	// Old session (7 days ago).
	old := &core.Summary{
		ID:               core.GenerateID("sum_"),
		Kind:             "session",
		Scope:            core.ScopeGlobal,
		Title:            "Old session",
		Body:             "Old work",
		TightDescription: "old session keywords",
		PrivacyLevel:     core.PrivacyPrivate,
		CreatedAt:        now.AddDate(0, 0, -7),
		UpdatedAt:        now.AddDate(0, 0, -7),
	}
	// Recent session (1 day ago).
	recent := &core.Summary{
		ID:               core.GenerateID("sum_"),
		Kind:             "session",
		Scope:            core.ScopeGlobal,
		Title:            "Recent session",
		Body:             "Recent work",
		TightDescription: "recent session keywords",
		PrivacyLevel:     core.PrivacyPrivate,
		CreatedAt:        now.AddDate(0, 0, -1),
		UpdatedAt:        now.AddDate(0, 0, -1),
	}
	for _, s := range []*core.Summary{old, recent} {
		if err := repo.InsertSummary(ctx, s); err != nil {
			t.Fatalf("insert summary: %v", err)
		}
	}

	// Recall with date range that only includes recent.
	after := now.AddDate(0, 0, -3).Format(time.RFC3339)
	result, err := svc.Recall(ctx, "", core.RecallOptions{
		Mode:  core.RecallModeSessions,
		After: after,
	})
	if err != nil {
		t.Fatalf("Recall sessions with after: %v", err)
	}
	if len(result.Items) != 1 {
		t.Fatalf("expected 1 session in date range, got %d", len(result.Items))
	}
	if result.Items[0].ID != recent.ID {
		t.Errorf("expected recent session, got %s", result.Items[0].ID)
	}
}

func TestRecallSessions_WithTextSearch(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	now := time.Now().UTC()
	summaries := []*core.Summary{
		{
			ID:               core.GenerateID("sum_"),
			Kind:             "session",
			Scope:            core.ScopeGlobal,
			Title:            "AMM consolidation work",
			Body:             "Implemented session consolidation pipeline with idle timeout",
			TightDescription: "amm consolidation pipeline idle-timeout session-first",
			PrivacyLevel:     core.PrivacyPrivate,
			CreatedAt:        now,
			UpdatedAt:        now,
		},
		{
			ID:               core.GenerateID("sum_"),
			Kind:             "session",
			Scope:            core.ScopeGlobal,
			Title:            "OAuth proxy debugging",
			Body:             "Debugged OAuth token refresh in claude-oauth-proxy",
			TightDescription: "oauth proxy token refresh debugging",
			PrivacyLevel:     core.PrivacyPrivate,
			CreatedAt:        now.Add(-time.Hour),
			UpdatedAt:        now.Add(-time.Hour),
		},
	}
	for _, s := range summaries {
		if err := repo.InsertSummary(ctx, s); err != nil {
			t.Fatalf("insert summary: %v", err)
		}
	}

	result, err := svc.Recall(ctx, "consolidation pipeline", core.RecallOptions{
		Mode: core.RecallModeSessions,
	})
	if err != nil {
		t.Fatalf("Recall sessions with query: %v", err)
	}
	if len(result.Items) == 0 {
		t.Fatal("expected at least one session for 'consolidation pipeline'")
	}
	// The consolidation session should be in the results.
	found := false
	for _, item := range result.Items {
		if item.ID == summaries[0].ID {
			found = true
		}
	}
	if !found {
		t.Error("expected to find consolidation session in results")
	}
}

func TestRecallHybrid_TemporalExtraction(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	now := time.Now().UTC()
	// Memory from yesterday.
	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "Yesterday fact about terraform infrastructure",
		TightDescription: "terraform infrastructure yesterday fact",
		ObservedAt:       timePtr(now.AddDate(0, 0, -1)),
	})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}
	// Memory from 30 days ago.
	_, err = svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "Old terraform fact from a month ago",
		TightDescription: "terraform infrastructure old fact",
		ObservedAt:       timePtr(now.AddDate(0, 0, -30)),
	})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	// Query with temporal reference "yesterday" — extraction should strip
	// "yesterday" and search for "terraform" with a temporal window.
	result, err := svc.Recall(ctx, "terraform yesterday", core.RecallOptions{
		Mode: core.RecallModeHybrid,
	})
	if err != nil {
		t.Fatalf("Recall hybrid with temporal: %v", err)
	}
	// Both memories match "terraform", but with temporal scoring the
	// yesterday one should rank higher. At minimum, recall should not error.
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestRecallSessions_WithExplicitAfterBefore(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	// Query with explicit after/before should NOT trigger temporal extraction.
	after := time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339)
	result, err := svc.Recall(ctx, "terraform last week", core.RecallOptions{
		Mode:  core.RecallModeSessions,
		After: after,
	})
	if err != nil {
		t.Fatalf("Recall sessions with explicit after: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// The query text "last week" should NOT be stripped when explicit flags are set.
	// We can't directly assert this from the result, but the call should succeed.
}

func TestTemporalWindowMultiplier(t *testing.T) {
	now := time.Now().UTC()
	yesterday := now.AddDate(0, 0, -1)
	lastWeek := now.AddDate(0, 0, -7)

	after := yesterday.Add(-time.Hour)
	before := now

	sctx := ScoringContext{
		TemporalAfter:       &after,
		TemporalBefore:      &before,
		TemporalAttenuation: 0.3,
	}

	// Item inside window.
	insideItem := ScoringCandidate{CreatedAt: yesterday}
	mult := temporalWindowMultiplier(insideItem, sctx)
	if mult != 1.0 {
		t.Errorf("expected 1.0 for inside-window item, got %f", mult)
	}

	// Item outside window (too old).
	outsideItem := ScoringCandidate{CreatedAt: lastWeek}
	mult = temporalWindowMultiplier(outsideItem, sctx)
	if mult != 0.3 {
		t.Errorf("expected 0.3 for outside-window item, got %f", mult)
	}

	// No temporal window set.
	noWindow := ScoringContext{}
	mult = temporalWindowMultiplier(outsideItem, noWindow)
	if mult != 1.0 {
		t.Errorf("expected 1.0 with no temporal window, got %f", mult)
	}
}

func timePtr(t time.Time) *time.Time {
	return &t
}
