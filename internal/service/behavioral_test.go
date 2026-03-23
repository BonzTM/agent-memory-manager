//go:build fts5

package service

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
	"path/filepath"

	"github.com/joshd-04/agent-memory-manager/internal/adapters/sqlite"
	"github.com/joshd-04/agent-memory-manager/internal/core"
)

// testServiceAndRepo creates an AMMService backed by a real SQLite DB and
// returns both the concrete service and the repository for direct inserts.
func testServiceAndRepo(t *testing.T) (*AMMService, *sqlite.SQLiteRepository) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	repo := &sqlite.SQLiteRepository{DB: db}
	svc := New(repo)
	t.Cleanup(func() { db.Close() })
	return svc, repo
}

// ---------------------------------------------------------------------------
// Reflect
// ---------------------------------------------------------------------------

func TestReflect_ExtractsPreferences(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	// Ingest events containing preference phrases.
	phrases := []string{
		"I prefer tabs over spaces for indentation",
		"always use dark mode in the editor",
	}
	for _, p := range phrases {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      p,
			OccurredAt:   time.Now().UTC(),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.Reflect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created < 1 {
		t.Fatalf("expected at least 1 memory created, got %d", created)
	}

	// Verify at least one memory with type=preference.
	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range mems {
		if m.Type == core.MemoryTypePreference {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one preference memory after Reflect")
	}
}

func TestReflect_ExtractsDecisions(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.IngestEvent(ctx, &core.Event{
		Kind:         "message",
		SourceSystem: "test",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "We decided to use PostgreSQL for the database layer",
		OccurredAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	created, err := svc.Reflect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created < 1 {
		t.Fatalf("expected at least 1 memory, got %d", created)
	}

	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range mems {
		if m.Type == core.MemoryTypeDecision {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected at least one decision memory after Reflect")
	}
}

func TestReflect_SkipsDuplicates(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.IngestEvent(ctx, &core.Event{
		Kind:         "message",
		SourceSystem: "test",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "I prefer using Go for backend services",
		OccurredAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := svc.Reflect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first < 1 {
		t.Fatalf("expected first Reflect to create >= 1, got %d", first)
	}

	second, err := svc.Reflect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second != 0 {
		t.Errorf("expected second Reflect to create 0 (dedup), got %d", second)
	}
}

// ---------------------------------------------------------------------------
// CompressHistory
// ---------------------------------------------------------------------------

func TestCompressHistory(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	// Ingest 15 events.
	for i := 0; i < 15; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("event number %d about compression testing", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.CompressHistory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// 15 events with chunk size 10 should create 2 leaf summaries.
	if created < 1 {
		t.Fatalf("expected at least 1 leaf summary, got %d", created)
	}

	// Verify summaries exist and have source_span.
	summaries, err := svc.repo.ListSummaries(ctx, core.ListSummariesOptions{Kind: "leaf", Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) == 0 {
		t.Fatal("expected leaf summaries to be created")
	}
	for _, s := range summaries {
		if len(s.SourceSpan.EventIDs) == 0 {
			t.Errorf("summary %s has empty source_span.event_ids", s.ID)
		}
	}
}

// ---------------------------------------------------------------------------
// ConsolidateSessions
// ---------------------------------------------------------------------------

func TestConsolidateSessions(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	sessID := "sess_consolidate_test"
	for i := 0; i < 5; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			SessionID:    sessID,
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("session event %d discussing consolidation", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	created, err := svc.ConsolidateSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created < 1 {
		t.Fatalf("expected at least 1 session summary, got %d", created)
	}

	summaries, err := svc.repo.ListSummaries(ctx, core.ListSummariesOptions{Kind: "session", SessionID: sessID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 session summary, got %d", len(summaries))
	}
	if summaries[0].SessionID != sessID {
		t.Errorf("expected session_id=%s, got %s", sessID, summaries[0].SessionID)
	}
}

// ---------------------------------------------------------------------------
// Ingestion policy
// ---------------------------------------------------------------------------

func TestIngestionPolicy_Ignore(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	now := time.Now().UTC()
	err := repo.InsertIngestionPolicy(ctx, &core.IngestionPolicy{
		ID:          "pol_ign",
		PatternType: "source",
		Pattern:     "noisy_system",
		Mode:        "ignore",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatal(err)
	}

	evt := &core.Event{
		Kind:         "message",
		SourceSystem: "noisy_system",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "this should be ignored",
		OccurredAt:   now,
	}

	result, err := svc.IngestEvent(ctx, evt)
	if err != nil {
		t.Fatal(err)
	}
	// The event is returned but not persisted.
	if result == nil {
		t.Fatal("expected non-nil event returned")
	}

	// Verify it was NOT stored.
	events, err := svc.repo.ListEvents(ctx, core.ListEventsOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if e.Content == "this should be ignored" {
			t.Error("event with ignore policy should not be persisted")
		}
	}
}

func TestIngestionPolicy_ReadOnly(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	now := time.Now().UTC()
	err := repo.InsertIngestionPolicy(ctx, &core.IngestionPolicy{
		ID:          "pol_ro",
		PatternType: "source",
		Pattern:     "readonly_system",
		Mode:        "read_only",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatal(err)
	}

	evt := &core.Event{
		Kind:         "message",
		SourceSystem: "readonly_system",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "this should be stored but not reflected",
		OccurredAt:   now,
	}

	ingest, createMem, err := svc.ShouldIngest(ctx, evt)
	if err != nil {
		t.Fatal(err)
	}
	if !ingest {
		t.Error("expected ingest=true for read_only policy")
	}
	if createMem {
		t.Error("expected createMemory=false for read_only policy")
	}

	// The event should still be ingested (stored in history).
	result, err := svc.IngestEvent(ctx, evt)
	if err != nil {
		t.Fatal(err)
	}
	if result.ID == "" {
		t.Error("expected event to be stored with an ID")
	}
}

func TestIngestionPolicy_Default(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	evt := &core.Event{
		Kind:         "message",
		SourceSystem: "normal_system",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "normal event",
		OccurredAt:   time.Now().UTC(),
	}

	ingest, createMem, err := svc.ShouldIngest(ctx, evt)
	if err != nil {
		t.Fatal(err)
	}
	if !ingest {
		t.Error("expected ingest=true with no policy (default full)")
	}
	if !createMem {
		t.Error("expected createMemory=true with no policy (default full)")
	}
}

// ---------------------------------------------------------------------------
// Supersession
// ---------------------------------------------------------------------------

func TestSupersession_Remember(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	memA, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "Go version is 1.21",
		TightDescription: "Go 1.21",
	})
	if err != nil {
		t.Fatal(err)
	}

	memB, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "Go version is 1.22",
		TightDescription: "Go 1.22",
		Supersedes:       memA.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	if memB.Supersedes != memA.ID {
		t.Errorf("expected memB.Supersedes=%s, got %s", memA.ID, memB.Supersedes)
	}

	// Verify A is now superseded.
	updatedA, err := svc.repo.GetMemory(ctx, memA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedA.Status != core.MemoryStatusSuperseded {
		t.Errorf("expected A status=superseded, got %s", updatedA.Status)
	}
	if updatedA.SupersededBy != memB.ID {
		t.Errorf("expected A.SupersededBy=%s, got %s", memB.ID, updatedA.SupersededBy)
	}
}

func TestSupersession_RecallFilters(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	memA, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "deployment target is kubernetes cluster alpha",
		TightDescription: "deploy to k8s alpha",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "deployment target is kubernetes cluster beta",
		TightDescription: "deploy to k8s beta",
		Supersedes:       memA.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.Recall(ctx, "deployment kubernetes", core.RecallOptions{
		Mode: core.RecallModeFacts,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, item := range result.Items {
		if item.ID == memA.ID {
			t.Error("superseded memory A should not appear in recall results")
		}
	}
}

// ---------------------------------------------------------------------------
// Repair / CheckIntegrity
// ---------------------------------------------------------------------------

func TestCheckIntegrity_Clean(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	// Add valid data.
	_, err := svc.IngestEvent(ctx, &core.Event{
		Kind:         "message",
		SourceSystem: "test",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "clean integrity test event",
		OccurredAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "clean integrity test memory",
		TightDescription: "integrity test",
	})
	if err != nil {
		t.Fatal(err)
	}

	report, err := svc.CheckIntegrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Issues != 0 {
		t.Errorf("expected 0 issues, got %d: %v", report.Issues, report.Details)
	}
}

func TestCheckIntegrity_BrokenSupersession(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	// Create a memory that supersedes a non-existent memory.
	now := time.Now().UTC()
	mem := &core.Memory{
		ID:               "mem_broken",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "broken supersession pointer",
		TightDescription: "broken",
		Confidence:       0.8,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		Supersedes:       "mem_nonexistent",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := svc.repo.InsertMemory(ctx, mem); err != nil {
		t.Fatal(err)
	}

	report, err := svc.CheckIntegrity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.Issues == 0 {
		t.Error("expected at least 1 issue for broken supersession")
	}

	// Verify the details mention supersession.
	foundDetail := false
	for _, d := range report.Details {
		if strings.Contains(d, "supersession") {
			foundDetail = true
			break
		}
	}
	if !foundDetail {
		t.Errorf("expected supersession issue in details, got %v", report.Details)
	}
}

// ---------------------------------------------------------------------------
// ExplainRecall
// ---------------------------------------------------------------------------

func TestExplainRecall(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "Terraform manages infrastructure as code",
		TightDescription: "Terraform IaC",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.ExplainRecall(ctx, "Terraform infrastructure", mem.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Verify required fields are present.
	for _, key := range []string{"query", "item_id", "item_kind", "signal_breakdown", "final_score"} {
		if _, ok := result[key]; !ok {
			t.Errorf("expected key %q in ExplainRecall result", key)
		}
	}

	breakdown, ok := result["signal_breakdown"].(SignalBreakdown)
	if !ok {
		t.Fatalf("expected signal_breakdown to be SignalBreakdown, got %T", result["signal_breakdown"])
	}
	if breakdown.FinalScore < 0 || breakdown.FinalScore > 1 {
		t.Errorf("expected final_score in [0,1], got %f", breakdown.FinalScore)
	}
	if result["item_kind"] != "memory" {
		t.Errorf("expected item_kind=memory, got %v", result["item_kind"])
	}
}

// ---------------------------------------------------------------------------
// Expand summary hierarchy
// ---------------------------------------------------------------------------

func TestExpandSummaryHierarchy(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	// Ingest events to get real event IDs.
	var eventIDs []string
	for i := 0; i < 3; i++ {
		evt, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("expand hierarchy event %d", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
		eventIDs = append(eventIDs, evt.ID)
	}

	// Create a summary with source_span pointing to the events.
	now := time.Now().UTC()
	sum := &core.Summary{
		ID:               "sum_expand_test",
		Kind:             "leaf",
		Scope:            core.ScopeGlobal,
		Title:            "Test Expand Summary",
		Body:             "body of the summary",
		TightDescription: "expand test",
		PrivacyLevel:     core.PrivacyPrivate,
		SourceSpan:       core.SourceSpan{EventIDs: eventIDs},
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.InsertSummary(ctx, sum); err != nil {
		t.Fatal(err)
	}

	// Also create summary_edges for the events.
	for i, eid := range eventIDs {
		edge := &core.SummaryEdge{
			ParentSummaryID: sum.ID,
			ChildKind:       "event",
			ChildID:         eid,
			EdgeOrder:       i,
		}
		if err := repo.InsertSummaryEdge(ctx, edge); err != nil {
			t.Fatal(err)
		}
	}

	// Call Expand.
	result, err := svc.Expand(ctx, sum.ID, "summary")
	if err != nil {
		t.Fatal(err)
	}

	if result.Summary == nil {
		t.Fatal("expected non-nil Summary in expand result")
	}
	if result.Summary.ID != sum.ID {
		t.Errorf("expected summary ID %s, got %s", sum.ID, result.Summary.ID)
	}

	// The events should be returned (via edges or source_span).
	if len(result.Events) != 3 {
		t.Errorf("expected 3 events from expand, got %d", len(result.Events))
	}
}

// ---------------------------------------------------------------------------
// ExtractClaims
// ---------------------------------------------------------------------------

func TestExtractClaims(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Subject:          "AMM",
		Body:             "AMM uses SQLite for storage",
		TightDescription: "AMM uses SQLite",
	})
	if err != nil {
		t.Fatal(err)
	}

	created, err := svc.ExtractClaims(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created < 1 {
		t.Fatalf("expected at least 1 claim created, got %d", created)
	}

	// Verify a claim with predicate "uses" exists.
	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{Status: core.MemoryStatusActive, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range mems {
		claims, err := svc.repo.ListClaimsByMemory(ctx, m.ID)
		if err != nil {
			continue
		}
		for _, c := range claims {
			if c.Predicate == "uses" {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("expected a claim with predicate 'uses' after ExtractClaims")
	}
}

func TestExtractClaims_SkipsExisting(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Subject:          "AMM",
		Body:             "AMM uses SQLite for storage",
		TightDescription: "AMM uses SQLite",
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := svc.ExtractClaims(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first < 1 {
		t.Fatalf("expected first ExtractClaims to create >= 1, got %d", first)
	}

	second, err := svc.ExtractClaims(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second != 0 {
		t.Errorf("expected second ExtractClaims to create 0 (already extracted), got %d", second)
	}
}

// ---------------------------------------------------------------------------
// FormEpisodes
// ---------------------------------------------------------------------------

func TestFormEpisodes(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	sessID := "ep-sess"
	var eventIDs []string
	for i := 0; i < 5; i++ {
		evt, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			SessionID:    sessID,
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("episode formation event %d about testing", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
		eventIDs = append(eventIDs, evt.ID)
	}

	created, err := svc.FormEpisodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if created < 1 {
		t.Fatalf("expected at least 1 episode created, got %d", created)
	}

	// Verify the episode has the correct session_id and source_span.
	episodes, err := svc.repo.ListEpisodes(ctx, core.ListEpisodesOptions{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ep := range episodes {
		if ep.SessionID == sessID {
			found = true
			// Verify all event IDs are in the source span.
			spanSet := make(map[string]bool, len(ep.SourceSpan.EventIDs))
			for _, eid := range ep.SourceSpan.EventIDs {
				spanSet[eid] = true
			}
			for _, eid := range eventIDs {
				if !spanSet[eid] {
					t.Errorf("expected event %s in episode source_span", eid)
				}
			}
			break
		}
	}
	if !found {
		t.Error("expected an episode with session_id 'ep-sess'")
	}
}

func TestFormEpisodes_SkipsExisting(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	sessID := "epsessdedup"
	for i := 0; i < 3; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			SessionID:    sessID,
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("dedup episode event %d with epsessdedup session", i),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	first, err := svc.FormEpisodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first < 1 {
		t.Fatalf("expected first FormEpisodes to create >= 1, got %d", first)
	}

	second, err := svc.FormEpisodes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if second != 0 {
		t.Errorf("expected second FormEpisodes to create 0 (already formed), got %d", second)
	}
}

// ---------------------------------------------------------------------------
// DecayStaleMemories
// ---------------------------------------------------------------------------

func TestDecayStaleMemories_Fresh(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "fresh memory that should not decay",
		TightDescription: "fresh memory",
	})
	if err != nil {
		t.Fatal(err)
	}

	decayed, err := svc.DecayStaleMemories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if decayed != 0 {
		t.Errorf("expected 0 decayed for a fresh memory, got %d", decayed)
	}
}

func TestDecayStaleMemories_Stale(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypePreference,
		Body:             "stale memory that should decay",
		TightDescription: "stale memory",
		Importance:       0.5,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Manually set timestamps to 60 days ago so the memory appears stale.
	// UpdateMemory does not update created_at, so we use raw SQL for that column.
	sixtyDaysAgo := time.Now().UTC().Add(-60 * 24 * time.Hour)
	sixtyDaysAgoStr := sixtyDaysAgo.Format(time.RFC3339)
	mem.UpdatedAt = sixtyDaysAgo
	obs := sixtyDaysAgo
	mem.ObservedAt = &obs
	if err := repo.UpdateMemory(ctx, mem); err != nil {
		t.Fatal(err)
	}
	// Also set created_at via raw SQL since UpdateMemory doesn't touch it.
	_, err = repo.ExecContext(ctx, "UPDATE memories SET created_at=? WHERE id=?", sixtyDaysAgoStr, mem.ID)
	if err != nil {
		t.Fatal(err)
	}

	decayed, err := svc.DecayStaleMemories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if decayed < 1 {
		t.Fatalf("expected at least 1 decayed for a stale memory, got %d", decayed)
	}

	// Verify the memory was actually modified (importance reduced or archived).
	updated, err := repo.GetMemory(ctx, mem.ID)
	if err != nil {
		t.Fatal(err)
	}
	importanceReduced := updated.Importance < 0.5
	archived := updated.Status == core.MemoryStatusArchived
	if !importanceReduced && !archived {
		t.Errorf("expected stale memory to have reduced importance or be archived; importance=%f status=%s",
			updated.Importance, updated.Status)
	}
}

// ---------------------------------------------------------------------------
// DetectContradictions
// ---------------------------------------------------------------------------

func TestDetectContradictions(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	// Remember two conflicting memories.
	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Subject:          "AMM",
		Body:             "AMM uses SQLite for persistence",
		TightDescription: "AMM uses SQLite",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Subject:          "AMM",
		Body:             "AMM uses Postgres for persistence",
		TightDescription: "AMM uses Postgres",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Extract claims so the contradiction detector has something to compare.
	claimsCreated, err := svc.ExtractClaims(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if claimsCreated < 2 {
		t.Fatalf("expected at least 2 claims extracted, got %d", claimsCreated)
	}

	found, err := svc.DetectContradictions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if found < 1 {
		t.Fatalf("expected at least 1 contradiction detected, got %d", found)
	}

	// Verify a contradiction memory was created.
	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Type:   core.MemoryTypeContradiction,
		Status: core.MemoryStatusActive,
		Limit:  100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) < 1 {
		t.Error("expected at least one contradiction memory to be created")
	}
}

// ---------------------------------------------------------------------------
// MergeDuplicates
// ---------------------------------------------------------------------------

func TestMergeDuplicates(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	// Remember two nearly identical memories.
	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "The deployment pipeline uses GitHub Actions for CI and CD",
		TightDescription: "deployment pipeline uses GitHub Actions CI CD",
		Confidence:       0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "The deployment pipeline uses GitHub Actions for CI and CD workflows",
		TightDescription: "deployment pipeline uses GitHub Actions CI CD workflows",
		Confidence:       0.9,
	})
	if err != nil {
		t.Fatal(err)
	}

	merged, err := svc.MergeDuplicates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if merged < 1 {
		t.Fatalf("expected at least 1 merge, got %d", merged)
	}

	// Verify one memory is now superseded.
	mems, err := svc.repo.ListMemories(ctx, core.ListMemoriesOptions{
		Status: core.MemoryStatusSuperseded,
		Limit:  100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(mems) < 1 {
		t.Error("expected at least one superseded memory after MergeDuplicates")
	}
}

// ---------------------------------------------------------------------------
// jaccardSimilarity
// ---------------------------------------------------------------------------

func TestJaccardSimilarity(t *testing.T) {
	// Identical text should yield 1.0.
	sim := jaccardSimilarity("hello world", "hello world")
	if math.Abs(sim-1.0) > 1e-9 {
		t.Errorf("expected similarity 1.0 for identical text, got %f", sim)
	}

	// Completely different text should yield 0.0.
	sim = jaccardSimilarity("alpha beta gamma", "delta epsilon zeta")
	if sim != 0.0 {
		t.Errorf("expected similarity 0.0 for completely different text, got %f", sim)
	}

	// Partial overlap should be between 0 and 1.
	sim = jaccardSimilarity("the quick brown fox", "the slow brown dog")
	if sim <= 0.0 || sim >= 1.0 {
		t.Errorf("expected similarity between 0 and 1 for partial overlap, got %f", sim)
	}

	// Verify the known value: intersection={the, brown}=2, union={the,quick,brown,fox,slow,dog}=6 => 2/6 ≈ 0.333
	expected := 2.0 / 6.0
	if math.Abs(sim-expected) > 1e-9 {
		t.Errorf("expected similarity %f for partial overlap, got %f", expected, sim)
	}

	// Both empty should yield 1.0.
	sim = jaccardSimilarity("", "")
	if math.Abs(sim-1.0) > 1e-9 {
		t.Errorf("expected similarity 1.0 for two empty strings, got %f", sim)
	}
}
