//go:build fts5

package sqlite

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
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

	policies := []*core.IngestionPolicy{
		{ID: "pol_glob_default", PatternType: "source", Pattern: "svc-*", Mode: "read_only", CreatedAt: now, UpdatedAt: now},
		{ID: "pol_exact", PatternType: "session", Pattern: "session-123", Mode: "ignore", MatchMode: "exact", CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)},
		{ID: "pol_glob", PatternType: "session", Pattern: "session-*", Mode: "read_only", MatchMode: "glob", CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second)},
		{ID: "pol_regex", PatternType: "agent", Pattern: `^sess_[0-9]+$`, Mode: "ignore", MatchMode: "regex", CreatedAt: now.Add(3 * time.Second), UpdatedAt: now.Add(3 * time.Second)},
		{ID: "pol_regex_invalid", PatternType: "project", Pattern: "(", Mode: "ignore", MatchMode: "regex", CreatedAt: now.Add(4 * time.Second), UpdatedAt: now.Add(4 * time.Second)},
		{ID: "pol_low_priority", PatternType: "surface", Pattern: "ui-*", Mode: "read_only", MatchMode: "glob", Priority: 10, CreatedAt: now.Add(5 * time.Second), UpdatedAt: now.Add(5 * time.Second)},
		{ID: "pol_high_priority", PatternType: "surface", Pattern: "ui-dashboard", Mode: "ignore", MatchMode: "exact", Priority: 100, CreatedAt: now.Add(6 * time.Second), UpdatedAt: now.Add(6 * time.Second)},
	}
	for _, policy := range policies {
		if err := repo.InsertIngestionPolicy(ctx, policy); err != nil {
			t.Fatalf("insert policy %s: %v", policy.ID, err)
		}
	}

	t.Run("default match_mode is glob", func(t *testing.T) {
		matched, err := repo.MatchIngestionPolicy(ctx, "source", "svc-orders")
		if err != nil {
			t.Fatalf("match ingestion policy: %v", err)
		}
		if matched == nil || matched.ID != "pol_glob_default" {
			t.Fatalf("expected default glob policy, got %+v", matched)
		}
		if matched.MatchMode != "glob" {
			t.Fatalf("expected default match_mode glob, got %q", matched.MatchMode)
		}
	})

	t.Run("exact match mode", func(t *testing.T) {
		matched, err := repo.MatchIngestionPolicy(ctx, "session", "session-123")
		if err != nil {
			t.Fatalf("match ingestion policy: %v", err)
		}
		if matched == nil || matched.ID != "pol_exact" {
			t.Fatalf("expected exact policy, got %+v", matched)
		}
	})

	t.Run("glob wildcard match mode", func(t *testing.T) {
		matched, err := repo.MatchIngestionPolicy(ctx, "session", "session-999")
		if err != nil {
			t.Fatalf("match ingestion policy: %v", err)
		}
		if matched == nil || matched.ID != "pol_glob" {
			t.Fatalf("expected glob policy, got %+v", matched)
		}
	})

	t.Run("regex match mode", func(t *testing.T) {
		matched, err := repo.MatchIngestionPolicy(ctx, "agent", "sess_42")
		if err != nil {
			t.Fatalf("match ingestion policy: %v", err)
		}
		if matched == nil || matched.ID != "pol_regex" {
			t.Fatalf("expected regex policy, got %+v", matched)
		}
	})

	t.Run("invalid regex does not crash and does not match", func(t *testing.T) {
		matched, err := repo.MatchIngestionPolicy(ctx, "project", "anything")
		if err != nil {
			t.Fatalf("match ingestion policy: %v", err)
		}
		if matched != nil {
			t.Fatalf("expected nil for invalid regex policy, got %+v", matched)
		}
	})

	t.Run("higher priority policy wins", func(t *testing.T) {
		matched, err := repo.MatchIngestionPolicy(ctx, "surface", "ui-dashboard")
		if err != nil {
			t.Fatalf("match ingestion policy: %v", err)
		}
		if matched == nil || matched.ID != "pol_high_priority" {
			t.Fatalf("expected higher priority policy, got %+v", matched)
		}
	})

	none, err := repo.MatchIngestionPolicy(ctx, "source", "random")
	if err != nil {
		t.Fatalf("match ingestion policy for non-match: %v", err)
	}
	if none != nil {
		t.Fatalf("expected nil for non-match, got %+v", none)
	}
}

func TestProjectsCRUD(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	project := &core.Project{
		ID:          "prj_test",
		Name:        "amm",
		Path:        "/tmp/amm",
		Description: "memory manager",
		Metadata:    map[string]string{"env": "test"},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := repo.InsertProject(ctx, project); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	got, err := repo.GetProject(ctx, project.ID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if got.Name != project.Name || got.Path != project.Path || got.Metadata["env"] != "test" {
		t.Fatalf("unexpected project: %+v", got)
	}

	projects, err := repo.ListProjects(ctx)
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	if len(projects) != 1 || projects[0].ID != project.ID {
		t.Fatalf("unexpected projects: %+v", projects)
	}

	if err := repo.DeleteProject(ctx, project.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	if _, err := repo.GetProject(ctx, project.ID); err == nil {
		t.Fatal("expected get project after delete to fail")
	}
}

func TestRelationshipsCRUD(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	from := &core.Entity{ID: "ent_from", Type: "person", CanonicalName: "Alice", CreatedAt: now, UpdatedAt: now}
	to := &core.Entity{ID: "ent_to", Type: "service", CanonicalName: "AMM", CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEntity(ctx, from); err != nil {
		t.Fatalf("insert from entity: %v", err)
	}
	if err := repo.InsertEntity(ctx, to); err != nil {
		t.Fatalf("insert to entity: %v", err)
	}

	rel := &core.Relationship{
		ID:               "rel_test",
		FromEntityID:     from.ID,
		ToEntityID:       to.ID,
		RelationshipType: "owns",
		Metadata:         map[string]string{"source": "test"},
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.InsertRelationship(ctx, rel); err != nil {
		t.Fatalf("insert relationship: %v", err)
	}

	got, err := repo.GetRelationship(ctx, rel.ID)
	if err != nil {
		t.Fatalf("get relationship: %v", err)
	}
	if got.FromEntityID != from.ID || got.ToEntityID != to.ID || got.RelationshipType != "owns" {
		t.Fatalf("unexpected relationship: %+v", got)
	}

	byEntity, err := repo.ListRelationships(ctx, core.ListRelationshipsOptions{EntityID: from.ID, Limit: 10})
	if err != nil {
		t.Fatalf("list relationships by entity: %v", err)
	}
	if len(byEntity) != 1 || byEntity[0].ID != rel.ID {
		t.Fatalf("unexpected relationships by entity: %+v", byEntity)
	}

	byType, err := repo.ListRelationships(ctx, core.ListRelationshipsOptions{RelationshipType: "owns", Limit: 10})
	if err != nil {
		t.Fatalf("list relationships by type: %v", err)
	}
	if len(byType) != 1 || byType[0].ID != rel.ID {
		t.Fatalf("unexpected relationships by type: %+v", byType)
	}

	if err := repo.DeleteRelationship(ctx, rel.ID); err != nil {
		t.Fatalf("delete relationship: %v", err)
	}
	if _, err := repo.GetRelationship(ctx, rel.ID); err == nil {
		t.Fatal("expected get relationship after delete to fail")
	}
}

func TestEmbeddingsRoundTrip(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	first := &core.EmbeddingRecord{
		ObjectID:   "mem_embed_1",
		ObjectKind: "memory",
		Model:      "local-test-v1",
		Vector:     []float32{0.1, 0.2, 0.3},
		CreatedAt:  now,
	}
	if err := repo.UpsertEmbedding(ctx, first); err != nil {
		t.Fatalf("upsert embedding: %v", err)
	}

	got, err := repo.GetEmbedding(ctx, first.ObjectID, first.ObjectKind, first.Model)
	if err != nil {
		t.Fatalf("get embedding: %v", err)
	}
	if got.ObjectID != first.ObjectID || got.ObjectKind != first.ObjectKind || got.Model != first.Model {
		t.Fatalf("unexpected embedding identity: %+v", got)
	}
	if len(got.Vector) != 3 || got.Vector[0] != 0.1 || got.Vector[2] != 0.3 {
		t.Fatalf("unexpected embedding vector: %#v", got.Vector)
	}

	updated := &core.EmbeddingRecord{
		ObjectID:   first.ObjectID,
		ObjectKind: first.ObjectKind,
		Model:      first.Model,
		Vector:     []float32{0.9, 0.8},
		CreatedAt:  now.Add(time.Minute),
	}
	if err := repo.UpsertEmbedding(ctx, updated); err != nil {
		t.Fatalf("upsert embedding update: %v", err)
	}

	gotUpdated, err := repo.GetEmbedding(ctx, updated.ObjectID, updated.ObjectKind, updated.Model)
	if err != nil {
		t.Fatalf("get updated embedding: %v", err)
	}
	if len(gotUpdated.Vector) != 2 || gotUpdated.Vector[0] != 0.9 || gotUpdated.Vector[1] != 0.8 {
		t.Fatalf("unexpected updated vector: %#v", gotUpdated.Vector)
	}

	secondModel := &core.EmbeddingRecord{
		ObjectID:   first.ObjectID,
		ObjectKind: first.ObjectKind,
		Model:      "local-test-v2",
		Vector:     []float32{0.4},
		CreatedAt:  now,
	}
	if err := repo.UpsertEmbedding(ctx, secondModel); err != nil {
		t.Fatalf("upsert second model embedding: %v", err)
	}

	if err := repo.DeleteEmbeddings(ctx, first.ObjectID, first.ObjectKind, first.Model); err != nil {
		t.Fatalf("delete single model embedding: %v", err)
	}
	if _, err := repo.GetEmbedding(ctx, first.ObjectID, first.ObjectKind, first.Model); err == nil {
		t.Fatal("expected deleted model embedding lookup to fail")
	}
	if _, err := repo.GetEmbedding(ctx, secondModel.ObjectID, secondModel.ObjectKind, secondModel.Model); err != nil {
		t.Fatalf("expected other model embedding to remain: %v", err)
	}

	if err := repo.DeleteEmbeddings(ctx, secondModel.ObjectID, secondModel.ObjectKind, ""); err != nil {
		t.Fatalf("delete all object embeddings: %v", err)
	}
	if _, err := repo.GetEmbedding(ctx, secondModel.ObjectID, secondModel.ObjectKind, secondModel.Model); err == nil {
		t.Fatal("expected all object embeddings to be deleted")
	}
}
