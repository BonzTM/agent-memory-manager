//go:build fts5

package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshd-04/agent-memory-manager/internal/core"
)

func TestRepositoryLifecycleAndInitialization(t *testing.T) {
	ctx := context.Background()
	repo := NewSQLiteRepository()
	if repo == nil {
		t.Fatal("expected non-nil repository")
	}
	if err := repo.Close(); err != nil {
		t.Fatalf("close nil db should not error: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "lifecycle.db")
	if err := repo.Open(ctx, dbPath); err != nil {
		t.Fatalf("open repository: %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })

	if repo.Path() != dbPath {
		t.Fatalf("expected db path %q, got %q", dbPath, repo.Path())
	}

	initialized, err := repo.IsInitialized(ctx)
	if err != nil {
		t.Fatalf("is initialized before migrate: %v", err)
	}
	if initialized {
		t.Fatal("expected uninitialized before migrate")
	}

	if err := repo.Migrate(ctx); err != nil {
		t.Fatalf("migrate via repository: %v", err)
	}

	initialized, err = repo.IsInitialized(ctx)
	if err != nil {
		t.Fatalf("is initialized after migrate: %v", err)
	}
	if !initialized {
		t.Fatal("expected initialized after migrate")
	}
}

func TestGenerateIDHasPrefix(t *testing.T) {
	id := generateID("mem_")
	if len(id) <= len("mem_") || id[:4] != "mem_" {
		t.Fatalf("unexpected generated id: %q", id)
	}
}

func TestSummaryEdgesAndListSummaries(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	parent := &core.Summary{
		ID:               "sum_parent",
		Kind:             "session",
		Scope:            core.ScopeProject,
		ProjectID:        "proj_a",
		SessionID:        "sess_a",
		Title:            "Parent",
		Body:             "Parent summary",
		TightDescription: "Parent",
		PrivacyLevel:     core.PrivacyPrivate,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	child := &core.Summary{
		ID:               "sum_child",
		Kind:             "leaf",
		Scope:            core.ScopeProject,
		ProjectID:        "proj_a",
		SessionID:        "sess_a",
		Title:            "Child",
		Body:             "Child summary",
		TightDescription: "Child",
		PrivacyLevel:     core.PrivacyPrivate,
		CreatedAt:        now.Add(time.Second),
		UpdatedAt:        now.Add(time.Second),
	}
	if err := repo.InsertSummary(ctx, parent); err != nil {
		t.Fatalf("insert parent summary: %v", err)
	}
	if err := repo.InsertSummary(ctx, child); err != nil {
		t.Fatalf("insert child summary: %v", err)
	}

	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: parent.ID, ChildKind: "summary", ChildID: child.ID, EdgeOrder: 1}); err != nil {
		t.Fatalf("insert summary edge: %v", err)
	}

	edges, err := repo.GetSummaryChildren(ctx, parent.ID)
	if err != nil {
		t.Fatalf("get summary children: %v", err)
	}
	if len(edges) != 1 || edges[0].ChildID != child.ID {
		t.Fatalf("unexpected edges: %+v", edges)
	}

	listed, err := repo.ListSummaries(ctx, core.ListSummariesOptions{Kind: "leaf", Scope: core.ScopeProject, ProjectID: "proj_a", SessionID: "sess_a", Limit: 10})
	if err != nil {
		t.Fatalf("list summaries: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != child.ID {
		t.Fatalf("unexpected summaries list: %+v", listed)
	}
}

func TestUpdateAndListMemories(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	obs := now.Add(-time.Hour)

	mem := &core.Memory{
		ID:               "mem_update",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeProject,
		ProjectID:        "proj_u",
		Body:             "before",
		TightDescription: "before",
		Confidence:       0.6,
		Importance:       0.4,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		ObservedAt:       &obs,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.InsertMemory(ctx, mem); err != nil {
		t.Fatalf("insert memory: %v", err)
	}

	mem.Body = "after"
	mem.TightDescription = "after"
	mem.Status = core.MemoryStatusArchived
	mem.Tags = []string{"updated"}
	mem.Metadata = map[string]string{"k": "v"}
	mem.UpdatedAt = now.Add(time.Minute)
	if err := repo.UpdateMemory(ctx, mem); err != nil {
		t.Fatalf("update memory: %v", err)
	}

	got, err := repo.GetMemory(ctx, mem.ID)
	if err != nil {
		t.Fatalf("get memory after update: %v", err)
	}
	if got.Body != "after" || got.Status != core.MemoryStatusArchived || len(got.Tags) != 1 || got.Metadata["k"] != "v" {
		t.Fatalf("memory update not persisted: %+v", got)
	}

	listed, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Type: core.MemoryTypeFact, Scope: core.ScopeProject, ProjectID: "proj_u", Status: core.MemoryStatusArchived, Limit: 10})
	if err != nil {
		t.Fatalf("list memories: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != mem.ID {
		t.Fatalf("unexpected memories list: %+v", listed)
	}
}

func TestEntitiesSearchAndMemoryLinks(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	entity := &core.Entity{ID: "ent_link", Type: "person", CanonicalName: "Bob", Description: "Platform engineer", CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEntity(ctx, entity); err != nil {
		t.Fatalf("insert entity: %v", err)
	}

	listed, err := repo.ListEntities(ctx, core.ListEntitiesOptions{Type: "person", Limit: 10})
	if err != nil {
		t.Fatalf("list entities: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != entity.ID {
		t.Fatalf("unexpected entities list: %+v", listed)
	}

	searched, err := repo.SearchEntities(ctx, "Platform", 10)
	if err != nil {
		t.Fatalf("search entities: %v", err)
	}
	if len(searched) == 0 {
		t.Fatal("expected search results for entity description")
	}

	mem := &core.Memory{ID: "mem_link", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "Bob owns deploy tooling", TightDescription: "Bob owns deploy tooling", Confidence: 0.7, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertMemory(ctx, mem); err != nil {
		t.Fatalf("insert memory: %v", err)
	}
	if err := repo.LinkMemoryEntity(ctx, mem.ID, entity.ID, "owner"); err != nil {
		t.Fatalf("link memory entity: %v", err)
	}
	linked, err := repo.GetMemoryEntities(ctx, mem.ID)
	if err != nil {
		t.Fatalf("get memory entities: %v", err)
	}
	if len(linked) != 1 || linked[0].ID != entity.ID {
		t.Fatalf("unexpected linked entities: %+v", linked)
	}
}

func TestListEpisodesArtifactAndJobs(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	start := now.Add(-2 * time.Hour)
	end := now.Add(-time.Hour)

	ep := &core.Episode{
		ID:               "epi_list",
		Title:            "Release work",
		Summary:          "Release completed",
		TightDescription: "Release work",
		Scope:            core.ScopeProject,
		ProjectID:        "proj_e",
		Importance:       0.7,
		PrivacyLevel:     core.PrivacyPrivate,
		StartedAt:        &start,
		EndedAt:          &end,
		Participants:     []string{"user", "assistant"},
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.InsertEpisode(ctx, ep); err != nil {
		t.Fatalf("insert episode: %v", err)
	}
	episodes, err := repo.ListEpisodes(ctx, core.ListEpisodesOptions{Scope: core.ScopeProject, ProjectID: "proj_e", Limit: 10})
	if err != nil {
		t.Fatalf("list episodes: %v", err)
	}
	if len(episodes) != 1 || episodes[0].ID != ep.ID {
		t.Fatalf("unexpected episodes list: %+v", episodes)
	}

	art := &core.Artifact{Kind: "doc", SourceSystem: "cli", ProjectID: "proj_e", Path: "README.md", Content: "artifact body", Metadata: map[string]string{"lang": "md"}, CreatedAt: now}
	if err := repo.InsertArtifact(ctx, art); err != nil {
		t.Fatalf("insert artifact: %v", err)
	}
	if art.ID == "" {
		t.Fatal("expected generated artifact id")
	}
	gotArtifact, err := repo.GetArtifact(ctx, art.ID)
	if err != nil {
		t.Fatalf("get artifact: %v", err)
	}
	if gotArtifact.Path != "README.md" {
		t.Fatalf("unexpected artifact: %+v", gotArtifact)
	}

	job := &core.Job{Kind: "reflect", Status: "pending", Payload: map[string]string{"scope": "project"}, CreatedAt: now}
	if err := repo.InsertJob(ctx, job); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	if job.ID == "" {
		t.Fatal("expected generated job id")
	}

	loaded, err := repo.GetJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get job: %v", err)
	}
	if loaded.Status != "pending" {
		t.Fatalf("unexpected job status: %+v", loaded)
	}

	loaded.Status = "completed"
	loaded.Result = map[string]string{"processed": "10"}
	loaded.ErrorText = ""
	if err := repo.UpdateJob(ctx, loaded); err != nil {
		t.Fatalf("update job: %v", err)
	}

	jobs, err := repo.ListJobs(ctx, core.ListJobsOptions{Kind: "reflect", Status: "completed", Limit: 10})
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("unexpected jobs list: %+v", jobs)
	}
}

func TestMatchIngestionPolicy(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	first := &core.IngestionPolicy{ID: "pol_old", PatternType: "source", Pattern: "svc-*", Mode: "read_only", CreatedAt: now, UpdatedAt: now}
	second := &core.IngestionPolicy{ID: "pol_new", PatternType: "source", Pattern: "svc-orders", Mode: "ignore", CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	if err := repo.InsertIngestionPolicy(ctx, first); err != nil {
		t.Fatalf("insert first policy: %v", err)
	}
	if err := repo.InsertIngestionPolicy(ctx, second); err != nil {
		t.Fatalf("insert second policy: %v", err)
	}

	matched, err := repo.MatchIngestionPolicy(ctx, "source", "svc-orders")
	if err != nil {
		t.Fatalf("match ingestion policy: %v", err)
	}
	if matched == nil || matched.ID != first.ID {
		t.Fatalf("expected oldest matching policy, got %+v", matched)
	}

	none, err := repo.MatchIngestionPolicy(ctx, "source", "random")
	if err != nil {
		t.Fatalf("match ingestion policy for non-match: %v", err)
	}
	if none != nil {
		t.Fatalf("expected nil for non-match, got %+v", none)
	}
}
