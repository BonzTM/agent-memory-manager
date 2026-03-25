//go:build fts5

package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshd-04/agent-memory-manager/internal/core"
)

type dummySummarizer struct{}

func (dummySummarizer) Summarize(context.Context, string, int) (string, error) { return "x", nil }
func (dummySummarizer) ExtractMemoryCandidate(context.Context, string) ([]core.MemoryCandidate, error) {
	return nil, nil
}
func (dummySummarizer) ExtractMemoryCandidateBatch(_ context.Context, events []string) ([]core.MemoryCandidate, error) {
	return nil, nil
}

func TestIngestTranscriptGettersAndUpdateMemory(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	events := []*core.Event{
		{Kind: "message", SourceSystem: "test", SessionID: "sess_cov", ProjectID: "proj_cov", PrivacyLevel: core.PrivacyPrivate, Content: "first event", OccurredAt: now},
		{Kind: "message", SourceSystem: "test", SessionID: "sess_cov", ProjectID: "proj_cov", PrivacyLevel: core.PrivacyPrivate, Content: "second event", OccurredAt: now.Add(time.Second)},
	}
	ingested, err := svc.IngestTranscript(ctx, events)
	if err != nil {
		t.Fatalf("ingest transcript: %v", err)
	}
	if ingested != 2 || events[0].ID == "" || events[1].ID == "" {
		t.Fatalf("unexpected transcript ingest result: ingested=%d events=%+v", ingested, events)
	}

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeProject,
		ProjectID:        "proj_cov",
		Body:             "initial body",
		TightDescription: "initial",
	})
	if err != nil {
		t.Fatalf("remember: %v", err)
	}

	gotMem, err := svc.GetMemory(ctx, mem.ID)
	if err != nil {
		t.Fatalf("get memory: %v", err)
	}
	gotMem.Body = "updated body"
	gotMem.TightDescription = "updated"
	updated, err := svc.UpdateMemory(ctx, gotMem)
	if err != nil {
		t.Fatalf("update memory: %v", err)
	}
	if updated.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be set by UpdateMemory")
	}

	summary := &core.Summary{
		ID:               "sum_cov",
		Kind:             "leaf",
		Scope:            core.ScopeProject,
		ProjectID:        "proj_cov",
		Body:             "summary body",
		TightDescription: "summary tight",
		PrivacyLevel:     core.PrivacyPrivate,
		SourceSpan:       core.SourceSpan{EventIDs: []string{events[0].ID}},
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.InsertSummary(ctx, summary); err != nil {
		t.Fatalf("insert summary: %v", err)
	}
	if _, err := svc.GetSummary(ctx, summary.ID); err != nil {
		t.Fatalf("get summary: %v", err)
	}

	episode := &core.Episode{
		ID:               "epi_cov",
		Title:            "Episode",
		Summary:          "Episode summary",
		TightDescription: "Episode tight",
		Scope:            core.ScopeProject,
		ProjectID:        "proj_cov",
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		SourceSpan:       core.SourceSpan{EventIDs: []string{events[1].ID}},
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.InsertEpisode(ctx, episode); err != nil {
		t.Fatalf("insert episode: %v", err)
	}
	if _, err := svc.GetEpisode(ctx, episode.ID); err != nil {
		t.Fatalf("get episode: %v", err)
	}

	entity := &core.Entity{ID: "ent_cov", Type: "service", CanonicalName: "AlphaSvc", Description: "alpha", CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEntity(ctx, entity); err != nil {
		t.Fatalf("insert entity: %v", err)
	}
	if _, err := svc.GetEntity(ctx, entity.ID); err != nil {
		t.Fatalf("get entity: %v", err)
	}
}

func TestRecallModesCoverage(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	evt, err := svc.IngestEvent(ctx, &core.Event{Kind: "message_user", SourceSystem: "test", SessionID: "sess_modes", ProjectID: "proj_modes", PrivacyLevel: core.PrivacyPrivate, Content: "alpha timeline event", OccurredAt: now})
	if err != nil {
		t.Fatalf("ingest event: %v", err)
	}

	if _, err := svc.Remember(ctx, &core.Memory{Type: core.MemoryTypeFact, Scope: core.ScopeProject, ProjectID: "proj_modes", Body: "alpha project memory", TightDescription: "alpha fact"}); err != nil {
		t.Fatalf("remember project memory: %v", err)
	}
	if _, err := svc.Remember(ctx, &core.Memory{Type: core.MemoryTypeActiveContext, Scope: core.ScopeSession, SessionID: "sess_modes", Body: "active alpha task", TightDescription: "active alpha"}); err != nil {
		t.Fatalf("remember active memory: %v", err)
	}

	if err := repo.InsertSummary(ctx, &core.Summary{ID: "sum_modes", Kind: "leaf", Scope: core.ScopeProject, ProjectID: "proj_modes", Body: "alpha summary body", TightDescription: "alpha summary", PrivacyLevel: core.PrivacyPrivate, SourceSpan: core.SourceSpan{EventIDs: []string{evt.ID}}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("insert summary: %v", err)
	}
	if err := repo.InsertEpisode(ctx, &core.Episode{ID: "epi_modes", Title: "alpha episode", Summary: "episode about alpha", TightDescription: "alpha episode", Scope: core.ScopeProject, ProjectID: "proj_modes", Importance: 0.6, PrivacyLevel: core.PrivacyPrivate, SourceSpan: core.SourceSpan{EventIDs: []string{evt.ID}}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("insert episode: %v", err)
	}
	if err := repo.InsertEntity(ctx, &core.Entity{ID: "ent_modes", Type: "component", CanonicalName: "AlphaService", Description: "handles alpha workload", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("insert entity: %v", err)
	}

	episodes, err := svc.Recall(ctx, "alpha", core.RecallOptions{Mode: core.RecallModeEpisodes, Limit: 10})
	if err != nil || len(episodes.Items) == 0 {
		t.Fatalf("recall episodes failed: err=%v items=%d", err, len(episodes.Items))
	}

	project, err := svc.Recall(ctx, "alpha", core.RecallOptions{Mode: core.RecallModeProject, ProjectID: "proj_modes", Limit: 10})
	if err != nil || len(project.Items) == 0 {
		t.Fatalf("recall project failed: err=%v items=%d", err, len(project.Items))
	}

	entity, err := svc.Recall(ctx, "AlphaService", core.RecallOptions{Mode: core.RecallModeEntity, Limit: 10})
	if err != nil {
		t.Fatalf("recall entity failed: %v", err)
	}
	foundEntity := false
	for _, it := range entity.Items {
		if it.Kind == "entity" {
			foundEntity = true
			break
		}
	}
	if !foundEntity {
		t.Fatalf("expected entity item in recall results: %+v", entity.Items)
	}

	history, err := svc.Recall(ctx, "timeline", core.RecallOptions{Mode: core.RecallModeHistory, Limit: 10})
	if err != nil || len(history.Items) == 0 {
		t.Fatalf("recall history failed: err=%v items=%d", err, len(history.Items))
	}

	hybrid, err := svc.Recall(ctx, "alpha", core.RecallOptions{Mode: core.RecallModeHybrid, Limit: 10})
	if err != nil || len(hybrid.Items) == 0 {
		t.Fatalf("recall hybrid failed: err=%v items=%d", err, len(hybrid.Items))
	}

	timeline, err := svc.Recall(ctx, "", core.RecallOptions{Mode: core.RecallModeTimeline, SessionID: "sess_modes", ProjectID: "proj_modes", Limit: 10})
	if err != nil || len(timeline.Items) == 0 {
		t.Fatalf("recall timeline failed: err=%v items=%d", err, len(timeline.Items))
	}
	if timeline.Items[0].Score != 1.0 {
		t.Fatalf("expected top timeline item score 1.0, got %f", timeline.Items[0].Score)
	}

	active, err := svc.Recall(ctx, "", core.RecallOptions{Mode: core.RecallModeActive, Limit: 10})
	if err != nil || len(active.Items) == 0 {
		t.Fatalf("recall active failed: err=%v items=%d", err, len(active.Items))
	}

	if _, err := svc.Recall(ctx, "x", core.RecallOptions{Mode: "unknown_mode"}); !errors.Is(err, core.ErrInvalidMode) {
		t.Fatalf("expected invalid mode error, got %v", err)
	}
}

func TestRunJobAndRepairPaths(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	if _, err := svc.IngestEvent(ctx, &core.Event{Kind: "message", SourceSystem: "test", PrivacyLevel: core.PrivacyPrivate, SessionID: "sess_jobs", Content: "I prefer concise replies", OccurredAt: now}); err != nil {
		t.Fatalf("ingest event: %v", err)
	}

	reflectJob, err := svc.RunJob(ctx, "reflect")
	if err != nil {
		t.Fatalf("run reflect job: %v", err)
	}
	if reflectJob.Status != "completed" {
		t.Fatalf("expected completed reflect job, got %+v", reflectJob)
	}

	cleanupJob, err := svc.RunJob(ctx, "cleanup_recall_history")
	if err != nil {
		t.Fatalf("run cleanup_recall_history job: %v", err)
	}
	if cleanupJob.Status != "completed" {
		t.Fatalf("expected completed cleanup job, got %+v", cleanupJob)
	}

	badJob, err := svc.RunJob(ctx, "invalid_job_kind")
	if err == nil || badJob == nil || badJob.Status != "failed" {
		t.Fatalf("expected failed invalid job, got job=%+v err=%v", badJob, err)
	}

	broken := &core.Memory{
		ID:               "mem_broken_fix",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "broken links",
		TightDescription: "broken links",
		Confidence:       0.8,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		Supersedes:       "mem_missing_a",
		SupersededBy:     "mem_missing_b",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.InsertMemory(ctx, broken); err != nil {
		t.Fatalf("insert broken memory: %v", err)
	}

	repairLinks, err := svc.Repair(ctx, false, "links")
	if err != nil {
		t.Fatalf("repair links: %v", err)
	}
	if repairLinks.Fixed < 2 {
		t.Fatalf("expected at least 2 fixed pointers, got %+v", repairLinks)
	}

	fixedMem, err := repo.GetMemory(ctx, broken.ID)
	if err != nil {
		t.Fatalf("get fixed memory: %v", err)
	}
	if fixedMem.Supersedes != "" || fixedMem.SupersededBy != "" {
		t.Fatalf("expected supersession pointers cleared, got %+v", fixedMem)
	}

	if _, err := svc.Repair(ctx, false, "recall_history"); err != nil {
		t.Fatalf("repair recall_history: %v", err)
	}

	if _, err := svc.Repair(ctx, false, "unknown_fix"); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("expected invalid fix error, got %v", err)
	}
}

func TestDescribeExpandHistoryExplainAndConversionHelpers(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	evt, err := svc.IngestEvent(ctx, &core.Event{Kind: "message_user", SourceSystem: "test", SessionID: "sess_d", ProjectID: "proj_d", PrivacyLevel: core.PrivacyPrivate, Content: "describe-expand event", OccurredAt: now})
	if err != nil {
		t.Fatalf("ingest event: %v", err)
	}

	mem, err := svc.Remember(ctx, &core.Memory{Type: core.MemoryTypeFact, Scope: core.ScopeProject, ProjectID: "proj_d", Body: "describe memory body", TightDescription: "describe memory"})
	if err != nil {
		t.Fatalf("remember memory: %v", err)
	}

	sum := &core.Summary{ID: "sum_d", Kind: "leaf", Scope: core.ScopeProject, ProjectID: "proj_d", Body: "summary body", TightDescription: "summary tight", PrivacyLevel: core.PrivacyPrivate, SourceSpan: core.SourceSpan{EventIDs: []string{evt.ID}}, CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertSummary(ctx, sum); err != nil {
		t.Fatalf("insert summary: %v", err)
	}
	ep := &core.Episode{ID: "epi_d", Title: "Episode D", Summary: "episode summary", TightDescription: "episode tight", Scope: core.ScopeProject, ProjectID: "proj_d", Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, SourceSpan: core.SourceSpan{EventIDs: []string{evt.ID}}, CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEpisode(ctx, ep); err != nil {
		t.Fatalf("insert episode: %v", err)
	}

	described, err := svc.Describe(ctx, []string{mem.ID, sum.ID, ep.ID, "missing"})
	if err != nil {
		t.Fatalf("describe failed: %v", err)
	}
	if len(described) != 3 {
		t.Fatalf("expected 3 describe results, got %d", len(described))
	}

	if _, err := svc.Expand(ctx, ep.ID, "episode"); err != nil {
		t.Fatalf("expand episode failed: %v", err)
	}
	if _, err := svc.Expand(ctx, mem.ID, "unknown-kind"); !errors.Is(err, core.ErrInvalidInput) {
		t.Fatalf("expected invalid input for unknown expand kind, got %v", err)
	}

	byQuery, err := svc.History(ctx, "describe-expand", core.HistoryOptions{Limit: 10})
	if err != nil || len(byQuery) == 0 {
		t.Fatalf("history query failed: err=%v len=%d", err, len(byQuery))
	}
	fallback, err := svc.History(ctx, "", core.HistoryOptions{ProjectID: "proj_d", Limit: 10})
	if err != nil || len(fallback) == 0 {
		t.Fatalf("history fallback failed: err=%v len=%d", err, len(fallback))
	}

	if _, err := svc.ExplainRecall(ctx, "summary", sum.ID); err != nil {
		t.Fatalf("explain recall summary failed: %v", err)
	}
	if _, err := svc.ExplainRecall(ctx, "episode", ep.ID); err != nil {
		t.Fatalf("explain recall episode failed: %v", err)
	}
	if _, err := svc.ExplainRecall(ctx, "event", evt.ID); err != nil {
		t.Fatalf("explain recall event failed: %v", err)
	}
	if _, err := svc.ExplainRecall(ctx, "missing", "id_missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expected not found from explain recall missing id, got %v", err)
	}

	sumItems := summariesToRecallItems([]core.Summary{*sum})
	epItems := episodesToRecallItems([]core.Episode{*ep})
	if len(sumItems) != 1 || len(epItems) != 1 {
		t.Fatalf("expected conversion helpers to return one item each: summaries=%d episodes=%d", len(sumItems), len(epItems))
	}
	if positionScore(0) != 1.0 || positionScore(3) >= 1.0 {
		t.Fatalf("unexpected position scores: p0=%f p3=%f", positionScore(0), positionScore(3))
	}
}

func TestRunJobDispatchForRemainingKinds(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	for i := 0; i < 6; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{Kind: "message", SourceSystem: "test", SessionID: "sess_dispatch", ProjectID: "proj_dispatch", PrivacyLevel: core.PrivacyPrivate, Content: "dispatch event", OccurredAt: now.Add(time.Duration(i) * time.Second)})
		if err != nil {
			t.Fatalf("seed ingest event %d failed: %v", i, err)
		}
	}

	_, _ = svc.Remember(ctx, &core.Memory{Type: core.MemoryTypeFact, Subject: "amm", Body: "amm uses sqlite", TightDescription: "uses sqlite"})
	_, _ = svc.Remember(ctx, &core.Memory{Type: core.MemoryTypeFact, Subject: "amm", Body: "amm uses postgres", TightDescription: "uses postgres"})
	_, _ = svc.Remember(ctx, &core.Memory{Type: core.MemoryTypeFact, Body: "duplicate text for merge duplicates", TightDescription: "duplicate text for merge duplicates", Confidence: 0.7})
	_, _ = svc.Remember(ctx, &core.Memory{Type: core.MemoryTypeFact, Body: "duplicate text for merge duplicates workflow", TightDescription: "duplicate text for merge duplicates workflow", Confidence: 0.8})

	kinds := []string{
		"compress_history",
		"consolidate_sessions",
		"rebuild_indexes",
		"extract_claims",
		"form_episodes",
		"detect_contradictions",
		"decay_stale_memory",
		"merge_duplicates",
	}

	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			job, err := svc.RunJob(ctx, kind)
			if err != nil {
				t.Fatalf("run job %s failed: %v", kind, err)
			}
			if job.Status != "completed" {
				t.Fatalf("expected completed status for %s, got %+v", kind, job)
			}
		})
	}
}

func TestRepairInternalCheckHelpersCoverage(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	mem := &core.Memory{ID: "mem_chk", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "helper memory", TightDescription: "helper memory", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertMemory(ctx, mem); err != nil {
		t.Fatalf("insert memory: %v", err)
	}
	ent := &core.Entity{ID: "ent_chk", Type: "person", CanonicalName: "Checker", CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEntity(ctx, ent); err != nil {
		t.Fatalf("insert entity: %v", err)
	}
	if err := repo.LinkMemoryEntity(ctx, mem.ID, ent.ID, "owner"); err != nil {
		t.Fatalf("link memory entity: %v", err)
	}

	brokenSummary := &core.Summary{ID: "sum_chk_broken", Kind: "leaf", Scope: core.ScopeGlobal, Body: "broken summary", TightDescription: "broken", PrivacyLevel: core.PrivacyPrivate, SourceSpan: core.SourceSpan{EventIDs: []string{"evt_missing"}}, CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertSummary(ctx, brokenSummary); err != nil {
		t.Fatalf("insert broken summary: %v", err)
	}

	orphanSummary := &core.Summary{ID: "sum_chk_orphan", Kind: "leaf", Scope: core.ScopeGlobal, Body: "orphan summary", TightDescription: "orphan", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertSummary(ctx, orphanSummary); err != nil {
		t.Fatalf("insert orphan summary: %v", err)
	}

	parent := &core.Summary{ID: "sum_chk_parent", Kind: "session", Scope: core.ScopeGlobal, Body: "parent summary", TightDescription: "parent", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertSummary(ctx, parent); err != nil {
		t.Fatalf("insert parent summary: %v", err)
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: parent.ID, ChildKind: "mystery", ChildID: "whatever", EdgeOrder: 0}); err != nil {
		t.Fatalf("insert broken summary edge: %v", err)
	}

	mem.SourceEventIDs = []string{"evt_missing"}
	mem.SourceSummaryIDs = []string{"sum_missing"}
	mem.SourceArtifactIDs = []string{"art_missing"}
	if err := repo.UpdateMemory(ctx, mem); err != nil {
		t.Fatalf("update memory with broken sources: %v", err)
	}

	if issues, checked, err := svc.checkSummarySourceLinks(ctx); err != nil || checked == 0 || issues == 0 {
		t.Fatalf("checkSummarySourceLinks unexpected result: issues=%d checked=%d err=%v", issues, checked, err)
	}
	if issues, checked, err := svc.checkMemorySourceLinks(ctx); err != nil || checked == 0 || issues == 0 {
		t.Fatalf("checkMemorySourceLinks unexpected result: issues=%d checked=%d err=%v", issues, checked, err)
	}
	if issues, checked, err := svc.checkEntityLinks(ctx); err != nil || checked == 0 || issues != 0 {
		t.Fatalf("checkEntityLinks unexpected result: issues=%d checked=%d err=%v", issues, checked, err)
	}
	if issues, checked, err := svc.checkOrphanedSummaries(ctx); err != nil || checked == 0 || issues == 0 {
		t.Fatalf("checkOrphanedSummaries unexpected result: issues=%d checked=%d err=%v", issues, checked, err)
	}
	if issues, checked, err := svc.checkSummaryEdgeIntegrity(ctx); err != nil || checked == 0 || issues == 0 {
		t.Fatalf("checkSummaryEdgeIntegrity unexpected result: issues=%d checked=%d err=%v", issues, checked, err)
	}

	if _, err := svc.Status(ctx); err != nil {
		t.Fatalf("status should succeed: %v", err)
	}
}

func TestCheckSupersessionChainsAndInitErrorCoverage(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	a := &core.Memory{ID: "mem_cycle_a", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "A", TightDescription: "A", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, Supersedes: "mem_cycle_b", CreatedAt: now, UpdatedAt: now}
	b := &core.Memory{ID: "mem_cycle_b", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "B", TightDescription: "B", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, Supersedes: "mem_cycle_a", CreatedAt: now, UpdatedAt: now}
	c := &core.Memory{ID: "mem_bad_superseded_by", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "C", TightDescription: "C", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, SupersededBy: "mem_missing_target", CreatedAt: now, UpdatedAt: now}
	for _, m := range []*core.Memory{a, b, c} {
		if err := repo.InsertMemory(ctx, m); err != nil {
			t.Fatalf("insert memory %s: %v", m.ID, err)
		}
	}

	issues, checked, err := svc.checkSupersessionChains(ctx)
	if err != nil {
		t.Fatalf("checkSupersessionChains error: %v", err)
	}
	if checked == 0 || issues == 0 {
		t.Fatalf("expected supersession issues, got issues=%d checked=%d", issues, checked)
	}

	badPath := filepath.Join(string([]byte{0}), "bad.db")
	repo2 := &AMMService{repo: repo}
	if err := repo2.Init(ctx, badPath); err == nil {
		t.Fatal("expected Init to fail for invalid path")
	}
}

func TestStatusErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		breakSQL string
		wantErr  string
	}{
		{name: "count events failure", breakSQL: "DROP TABLE events", wantErr: "count events"},
		{name: "count memories failure", breakSQL: "DROP TABLE memories", wantErr: "count memories"},
		{name: "count summaries failure", breakSQL: "DROP TABLE summaries", wantErr: "count summaries"},
		{name: "count episodes failure", breakSQL: "DROP TABLE episodes", wantErr: "count episodes"},
		{name: "count entities failure", breakSQL: "DROP TABLE entities", wantErr: "count entities"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := testServiceAndRepo(t)
			ctx := context.Background()
			if _, err := repo.ExecContext(ctx, tt.breakSQL); err != nil {
				t.Fatalf("break schema with %q: %v", tt.breakSQL, err)
			}
			_, err := svc.Status(ctx)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected status error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestExpandSummaryChildAndFallbackSourceSpan(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	evt, err := svc.IngestEvent(ctx, &core.Event{Kind: "message", SourceSystem: "test", PrivacyLevel: core.PrivacyPrivate, Content: "expand fallback event", OccurredAt: now})
	if err != nil {
		t.Fatalf("ingest event: %v", err)
	}

	parent := &core.Summary{ID: "sum_expand_parent", Kind: "session", Scope: core.ScopeGlobal, Body: "parent body", TightDescription: "parent", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}
	child := &core.Summary{ID: "sum_expand_child", Kind: "leaf", Scope: core.ScopeGlobal, Body: "child body", TightDescription: "child", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}
	fallback := &core.Summary{ID: "sum_expand_fallback", Kind: "leaf", Scope: core.ScopeGlobal, Body: "fallback body", TightDescription: "fallback", PrivacyLevel: core.PrivacyPrivate, SourceSpan: core.SourceSpan{EventIDs: []string{evt.ID}}, CreatedAt: now, UpdatedAt: now}
	for _, s := range []*core.Summary{parent, child, fallback} {
		if err := repo.InsertSummary(ctx, s); err != nil {
			t.Fatalf("insert summary %s: %v", s.ID, err)
		}
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: parent.ID, ChildKind: "summary", ChildID: child.ID, EdgeOrder: 0}); err != nil {
		t.Fatalf("insert summary child edge: %v", err)
	}

	parentExpanded, err := svc.Expand(ctx, parent.ID, "summary")
	if err != nil {
		t.Fatalf("expand parent summary: %v", err)
	}
	if len(parentExpanded.Children) != 1 || parentExpanded.Children[0].ID != child.ID {
		t.Fatalf("expected expanded child summary, got %+v", parentExpanded.Children)
	}

	fallbackExpanded, err := svc.Expand(ctx, fallback.ID, "summary")
	if err != nil {
		t.Fatalf("expand fallback summary: %v", err)
	}
	if len(fallbackExpanded.Events) != 1 || fallbackExpanded.Events[0].ID != evt.ID {
		t.Fatalf("expected source-span fallback events, got %+v", fallbackExpanded.Events)
	}
}

func TestNewWithCustomSummarizerAndObservedRecallItem(t *testing.T) {
	_, repo := testServiceAndRepo(t)
	svc := New(repo, "/tmp/test.db", dummySummarizer{})
	if svc.summarizer == nil {
		t.Fatal("expected custom summarizer to be set")
	}

	now := time.Now().UTC().Truncate(time.Second)
	item := memoryToRecallItem(core.Memory{ID: "m_obs", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, TightDescription: "observed", Confidence: 0.9, ObservedAt: &now}, 0)
	if item.ObservedAt == "" || item.Confidence == nil {
		t.Fatalf("expected observed_at and confidence to be populated: %+v", item)
	}
}
