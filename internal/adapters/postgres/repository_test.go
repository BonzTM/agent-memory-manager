package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func nowUTC() time.Time { return time.Now().UTC().Truncate(time.Second) }

func resetPublicSchema(ctx context.Context, repo *Repository) error {
	_, err := repo.db.ExecContext(ctx, `DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;`)
	return err
}

func testRepo(t *testing.T) (*Repository, func()) {
	t.Helper()
	dsn := os.Getenv("AMM_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set AMM_TEST_POSTGRES_DSN to run Postgres tests")
	}
	repo := NewRepository()
	ctx := context.Background()
	if err := repo.Open(ctx, dsn); err != nil {
		t.Fatalf("open repo: %v", err)
	}
	if err := resetPublicSchema(ctx, repo); err != nil {
		_ = repo.Close()
		t.Fatalf("reset schema: %v", err)
	}
	if err := repo.Migrate(ctx); err != nil {
		_ = repo.Close()
		t.Fatalf("migrate repo: %v", err)
	}
	cleanup := func() { _ = repo.Close() }
	t.Cleanup(cleanup)
	return repo, cleanup
}

func migrateThroughVersion(t *testing.T, repo *Repository, targetVersion int) {
	t.Helper()

	ctx := context.Background()
	if _, err := repo.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		t.Fatalf("create schema_version table: %v", err)
	}

	for _, m := range migrations {
		if m.Version > targetVersion {
			break
		}
		tx, err := repo.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin migration %d: %v", m.Version, err)
		}
		if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
			_ = tx.Rollback()
			t.Fatalf("exec migration %d: %v", m.Version, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version (version) VALUES ($1)`, m.Version); err != nil {
			_ = tx.Rollback()
			t.Fatalf("record migration %d: %v", m.Version, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatalf("commit migration %d: %v", m.Version, err)
		}
	}
}

type stubScanner struct {
	values []any
}

func (s stubScanner) Scan(dest ...any) error {
	if len(dest) != len(s.values) {
		return fmt.Errorf("destination count %d != value count %d", len(dest), len(s.values))
	}
	for i := range dest {
		if err := assignScanValue(dest[i], s.values[i]); err != nil {
			return fmt.Errorf("scan column %d: %w", i, err)
		}
	}
	return nil
}

func assignScanValue(dest, src any) error {
	switch d := dest.(type) {
	case *sql.NullTime:
		if src == nil {
			*d = sql.NullTime{}
			return nil
		}
		t, ok := src.(time.Time)
		if !ok {
			return fmt.Errorf("null time source %T", src)
		}
		*d = sql.NullTime{Time: t, Valid: true}
		return nil
	case *[]byte:
		switch v := src.(type) {
		case nil:
			*d = nil
		case []byte:
			*d = append([]byte(nil), v...)
		case string:
			*d = []byte(v)
		default:
			return fmt.Errorf("[]byte source %T", src)
		}
		return nil
	}

	if scanner, ok := dest.(sql.Scanner); ok {
		return scanner.Scan(src)
	}

	dv := reflect.ValueOf(dest)
	if dv.Kind() != reflect.Ptr || dv.IsNil() {
		return fmt.Errorf("destination %T is not a writable pointer", dest)
	}

	elem := dv.Elem()
	if src == nil {
		elem.Set(reflect.Zero(elem.Type()))
		return nil
	}

	sv := reflect.ValueOf(src)
	if sv.Type().AssignableTo(elem.Type()) {
		elem.Set(sv)
		return nil
	}
	if sv.Type().ConvertibleTo(elem.Type()) {
		elem.Set(sv.Convert(elem.Type()))
		return nil
	}
	return fmt.Errorf("cannot assign %T to %T", src, dest)
}

func mustInsertEvent(t *testing.T, repo *Repository, evt *core.Event) {
	t.Helper()
	if err := repo.InsertEvent(context.Background(), evt); err != nil {
		t.Fatalf("insert event %s: %v", evt.ID, err)
	}
}

func TestMigrateCreatesTables(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()

	tables := []string{
		"events", "memories", "summaries", "summary_edges", "claims", "entities", "memory_entities",
		"relationships", "entity_graph_projection", "episodes", "artifacts", "jobs", "ingestion_policies",
		"recall_history", "relevance_feedback", "embeddings", "projects", "schema_version",
	}
	for _, table := range tables {
		var exists bool
		err := repo.db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_schema = 'public' AND table_name = $1
			)`, table).Scan(&exists)
		if err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("expected table %s to exist", table)
		}
	}
}

func TestMigrateSeedsDefaultToolIgnorePolicy(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()

	policies, err := repo.ListIngestionPolicies(ctx)
	if err != nil {
		t.Fatalf("ListIngestionPolicies: %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("expected exactly one seeded policy on fresh migrate, got %d", len(policies))
	}
	policy := policies[0]
	if policy.ID != "pol_default_tool_events_ignore" || policy.PatternType != "kind" || policy.Pattern != "tool_*" || policy.Mode != "ignore" || policy.MatchMode != "glob" || policy.Priority != 100 {
		t.Fatalf("unexpected seeded policy: %+v", policy)
	}
}

func TestMigrateDoesNotSeedDefaultToolIgnorePolicyWhenPoliciesExist(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	if err := resetPublicSchema(ctx, repo); err != nil {
		t.Fatalf("reset schema: %v", err)
	}
	migrateThroughVersion(t, repo, 3)

	now := nowUTC()
	custom := &core.IngestionPolicy{
		ID:          "pol_custom_existing",
		PatternType: "source",
		Pattern:     "ci-bot",
		Mode:        "ignore",
		Priority:    5,
		MatchMode:   "glob",
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := repo.InsertIngestionPolicy(ctx, custom); err != nil {
		t.Fatalf("InsertIngestionPolicy: %v", err)
	}

	if err := repo.Migrate(ctx); err != nil {
		t.Fatalf("Migrate upgrade: %v", err)
	}

	policies, err := repo.ListIngestionPolicies(ctx)
	if err != nil {
		t.Fatalf("ListIngestionPolicies: %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("expected existing custom policy set to remain untouched, got %d policies", len(policies))
	}
	if policies[0].ID != custom.ID {
		t.Fatalf("expected custom policy to remain the only policy, got %+v", policies[0])
	}
}

func TestRepositoryEvents(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	now := nowUTC()

	e1 := &core.Event{ID: "evt_1", Kind: "message_user", SourceSystem: "cli", SessionID: "sess_a", ProjectID: "proj_a", PrivacyLevel: core.PrivacyPrivate, Content: "alpha event", Metadata: map[string]string{"k": "v"}, OccurredAt: now.Add(-3 * time.Minute), IngestedAt: now}
	e2 := &core.Event{ID: "evt_2", Kind: "message_assistant", SourceSystem: "cli", SessionID: "", ProjectID: "proj_a", PrivacyLevel: core.PrivacyPrivate, Content: "beta event", OccurredAt: now.Add(-2 * time.Minute), IngestedAt: now}
	e3 := &core.Event{ID: "evt_3", Kind: "message_user", SourceSystem: "cli", SessionID: "", ProjectID: "proj_b", PrivacyLevel: core.PrivacyPrivate, Content: "gamma findme", OccurredAt: now.Add(-1 * time.Minute), IngestedAt: now}
	mustInsertEvent(t, repo, e1)
	mustInsertEvent(t, repo, e2)
	mustInsertEvent(t, repo, e3)

	got, err := repo.GetEvent(ctx, e1.ID)
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if got.Metadata["k"] != "v" || got.Content != "alpha event" {
		t.Fatalf("GetEvent roundtrip mismatch: %+v", got)
	}

	e1.Content = "alpha updated"
	rt := now.Add(5 * time.Minute)
	e1.ReflectedAt = &rt
	if err := repo.UpdateEvent(ctx, e1); err != nil {
		t.Fatalf("UpdateEvent: %v", err)
	}
	updated, err := repo.GetEvent(ctx, e1.ID)
	if err != nil {
		t.Fatalf("GetEvent updated: %v", err)
	}
	if updated.Content != "alpha updated" || updated.ReflectedAt == nil {
		t.Fatalf("event not updated: %+v", updated)
	}

	list, err := repo.ListEvents(ctx, core.ListEventsOptions{SessionID: "sess_a", Limit: 10})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	// Only e1 has sess_a; e2 is sessionless.
	if len(list) != 1 {
		t.Fatalf("expected 1 event for session sess_a, got %d", len(list))
	}

	searched, err := repo.SearchEvents(ctx, "findme", 10)
	if err != nil {
		t.Fatalf("SearchEvents: %v", err)
	}
	if len(searched) == 0 || searched[0].ID != "evt_3" {
		t.Fatalf("expected evt_3 in search results, got %#v", searched)
	}

	unreflected, err := repo.CountUnreflectedEvents(ctx)
	if err != nil {
		t.Fatalf("CountUnreflectedEvents: %v", err)
	}
	if unreflected != 2 {
		t.Fatalf("expected 2 unreflected events, got %d", unreflected)
	}

	claimed, err := repo.ClaimUnreflectedEvents(ctx, 1)
	if err != nil {
		t.Fatalf("ClaimUnreflectedEvents: %v", err)
	}
	if len(claimed) != 1 {
		t.Fatalf("expected 1 claimed event, got %d", len(claimed))
	}
	if claimed[0].ReflectedAt == nil {
		t.Fatal("expected claimed event payload to include reflected_at")
	}

	postClaimCount, err := repo.CountUnreflectedEvents(ctx)
	if err != nil {
		t.Fatalf("CountUnreflectedEvents post-claim: %v", err)
	}
	if postClaimCount != 1 {
		t.Fatalf("expected 1 unreflected after claim, got %d", postClaimCount)
	}
}

func TestScanMemoryReadsStringArrays(t *testing.T) {
	repo := NewRepository()
	now := nowUTC()
	observedAt := now.Add(-time.Hour)
	validFrom := now.Add(-30 * time.Minute)
	validTo := now.Add(30 * time.Minute)
	lastConfirmedAt := now.Add(-5 * time.Minute)
	supersededAt := now.Add(45 * time.Minute)

	got, err := repo.scanMemory(stubScanner{values: []any{
		"mem_scan",
		string(core.MemoryTypeFact),
		string(core.ScopeProject),
		"proj_scan",
		"sess_scan",
		"agent_scan",
		"subject",
		"body",
		"tight",
		0.9,
		0.7,
		string(core.PrivacyPrivate),
		string(core.MemoryStatusActive),
		observedAt,
		now,
		now.Add(time.Minute),
		validFrom,
		validTo,
		lastConfirmedAt,
		"mem_old",
		"mem_new",
		supersededAt,
		`{"evt_1","evt_2"}`,
		`{"sum_1"}`,
		`{"art_1"}`,
		`{"tag_a","tag_b"}`,
		`{"k":"v"}`,
	}})
	if err != nil {
		t.Fatalf("scanMemory: %v", err)
	}

	if !reflect.DeepEqual(got.SourceEventIDs, []string{"evt_1", "evt_2"}) {
		t.Fatalf("unexpected SourceEventIDs: %#v", got.SourceEventIDs)
	}
	if !reflect.DeepEqual(got.SourceSummaryIDs, []string{"sum_1"}) {
		t.Fatalf("unexpected SourceSummaryIDs: %#v", got.SourceSummaryIDs)
	}
	if !reflect.DeepEqual(got.SourceArtifactIDs, []string{"art_1"}) {
		t.Fatalf("unexpected SourceArtifactIDs: %#v", got.SourceArtifactIDs)
	}
	if !reflect.DeepEqual(got.Tags, []string{"tag_a", "tag_b"}) {
		t.Fatalf("unexpected Tags: %#v", got.Tags)
	}
	if got.Metadata["k"] != "v" {
		t.Fatalf("unexpected Metadata: %#v", got.Metadata)
	}
	if got.ObservedAt == nil || !got.ObservedAt.Equal(observedAt) {
		t.Fatalf("unexpected ObservedAt: %#v", got.ObservedAt)
	}
	if got.ValidFrom == nil || !got.ValidFrom.Equal(validFrom) {
		t.Fatalf("unexpected ValidFrom: %#v", got.ValidFrom)
	}
	if got.ValidTo == nil || !got.ValidTo.Equal(validTo) {
		t.Fatalf("unexpected ValidTo: %#v", got.ValidTo)
	}
	if got.LastConfirmedAt == nil || !got.LastConfirmedAt.Equal(lastConfirmedAt) {
		t.Fatalf("unexpected LastConfirmedAt: %#v", got.LastConfirmedAt)
	}
	if got.SupersededAt == nil || !got.SupersededAt.Equal(supersededAt) {
		t.Fatalf("unexpected SupersededAt: %#v", got.SupersededAt)
	}
}

func TestScanEntityReadsAliases(t *testing.T) {
	repo := NewRepository()
	now := nowUTC()

	got, err := repo.scanEntity(stubScanner{values: []any{
		"ent_scan",
		"system",
		"Postgres",
		`{"postgresql","pg"}`,
		"database",
		`{"team":"storage"}`,
		now,
		now.Add(time.Minute),
	}})
	if err != nil {
		t.Fatalf("scanEntity: %v", err)
	}

	if !reflect.DeepEqual(got.Aliases, []string{"postgresql", "pg"}) {
		t.Fatalf("unexpected Aliases: %#v", got.Aliases)
	}
	if got.Metadata["team"] != "storage" {
		t.Fatalf("unexpected Metadata: %#v", got.Metadata)
	}
}

func TestScanEpisodeReadsStringArrays(t *testing.T) {
	repo := NewRepository()
	now := nowUTC()
	startedAt := now.Add(-2 * time.Hour)
	endedAt := now.Add(-time.Hour)

	got, err := repo.scanEpisode(stubScanner{values: []any{
		"epi_scan",
		"Episode Scan",
		"summary",
		"tight",
		string(core.ScopeProject),
		"proj_epi",
		"sess_epi",
		0.8,
		string(core.PrivacyPrivate),
		startedAt,
		endedAt,
		`{"event_ids":["evt_1"],"summary_ids":["sum_0"]}`,
		`{"sum_1","sum_2"}`,
		`{"alice","bob"}`,
		`{"ent_1"}`,
		`{"done"}`,
		`{"follow_up"}`,
		`{"kind":"incident"}`,
		now,
		now.Add(time.Minute),
	}})
	if err != nil {
		t.Fatalf("scanEpisode: %v", err)
	}

	if !reflect.DeepEqual(got.SourceSummaryIDs, []string{"sum_1", "sum_2"}) {
		t.Fatalf("unexpected SourceSummaryIDs: %#v", got.SourceSummaryIDs)
	}
	if !reflect.DeepEqual(got.Participants, []string{"alice", "bob"}) {
		t.Fatalf("unexpected Participants: %#v", got.Participants)
	}
	if !reflect.DeepEqual(got.RelatedEntities, []string{"ent_1"}) {
		t.Fatalf("unexpected RelatedEntities: %#v", got.RelatedEntities)
	}
	if !reflect.DeepEqual(got.Outcomes, []string{"done"}) {
		t.Fatalf("unexpected Outcomes: %#v", got.Outcomes)
	}
	if !reflect.DeepEqual(got.UnresolvedItems, []string{"follow_up"}) {
		t.Fatalf("unexpected UnresolvedItems: %#v", got.UnresolvedItems)
	}
	if got.SourceSpan.EventIDs[0] != "evt_1" || got.SourceSpan.SummaryIDs[0] != "sum_0" {
		t.Fatalf("unexpected SourceSpan: %#v", got.SourceSpan)
	}
	if got.Metadata["kind"] != "incident" {
		t.Fatalf("unexpected Metadata: %#v", got.Metadata)
	}
	if got.StartedAt == nil || !got.StartedAt.Equal(startedAt) {
		t.Fatalf("unexpected StartedAt: %#v", got.StartedAt)
	}
	if got.EndedAt == nil || !got.EndedAt.Equal(endedAt) {
		t.Fatalf("unexpected EndedAt: %#v", got.EndedAt)
	}
}

func TestRepositoryMemories(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	now := nowUTC()

	mustInsertEvent(t, repo, &core.Event{ID: "evt_mem_1", Kind: "message_user", SourceSystem: "cli", PrivacyLevel: core.PrivacyPrivate, Content: "source event one", OccurredAt: now, IngestedAt: now})
	mustInsertEvent(t, repo, &core.Event{ID: "evt_mem_2", Kind: "message_user", SourceSystem: "cli", PrivacyLevel: core.PrivacyPrivate, Content: "source event two", OccurredAt: now.Add(time.Second), IngestedAt: now})

	m1 := &core.Memory{ID: "mem_1", Type: core.MemoryTypeFact, Scope: core.ScopeProject, ProjectID: "proj_mem", AgentID: "agent-1", Subject: "db", Body: "postgres memory alpha", TightDescription: "alpha", Confidence: 0.9, Importance: 0.7, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, SourceEventIDs: []string{"evt_mem_1", "evt_mem_2"}, SourceSummaryIDs: []string{"sum_mem_1"}, SourceArtifactIDs: []string{"art_mem_1"}, Tags: []string{"db", "primary"}, Metadata: map[string]string{"a": "1"}, CreatedAt: now, UpdatedAt: now}
	m2 := &core.Memory{ID: "mem_2", Type: core.MemoryTypeDecision, Scope: core.ScopeProject, ProjectID: "proj_mem", AgentID: "agent-2", Subject: "cache", Body: "postgres memory beta", TightDescription: "beta", Confidence: 0.8, Importance: 0.6, PrivacyLevel: core.PrivacyShared, Status: core.MemoryStatusActive, SourceEventIDs: []string{"evt_mem_2"}, SourceSummaryIDs: []string{"sum_mem_2"}, SourceArtifactIDs: []string{"art_mem_2"}, Tags: []string{"cache"}, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	m3 := &core.Memory{ID: "mem_3", Type: core.MemoryTypeFact, Scope: core.ScopeProject, ProjectID: "proj_mem", Body: "archived memory", TightDescription: "gamma", Confidence: 0.7, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusArchived, SourceEventIDs: []string{}, SourceSummaryIDs: []string{}, SourceArtifactIDs: []string{}, Tags: []string{}, CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second)}

	for _, m := range []*core.Memory{m1, m2, m3} {
		if err := repo.InsertMemory(ctx, m); err != nil {
			t.Fatalf("InsertMemory %s: %v", m.ID, err)
		}
	}

	got, err := repo.GetMemory(ctx, "mem_1")
	if err != nil {
		t.Fatalf("GetMemory: %v", err)
	}
	if got.Subject != "db" || got.Metadata["a"] != "1" {
		t.Fatalf("GetMemory mismatch: %+v", got)
	}
	if !reflect.DeepEqual(got.SourceEventIDs, []string{"evt_mem_1", "evt_mem_2"}) || !reflect.DeepEqual(got.SourceSummaryIDs, []string{"sum_mem_1"}) || !reflect.DeepEqual(got.Tags, []string{"db", "primary"}) {
		t.Fatalf("GetMemory arrays mismatch: %+v", got)
	}

	batch, err := repo.GetMemoriesByIDs(ctx, []string{"mem_1", "mem_2", "missing"})
	if err != nil {
		t.Fatalf("GetMemoriesByIDs: %v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("expected 2 memories in batch, got %d", len(batch))
	}

	// ListMemoriesBySourceEventIDs only returns active memories, so test
	// before we supersede mem_1 below.
	bySource, err := repo.ListMemoriesBySourceEventIDs(ctx, []string{"evt_mem_1"})
	if err != nil {
		t.Fatalf("ListMemoriesBySourceEventIDs: %v", err)
	}
	if len(bySource) != 1 || bySource[0].ID != "mem_1" {
		t.Fatalf("unexpected source lookup result: %#v", bySource)
	}

	emptyBySource, err := repo.ListMemoriesBySourceEventIDs(ctx, []string{})
	if err != nil {
		t.Fatalf("ListMemoriesBySourceEventIDs empty: %v", err)
	}
	if len(emptyBySource) != 0 {
		t.Fatalf("expected empty result for empty source lookup, got %#v", emptyBySource)
	}

	m1.Body = "postgres memory alpha updated"
	m1.UpdatedAt = now.Add(3 * time.Second)
	m1.Status = core.MemoryStatusSuperseded
	if err := repo.UpdateMemory(ctx, m1); err != nil {
		t.Fatalf("UpdateMemory: %v", err)
	}
	upd, err := repo.GetMemory(ctx, "mem_1")
	if err != nil {
		t.Fatalf("GetMemory updated: %v", err)
	}
	if upd.Body != "postgres memory alpha updated" || upd.Status != core.MemoryStatusSuperseded {
		t.Fatalf("updated memory mismatch: %+v", upd)
	}

	// After superseding mem_1, source lookup for evt_mem_1 should return
	// empty — the function only returns active memories.
	bySourceAfter, err := repo.ListMemoriesBySourceEventIDs(ctx, []string{"evt_mem_1"})
	if err != nil {
		t.Fatalf("ListMemoriesBySourceEventIDs after supersede: %v", err)
	}
	if len(bySourceAfter) != 0 {
		t.Fatalf("expected empty result after superseding mem_1, got %#v", bySourceAfter)
	}

	listed, err := repo.ListMemories(ctx, core.ListMemoriesOptions{Scope: core.ScopeProject, ProjectID: "proj_mem", Status: core.MemoryStatusActive, Limit: 10})
	if err != nil {
		t.Fatalf("ListMemories: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "mem_2" {
		t.Fatalf("unexpected ListMemories result: %#v", listed)
	}

	fts, err := repo.SearchMemories(ctx, "beta", core.ListMemoriesOptions{AgentID: "agent-2", Limit: 10})
	if err != nil {
		t.Fatalf("SearchMemories: %v", err)
	}
	if len(fts) == 0 || fts[0].ID != "mem_2" {
		t.Fatalf("unexpected SearchMemories result: %#v", fts)
	}

	fuzzy, err := repo.SearchMemoriesFuzzy(ctx, "alpha updated", core.ListMemoriesOptions{Status: core.MemoryStatusSuperseded, Type: core.MemoryTypeFact, Scope: core.ScopeProject, ProjectID: "proj_mem", Limit: 10})
	if err != nil {
		t.Fatalf("SearchMemoriesFuzzy: %v", err)
	}
	if len(fuzzy) != 1 || fuzzy[0].ID != "mem_1" {
		t.Fatalf("unexpected SearchMemoriesFuzzy result: %#v", fuzzy)
	}

	activeCount, err := repo.CountActiveMemories(ctx)
	if err != nil {
		t.Fatalf("CountActiveMemories: %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("expected 1 active memory, got %d", activeCount)
	}
}

func TestInsertMemoryNilSlices(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	now := nowUTC()

	m := &core.Memory{
		ID:               "mem_nil_slices",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "memory with nil slices",
		TightDescription: "nil slices test",
		Confidence:       0.8,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := repo.InsertMemory(ctx, m); err != nil {
		t.Fatalf("InsertMemory with nil slices: %v", err)
	}

	got, err := repo.GetMemory(ctx, "mem_nil_slices")
	if err != nil {
		t.Fatalf("GetMemory after nil-slice insert: %v", err)
	}
	if got.Body != "memory with nil slices" {
		t.Fatalf("body mismatch: %q", got.Body)
	}
	if got.SourceEventIDs == nil {
		t.Fatal("expected non-nil SourceEventIDs after round-trip")
	}
	if len(got.SourceEventIDs) != 0 {
		t.Fatalf("expected empty SourceEventIDs, got %v", got.SourceEventIDs)
	}

	m.Body = "updated with nil slices"
	m.UpdatedAt = now.Add(time.Second)
	if err := repo.UpdateMemory(ctx, m); err != nil {
		t.Fatalf("UpdateMemory with nil slices: %v", err)
	}
}

func TestEmptyIfNil(t *testing.T) {
	if got := emptyIfNil(nil); got == nil || len(got) != 0 {
		t.Fatalf("emptyIfNil(nil) = %v, want empty non-nil slice", got)
	}
	input := []string{"a", "b"}
	if got := emptyIfNil(input); len(got) != 2 || got[0] != "a" {
		t.Fatalf("emptyIfNil(non-nil) = %v, want %v", got, input)
	}
}

func TestRepositorySummaries(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	now := nowUTC()

	s1 := &core.Summary{ID: "sum_1", Kind: "leaf", Scope: core.ScopeProject, ProjectID: "proj_sum", SessionID: "sess_sum", AgentID: "agent", Title: "title one", Body: "summary alpha", TightDescription: "alpha desc", PrivacyLevel: core.PrivacyPrivate, SourceSpan: core.SourceSpan{EventIDs: []string{"evt_1"}}, Metadata: map[string]string{"k": "v"}, CreatedAt: now, UpdatedAt: now}
	s2 := &core.Summary{ID: "sum_2", Kind: "leaf", Scope: core.ScopeProject, ProjectID: "proj_sum", Body: "summary beta keyword", TightDescription: "beta", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	for _, s := range []*core.Summary{s1, s2} {
		if err := repo.InsertSummary(ctx, s); err != nil {
			t.Fatalf("InsertSummary %s: %v", s.ID, err)
		}
	}

	got, err := repo.GetSummary(ctx, "sum_1")
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	if got.Title != "title one" || got.Metadata["k"] != "v" {
		t.Fatalf("GetSummary mismatch: %+v", got)
	}

	list, err := repo.ListSummaries(ctx, core.ListSummariesOptions{Scope: core.ScopeProject, ProjectID: "proj_sum", Limit: 10})
	if err != nil {
		t.Fatalf("ListSummaries: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(list))
	}

	search, err := repo.SearchSummaries(ctx, "keyword", 10)
	if err != nil {
		t.Fatalf("SearchSummaries: %v", err)
	}
	if len(search) == 0 || search[0].ID != "sum_2" {
		t.Fatalf("unexpected SearchSummaries results: %#v", search)
	}

	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: "sum_1", ChildKind: "summary", ChildID: "sum_2", EdgeOrder: 2}); err != nil {
		t.Fatalf("InsertSummaryEdge: %v", err)
	}
	children, err := repo.GetSummaryChildren(ctx, "sum_1")
	if err != nil {
		t.Fatalf("GetSummaryChildren: %v", err)
	}
	if len(children) != 1 || children[0].ChildID != "sum_2" {
		t.Fatalf("unexpected children: %#v", children)
	}

	parented, err := repo.ListParentedSummaryIDs(ctx)
	if err != nil {
		t.Fatalf("ListParentedSummaryIDs: %v", err)
	}
	if !parented["sum_2"] {
		t.Fatalf("expected sum_2 to be marked parented: %#v", parented)
	}
}

func TestGetSummaryParents_ReturnsParentEdges(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	now := nowUTC()

	parent := &core.Summary{ID: "sum_parent_single", Kind: "session", Scope: core.ScopeProject, ProjectID: "proj_parent_single", Body: "parent", TightDescription: "parent", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}
	child := &core.Summary{ID: "sum_child_single", Kind: "leaf", Scope: core.ScopeProject, ProjectID: "proj_parent_single", Body: "child", TightDescription: "child", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}

	if err := repo.InsertSummary(ctx, parent); err != nil {
		t.Fatalf("InsertSummary parent: %v", err)
	}
	if err := repo.InsertSummary(ctx, child); err != nil {
		t.Fatalf("InsertSummary child: %v", err)
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: parent.ID, ChildKind: "summary", ChildID: child.ID, EdgeOrder: 1}); err != nil {
		t.Fatalf("InsertSummaryEdge: %v", err)
	}

	edges, err := repo.GetSummaryParents(ctx, child.ID)
	if err != nil {
		t.Fatalf("GetSummaryParents: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("expected 1 parent edge, got %#v", edges)
	}
	if edges[0].ParentSummaryID != parent.ID || edges[0].ChildID != child.ID || edges[0].ChildKind != "summary" {
		t.Fatalf("unexpected parent edge: %#v", edges[0])
	}
}

func TestGetSummaryParents_MultipleParents(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	now := nowUTC()

	parentOne := &core.Summary{ID: "sum_parent_one", Kind: "session", Scope: core.ScopeProject, ProjectID: "proj_parent_multi", Body: "parent one", TightDescription: "parent one", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}
	parentTwo := &core.Summary{ID: "sum_parent_two", Kind: "session", Scope: core.ScopeProject, ProjectID: "proj_parent_multi", Body: "parent two", TightDescription: "parent two", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	child := &core.Summary{ID: "sum_child_multi", Kind: "leaf", Scope: core.ScopeProject, ProjectID: "proj_parent_multi", Body: "child", TightDescription: "child", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second)}

	for _, summary := range []*core.Summary{parentOne, parentTwo, child} {
		if err := repo.InsertSummary(ctx, summary); err != nil {
			t.Fatalf("InsertSummary %s: %v", summary.ID, err)
		}
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: parentOne.ID, ChildKind: "summary", ChildID: child.ID, EdgeOrder: 1}); err != nil {
		t.Fatalf("InsertSummaryEdge one: %v", err)
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: parentTwo.ID, ChildKind: "summary", ChildID: child.ID, EdgeOrder: 2}); err != nil {
		t.Fatalf("InsertSummaryEdge two: %v", err)
	}

	edges, err := repo.GetSummaryParents(ctx, child.ID)
	if err != nil {
		t.Fatalf("GetSummaryParents: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("expected 2 parent edges, got %#v", edges)
	}
	if edges[0].ParentSummaryID != parentOne.ID || edges[1].ParentSummaryID != parentTwo.ID {
		t.Fatalf("unexpected parent edges order/content: %#v", edges)
	}
}

func TestGetSummaryParents_NoParents(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	now := nowUTC()

	orphan := &core.Summary{ID: "sum_orphan", Kind: "leaf", Scope: core.ScopeProject, ProjectID: "proj_orphan", Body: "orphan", TightDescription: "orphan", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertSummary(ctx, orphan); err != nil {
		t.Fatalf("InsertSummary orphan: %v", err)
	}

	edges, err := repo.GetSummaryParents(ctx, orphan.ID)
	if err != nil {
		t.Fatalf("GetSummaryParents: %v", err)
	}
	if len(edges) != 0 {
		t.Fatalf("expected no parent edges, got %#v", edges)
	}
}

func TestRepositoryEpisodes(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	now := nowUTC()

	e1 := &core.Episode{ID: "epi_1", Title: "Episode One", Summary: "episode alpha", TightDescription: "alpha", Scope: core.ScopeProject, ProjectID: "proj_epi", Importance: 0.7, PrivacyLevel: core.PrivacyPrivate, SourceSpan: core.SourceSpan{EventIDs: []string{"evt_1"}, SummaryIDs: []string{"sum_0"}}, SourceSummaryIDs: []string{"sum_1"}, Participants: []string{"alice", "bob"}, RelatedEntities: []string{"ent_1"}, Outcomes: []string{"resolved"}, UnresolvedItems: []string{"follow_up"}, CreatedAt: now, UpdatedAt: now}
	e2 := &core.Episode{ID: "epi_2", Title: "Episode Two", Summary: "episode beta keyword", TightDescription: "beta", Scope: core.ScopeProject, ProjectID: "proj_epi", Importance: 0.8, PrivacyLevel: core.PrivacyPrivate, SourceSummaryIDs: []string{"sum_2"}, Participants: []string{"carol"}, RelatedEntities: []string{"ent_2"}, Outcomes: []string{"logged"}, UnresolvedItems: []string{"owner"}, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	for _, e := range []*core.Episode{e1, e2} {
		if err := repo.InsertEpisode(ctx, e); err != nil {
			t.Fatalf("InsertEpisode %s: %v", e.ID, err)
		}
	}

	got, err := repo.GetEpisode(ctx, "epi_1")
	if err != nil {
		t.Fatalf("GetEpisode: %v", err)
	}
	if got.Title != "Episode One" {
		t.Fatalf("GetEpisode mismatch: %+v", got)
	}
	if !reflect.DeepEqual(got.SourceSummaryIDs, []string{"sum_1"}) || !reflect.DeepEqual(got.Participants, []string{"alice", "bob"}) || !reflect.DeepEqual(got.RelatedEntities, []string{"ent_1"}) {
		t.Fatalf("GetEpisode arrays mismatch: %+v", got)
	}

	list, err := repo.ListEpisodes(ctx, core.ListEpisodesOptions{Scope: core.ScopeProject, ProjectID: "proj_epi", Limit: 10})
	if err != nil {
		t.Fatalf("ListEpisodes: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 episodes, got %d", len(list))
	}

	search, err := repo.SearchEpisodes(ctx, "keyword", 10)
	if err != nil {
		t.Fatalf("SearchEpisodes: %v", err)
	}
	if len(search) == 0 || search[0].ID != "epi_2" {
		t.Fatalf("unexpected SearchEpisodes result: %#v", search)
	}
}

func TestRepositoryEntitiesAndLinks(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	now := nowUTC()

	for _, m := range []*core.Memory{
		{ID: "mem_link_1", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "memory link one", TightDescription: "one", Confidence: 0.7, Importance: 0.7, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, SourceEventIDs: []string{}, SourceSummaryIDs: []string{}, SourceArtifactIDs: []string{}, Tags: []string{}, CreatedAt: now, UpdatedAt: now},
		{ID: "mem_link_2", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "memory link two", TightDescription: "two", Confidence: 0.7, Importance: 0.7, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, SourceEventIDs: []string{}, SourceSummaryIDs: []string{}, SourceArtifactIDs: []string{}, Tags: []string{}, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)},
	} {
		if err := repo.InsertMemory(ctx, m); err != nil {
			t.Fatalf("InsertMemory %s: %v", m.ID, err)
		}
	}

	ent1 := &core.Entity{ID: "ent_1", Type: "person", CanonicalName: "Alice", Aliases: []string{"ally", "alicia"}, Description: "alpha person", Metadata: map[string]string{"team": "x"}, CreatedAt: now, UpdatedAt: now}
	ent2 := &core.Entity{ID: "ent_2", Type: "system", CanonicalName: "Postgres", Aliases: []string{"postgresql", "pg"}, Description: "database", CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	for _, e := range []*core.Entity{ent1, ent2} {
		if err := repo.InsertEntity(ctx, e); err != nil {
			t.Fatalf("InsertEntity %s: %v", e.ID, err)
		}
	}

	ent1.Description = "alpha person updated"
	ent1.UpdatedAt = now.Add(2 * time.Second)
	if err := repo.UpdateEntity(ctx, ent1); err != nil {
		t.Fatalf("UpdateEntity: %v", err)
	}
	got, err := repo.GetEntity(ctx, "ent_1")
	if err != nil {
		t.Fatalf("GetEntity: %v", err)
	}
	if got.Description != "alpha person updated" {
		t.Fatalf("entity not updated: %+v", got)
	}
	if !reflect.DeepEqual(got.Aliases, []string{"ally", "alicia"}) {
		t.Fatalf("entity aliases not round-tripped: %+v", got)
	}

	byIDs, err := repo.GetEntitiesByIDs(ctx, []string{"ent_1", "ent_2"})
	if err != nil {
		t.Fatalf("GetEntitiesByIDs: %v", err)
	}
	if len(byIDs) != 2 {
		t.Fatalf("expected 2 entities by ids, got %d", len(byIDs))
	}

	listed, err := repo.ListEntities(ctx, core.ListEntitiesOptions{Type: "person", Limit: 10})
	if err != nil {
		t.Fatalf("ListEntities: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != "ent_1" {
		t.Fatalf("unexpected ListEntities result: %#v", listed)
	}

	searched, err := repo.SearchEntities(ctx, "postgresql", 10)
	if err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}
	if len(searched) != 1 || searched[0].ID != "ent_2" {
		t.Fatalf("unexpected SearchEntities result: %#v", searched)
	}

	if err := repo.LinkMemoryEntity(ctx, "mem_link_1", "ent_1", "owner"); err != nil {
		t.Fatalf("LinkMemoryEntity: %v", err)
	}
	if err := repo.LinkMemoryEntitiesBatch(ctx, []core.MemoryEntityLink{{MemoryID: "mem_link_1", EntityID: "ent_2", Role: "mentions"}, {MemoryID: "mem_link_2", EntityID: "ent_1", Role: "mentions"}}); err != nil {
		t.Fatalf("LinkMemoryEntitiesBatch: %v", err)
	}

	mem1Entities, err := repo.GetMemoryEntities(ctx, "mem_link_1")
	if err != nil {
		t.Fatalf("GetMemoryEntities: %v", err)
	}
	if len(mem1Entities) != 2 {
		t.Fatalf("expected 2 entities linked to mem_link_1, got %d", len(mem1Entities))
	}

	batchLinks, err := repo.GetMemoryEntitiesBatch(ctx, []string{"mem_link_1", "mem_link_2"})
	if err != nil {
		t.Fatalf("GetMemoryEntitiesBatch: %v", err)
	}
	if len(batchLinks["mem_link_2"]) != 1 {
		t.Fatalf("expected mem_link_2 to have one entity, got %#v", batchLinks)
	}

	countEnt1, err := repo.CountMemoryEntityLinks(ctx, "ent_1")
	if err != nil {
		t.Fatalf("CountMemoryEntityLinks: %v", err)
	}
	if countEnt1 != 2 {
		t.Fatalf("expected 2 links for ent_1, got %d", countEnt1)
	}

	counts, err := repo.CountMemoryEntityLinksBatch(ctx, []string{"ent_1", "ent_2"})
	if err != nil {
		t.Fatalf("CountMemoryEntityLinksBatch: %v", err)
	}
	if counts["ent_2"] != 1 {
		t.Fatalf("expected 1 link for ent_2, got %d", counts["ent_2"])
	}
}

func TestRepositoryProjectsAndRelationships(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	now := nowUTC()

	p := &core.Project{ID: "prj_1", Name: "amm", Path: "/tmp/amm", Description: "project", Metadata: map[string]string{"lang": "go"}, CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertProject(ctx, p); err != nil {
		t.Fatalf("InsertProject: %v", err)
	}
	gp, err := repo.GetProject(ctx, p.ID)
	if err != nil {
		t.Fatalf("GetProject: %v", err)
	}
	if gp.Name != "amm" || gp.Metadata["lang"] != "go" {
		t.Fatalf("GetProject mismatch: %+v", gp)
	}
	pl, err := repo.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(pl) != 1 {
		t.Fatalf("expected 1 project, got %d", len(pl))
	}
	if err := repo.DeleteProject(ctx, p.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if _, err := repo.GetProject(ctx, p.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after DeleteProject, got %v", err)
	}

	for _, e := range []*core.Entity{
		{ID: "ent_rel_1", Type: "service", CanonicalName: "api", Aliases: []string{}, CreatedAt: now, UpdatedAt: now},
		{ID: "ent_rel_2", Type: "service", CanonicalName: "db", Aliases: []string{}, CreatedAt: now, UpdatedAt: now},
		{ID: "ent_rel_3", Type: "service", CanonicalName: "cache", Aliases: []string{}, CreatedAt: now, UpdatedAt: now},
	} {
		if err := repo.InsertEntity(ctx, e); err != nil {
			t.Fatalf("insert rel entity %s: %v", e.ID, err)
		}
	}

	r1 := &core.Relationship{ID: "rel_1", FromEntityID: "ent_rel_1", ToEntityID: "ent_rel_2", RelationshipType: "uses", Metadata: map[string]string{"k": "v"}, CreatedAt: now, UpdatedAt: now}
	r2 := &core.Relationship{ID: "rel_2", FromEntityID: "ent_rel_2", ToEntityID: "ent_rel_3", RelationshipType: "depends_on", CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	if err := repo.InsertRelationship(ctx, r1); err != nil {
		t.Fatalf("InsertRelationship: %v", err)
	}
	if err := repo.InsertRelationshipsBatch(ctx, []*core.Relationship{r2}); err != nil {
		t.Fatalf("InsertRelationshipsBatch: %v", err)
	}

	gr, err := repo.GetRelationship(ctx, r1.ID)
	if err != nil {
		t.Fatalf("GetRelationship: %v", err)
	}
	if gr.RelationshipType != "uses" {
		t.Fatalf("GetRelationship mismatch: %+v", gr)
	}

	list, err := repo.ListRelationships(ctx, core.ListRelationshipsOptions{EntityID: "ent_rel_2", RelationshipType: "uses", Limit: 10})
	if err != nil {
		t.Fatalf("ListRelationships: %v", err)
	}
	if len(list) != 1 || list[0].ID != "rel_1" {
		t.Fatalf("unexpected ListRelationships result: %#v", list)
	}

	byEntityIDs, err := repo.ListRelationshipsByEntityIDs(ctx, []string{"ent_rel_1", "ent_rel_3"})
	if err != nil {
		t.Fatalf("ListRelationshipsByEntityIDs: %v", err)
	}
	if len(byEntityIDs) != 2 {
		t.Fatalf("expected 2 relationships from list-by-entity-ids, got %d", len(byEntityIDs))
	}

	related, err := repo.ListRelatedEntities(ctx, "ent_rel_2", 2)
	if err != nil {
		t.Fatalf("ListRelatedEntities: %v", err)
	}
	if len(related) != 2 {
		t.Fatalf("expected 2 related entities, got %#v", related)
	}
	gotRelated := map[string]core.RelatedEntity{}
	for _, rel := range related {
		gotRelated[rel.Entity.ID] = rel
	}
	if gotRelated["ent_rel_1"].HopDistance != 1 || gotRelated["ent_rel_3"].HopDistance != 1 {
		t.Fatalf("expected hop distance 1 for directly related entities, got %#v", gotRelated)
	}

	if err := repo.RebuildEntityGraphProjection(ctx); err != nil {
		t.Fatalf("RebuildEntityGraphProjection: %v", err)
	}
	proj, err := repo.ListProjectedRelatedEntities(ctx, "ent_rel_2")
	if err != nil {
		t.Fatalf("ListProjectedRelatedEntities: %v", err)
	}
	if len(proj) < 2 {
		t.Fatalf("expected projected relations for ent_rel_2, got %#v", proj)
	}
	projByID := map[string]core.ProjectedRelation{}
	for _, rel := range proj {
		projByID[rel.RelatedEntityID] = rel
	}
	if projByID["ent_rel_1"].HopDistance != 1 || projByID["ent_rel_3"].HopDistance != 1 {
		t.Fatalf("expected 1-hop projected relations for direct neighbors, got %#v", projByID)
	}

	if err := repo.DeleteRelationship(ctx, "rel_1"); err != nil {
		t.Fatalf("DeleteRelationship: %v", err)
	}
	if _, err := repo.GetRelationship(ctx, "rel_1"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after DeleteRelationship, got %v", err)
	}
}

func TestRepositoryPoliciesAndJobs(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	now := nowUTC()

	policies := []*core.IngestionPolicy{
		{ID: "pol_1", PatternType: "source", Pattern: "svc-*", Mode: "full", Priority: 1, MatchMode: "glob", CreatedAt: now, UpdatedAt: now},
		{ID: "pol_2", PatternType: "source", Pattern: "^svc-prod-.*$", Mode: "read_only", Priority: 10, MatchMode: "regex", CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)},
	}
	for _, p := range policies {
		if err := repo.InsertIngestionPolicy(ctx, p); err != nil {
			t.Fatalf("InsertIngestionPolicy %s: %v", p.ID, err)
		}
	}

	gp, err := repo.GetIngestionPolicy(ctx, "pol_1")
	if err != nil {
		t.Fatalf("GetIngestionPolicy: %v", err)
	}
	if gp.Mode != "full" {
		t.Fatalf("GetIngestionPolicy mismatch: %+v", gp)
	}

	pl, err := repo.ListIngestionPolicies(ctx)
	if err != nil {
		t.Fatalf("ListIngestionPolicies: %v", err)
	}
	if len(pl) != 3 {
		t.Fatalf("expected 3 policies including the seeded default, got %d", len(pl))
	}
	idSet := map[string]bool{}
	for _, policy := range pl {
		idSet[policy.ID] = true
	}
	if !idSet["pol_default_tool_events_ignore"] || !idSet["pol_1"] || !idSet["pol_2"] {
		t.Fatalf("expected default and inserted policies, got ids=%v", idSet)
	}

	matched, err := repo.MatchIngestionPolicy(ctx, "source", "svc-prod-api")
	if err != nil {
		t.Fatalf("MatchIngestionPolicy: %v", err)
	}
	if matched == nil || matched.ID != "pol_2" {
		t.Fatalf("expected highest-priority regex policy pol_2, got %+v", matched)
	}

	if err := repo.DeleteIngestionPolicy(ctx, "pol_1"); err != nil {
		t.Fatalf("DeleteIngestionPolicy: %v", err)
	}
	if _, err := repo.GetIngestionPolicy(ctx, "pol_1"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after policy delete, got %v", err)
	}

	job := &core.Job{ID: "job_1", Kind: "reflect", Status: "pending", Payload: map[string]string{"a": "1"}, Result: map[string]string{"r": "0"}, CreatedAt: now}
	if err := repo.InsertJob(ctx, job); err != nil {
		t.Fatalf("InsertJob: %v", err)
	}
	gj, err := repo.GetJob(ctx, "job_1")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if gj.Kind != "reflect" || gj.Payload["a"] != "1" {
		t.Fatalf("GetJob mismatch: %+v", gj)
	}

	s := now.Add(10 * time.Second)
	f := now.Add(30 * time.Second)
	job.Status = "done"
	job.Result = map[string]string{"r": "1"}
	job.StartedAt = &s
	job.FinishedAt = &f
	if err := repo.UpdateJob(ctx, job); err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	updated, err := repo.GetJob(ctx, "job_1")
	if err != nil {
		t.Fatalf("GetJob updated: %v", err)
	}
	if updated.Status != "done" || updated.Result["r"] != "1" {
		t.Fatalf("updated job mismatch: %+v", updated)
	}

	jobs, err := repo.ListJobs(ctx, core.ListJobsOptions{Kind: "reflect", Status: "done", Limit: 10})
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != "job_1" {
		t.Fatalf("unexpected ListJobs result: %#v", jobs)
	}
}

func TestRepositoryRecallHistory(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()

	if err := repo.RecordRecall(ctx, "sess_r", "mem_1", "memory"); err != nil {
		t.Fatalf("RecordRecall: %v", err)
	}
	if err := repo.RecordRecallBatch(ctx, "sess_r", []core.RecallRecord{{ItemID: "sum_1", ItemKind: "summary"}, {ItemID: "epi_1", ItemKind: "episode"}}); err != nil {
		t.Fatalf("RecordRecallBatch: %v", err)
	}

	recent, err := repo.GetRecentRecalls(ctx, "sess_r", 10)
	if err != nil {
		t.Fatalf("GetRecentRecalls: %v", err)
	}
	if len(recent) != 3 {
		t.Fatalf("expected 3 recall entries, got %d", len(recent))
	}
	if _, err := time.Parse(time.RFC3339, recent[0].ShownAt); err != nil {
		t.Fatalf("shown_at should be RFC3339, got %q", recent[0].ShownAt)
	}

	stats, err := repo.ListMemoryAccessStats(ctx, nowUTC().Add(-time.Hour))
	if err != nil {
		t.Fatalf("ListMemoryAccessStats: %v", err)
	}
	if len(stats) != 1 || stats[0].MemoryID != "mem_1" || stats[0].AccessCount != 1 {
		t.Fatalf("unexpected memory access stats: %#v", stats)
	}

	if err := repo.InsertRelevanceFeedback(ctx, "sess_r", "mem_1", "memory", "expanded"); err != nil {
		t.Fatalf("InsertRelevanceFeedback first: %v", err)
	}
	if err := repo.InsertRelevanceFeedback(ctx, "sess_r", "mem_1", "memory", "expanded"); err != nil {
		t.Fatalf("InsertRelevanceFeedback duplicate should not fail: %v", err)
	}
	fb, err := repo.ListRelevanceFeedback(ctx, "mem_1")
	if err != nil {
		t.Fatalf("ListRelevanceFeedback: %v", err)
	}
	if len(fb) != 1 {
		t.Fatalf("expected deduped feedback count of 1, got %d", len(fb))
	}

	counts, err := repo.CountExpandedFeedbackBatch(ctx, []string{"mem_1", "mem_2"})
	if err != nil {
		t.Fatalf("CountExpandedFeedbackBatch: %v", err)
	}
	if counts["mem_1"] != 1 || counts["mem_2"] != 0 {
		t.Fatalf("unexpected expanded feedback counts: %#v", counts)
	}

	deleted, err := repo.CleanupRecallHistory(ctx, -1)
	if err != nil {
		t.Fatalf("CleanupRecallHistory: %v", err)
	}
	if deleted < 3 {
		t.Fatalf("expected cleanup to remove recorded recalls, deleted=%d", deleted)
	}

	after, err := repo.GetRecentRecalls(ctx, "sess_r", 10)
	if err != nil {
		t.Fatalf("GetRecentRecalls after cleanup: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("expected no recalls after cleanup, got %#v", after)
	}
}

func TestRepositoryEmbeddingsAndUnembeddedLists(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	now := nowUTC()
	model := "test-model"

	if err := repo.InsertMemory(ctx, &core.Memory{ID: "mem_emb_1", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "memory with embedding", TightDescription: "mem emb", Confidence: 0.9, Importance: 0.7, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, SourceEventIDs: []string{}, SourceSummaryIDs: []string{}, SourceArtifactIDs: []string{}, Tags: []string{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("insert mem_emb_1: %v", err)
	}
	if err := repo.InsertMemory(ctx, &core.Memory{ID: "mem_emb_2", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "memory without embedding", TightDescription: "mem no emb", Confidence: 0.9, Importance: 0.7, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, SourceEventIDs: []string{}, SourceSummaryIDs: []string{}, SourceArtifactIDs: []string{}, Tags: []string{}, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}); err != nil {
		t.Fatalf("insert mem_emb_2: %v", err)
	}
	if err := repo.InsertSummary(ctx, &core.Summary{ID: "sum_emb_1", Kind: "leaf", Scope: core.ScopeGlobal, Body: "summary with embedding", TightDescription: "sum emb", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("insert sum_emb_1: %v", err)
	}
	if err := repo.InsertSummary(ctx, &core.Summary{ID: "sum_emb_2", Kind: "leaf", Scope: core.ScopeGlobal, Body: "summary without embedding", TightDescription: "sum no emb", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}); err != nil {
		t.Fatalf("insert sum_emb_2: %v", err)
	}
	if err := repo.InsertEpisode(ctx, &core.Episode{ID: "epi_emb_1", Title: "episode with embedding", Summary: "epi emb", TightDescription: "epi emb", Scope: core.ScopeGlobal, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, SourceSummaryIDs: []string{}, Participants: []string{}, RelatedEntities: []string{}, Outcomes: []string{}, UnresolvedItems: []string{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("insert epi_emb_1: %v", err)
	}
	if err := repo.InsertEpisode(ctx, &core.Episode{ID: "epi_emb_2", Title: "episode without embedding", Summary: "epi no emb", TightDescription: "epi no emb", Scope: core.ScopeGlobal, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, SourceSummaryIDs: []string{}, Participants: []string{}, RelatedEntities: []string{}, Outcomes: []string{}, UnresolvedItems: []string{}, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}); err != nil {
		t.Fatalf("insert epi_emb_2: %v", err)
	}

	if err := repo.UpsertEmbedding(ctx, &core.EmbeddingRecord{ObjectID: "mem_emb_1", ObjectKind: "memory", Model: model, Vector: []float32{1, 2, 3}, CreatedAt: now}); err != nil {
		t.Fatalf("UpsertEmbedding memory: %v", err)
	}
	if err := repo.UpsertEmbedding(ctx, &core.EmbeddingRecord{ObjectID: "sum_emb_1", ObjectKind: "summary", Model: model, Vector: []float32{2, 3, 4}, CreatedAt: now}); err != nil {
		t.Fatalf("UpsertEmbedding summary: %v", err)
	}
	if err := repo.UpsertEmbedding(ctx, &core.EmbeddingRecord{ObjectID: "epi_emb_1", ObjectKind: "episode", Model: model, Vector: []float32{3, 4, 5}, CreatedAt: now}); err != nil {
		t.Fatalf("UpsertEmbedding episode: %v", err)
	}

	g, err := repo.GetEmbedding(ctx, "mem_emb_1", "memory", model)
	if err != nil {
		t.Fatalf("GetEmbedding: %v", err)
	}
	if len(g.Vector) != 3 || g.Vector[0] != 1 {
		t.Fatalf("unexpected embedding vector: %#v", g.Vector)
	}

	batch, err := repo.GetEmbeddingsBatch(ctx, []string{"mem_emb_1", "sum_emb_1", "missing"}, "memory", model)
	if err != nil {
		t.Fatalf("GetEmbeddingsBatch: %v", err)
	}
	if len(batch) != 1 {
		t.Fatalf("expected one memory embedding in batch, got %d", len(batch))
	}

	kindList, err := repo.ListEmbeddingsByKind(ctx, "memory", model, 10)
	if err != nil {
		t.Fatalf("ListEmbeddingsByKind: %v", err)
	}
	if len(kindList) != 1 || kindList[0].ObjectID != "mem_emb_1" {
		t.Fatalf("unexpected embeddings-by-kind result: %#v", kindList)
	}

	unMem, err := repo.ListUnembeddedMemories(ctx, model, 10)
	if err != nil {
		t.Fatalf("ListUnembeddedMemories: %v", err)
	}
	if len(unMem) != 1 || unMem[0].ID != "mem_emb_2" {
		t.Fatalf("unexpected unembedded memories: %#v", unMem)
	}

	unSum, err := repo.ListUnembeddedSummaries(ctx, model, 10)
	if err != nil {
		t.Fatalf("ListUnembeddedSummaries: %v", err)
	}
	if len(unSum) != 1 || unSum[0].ID != "sum_emb_2" {
		t.Fatalf("unexpected unembedded summaries: %#v", unSum)
	}

	unEpi, err := repo.ListUnembeddedEpisodes(ctx, model, 10)
	if err != nil {
		t.Fatalf("ListUnembeddedEpisodes: %v", err)
	}
	if len(unEpi) != 1 || unEpi[0].ID != "epi_emb_2" {
		t.Fatalf("unexpected unembedded episodes: %#v", unEpi)
	}

	if err := repo.DeleteEmbeddings(ctx, "mem_emb_1", "memory", model); err != nil {
		t.Fatalf("DeleteEmbeddings model-specific: %v", err)
	}
	if _, err := repo.GetEmbedding(ctx, "mem_emb_1", "memory", model); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after embedding delete, got %v", err)
	}
}

func TestRepositoryCountsAndMaintenance(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	now := nowUTC()

	mustInsertEvent(t, repo, &core.Event{ID: "evt_count", Kind: "message_user", SourceSystem: "cli", PrivacyLevel: core.PrivacyPrivate, Content: "counted event", OccurredAt: now, IngestedAt: now})
	if err := repo.InsertMemory(ctx, &core.Memory{ID: "mem_count", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "count memory", TightDescription: "count memory", Confidence: 0.5, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, SourceEventIDs: []string{}, SourceSummaryIDs: []string{}, SourceArtifactIDs: []string{}, Tags: []string{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("InsertMemory count: %v", err)
	}
	if err := repo.InsertSummary(ctx, &core.Summary{ID: "sum_count", Kind: "leaf", Scope: core.ScopeGlobal, Body: "count summary", TightDescription: "count summary", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("InsertSummary count: %v", err)
	}
	if err := repo.InsertEpisode(ctx, &core.Episode{ID: "epi_count", Title: "count episode", Summary: "count episode", TightDescription: "count episode", Scope: core.ScopeGlobal, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, SourceSummaryIDs: []string{}, Participants: []string{}, RelatedEntities: []string{}, Outcomes: []string{}, UnresolvedItems: []string{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("InsertEpisode count: %v", err)
	}
	if err := repo.InsertEntity(ctx, &core.Entity{ID: "ent_count", Type: "thing", CanonicalName: "count", Aliases: []string{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("InsertEntity count: %v", err)
	}

	counts := map[string]func(context.Context) (int64, error){
		"events":    repo.CountEvents,
		"memories":  repo.CountMemories,
		"summaries": repo.CountSummaries,
		"episodes":  repo.CountEpisodes,
		"entities":  repo.CountEntities,
	}
	for name, fn := range counts {
		v, err := fn(ctx)
		if err != nil {
			t.Fatalf("count %s: %v", name, err)
		}
		if v != 1 {
			t.Fatalf("expected count(%s)=1 got %d", name, v)
		}
	}

	if err := repo.RebuildFTSIndexes(ctx); err != nil {
		t.Fatalf("RebuildFTSIndexes: %v", err)
	}
	if _, err := repo.SearchMemories(ctx, "count", core.ListMemoriesOptions{Limit: 10}); err != nil {
		t.Fatalf("SearchMemories after rebuild: %v", err)
	}

	reset, err := repo.ResetDerived(ctx)
	if err != nil {
		t.Fatalf("ResetDerived: %v", err)
	}
	if reset.MemoriesDeleted == 0 || reset.SummariesDeleted == 0 || reset.EpisodesDeleted == 0 {
		t.Fatalf("expected ResetDerived to delete canonical records, got %+v", reset)
	}
	remainingMem, err := repo.CountMemories(ctx)
	if err != nil {
		t.Fatalf("CountMemories after reset: %v", err)
	}
	if remainingMem != 0 {
		t.Fatalf("expected memories count to be 0 after ResetDerived, got %d", remainingMem)
	}

	unreflected, err := repo.CountUnreflectedEvents(ctx)
	if err != nil {
		t.Fatalf("CountUnreflectedEvents after reset: %v", err)
	}
	if unreflected != 1 {
		t.Fatalf("expected events reflected_at reset to NULL, got unreflected=%d", unreflected)
	}
}

func TestResetDerived_ClearsStaleMetadataOnPreservedRememberMemories(t *testing.T) {
	repo, closeRepo := testRepo(t)
	defer closeRepo()
	ctx := context.Background()
	now := nowUTC()

	mem := &core.Memory{
		ID:               "mem_reset_preserve",
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "keep this remembered memory",
		TightDescription: "remembered memory",
		Confidence:       0.9,
		Importance:       0.7,
		PrivacyLevel:     core.PrivacyPrivate,
		Status:           core.MemoryStatusActive,
		Metadata: map[string]string{
			"source_system":             "remember",
			"extraction_quality":        "verified",
			"entities_extracted":        "true",
			"entities_extracted_method": "llm",
			"claims_extracted":          "true",
			"embedded_at":               now.Format(time.RFC3339),
			"embedded_model":            "test-embed",
			"lifecycle_reviewed_at":     now.Format(time.RFC3339),
			"lifecycle_reviewed_model":  "test-review",
			"narrative_included":        "true",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := repo.InsertMemory(ctx, mem); err != nil {
		t.Fatalf("insert preserved memory: %v", err)
	}

	if _, err := repo.ResetDerived(ctx); err != nil {
		t.Fatalf("ResetDerived: %v", err)
	}

	updated, err := repo.GetMemory(ctx, mem.ID)
	if err != nil {
		t.Fatalf("get preserved memory after reset-derived: %v", err)
	}
	if updated.Metadata["source_system"] != "remember" {
		t.Fatalf("expected source_system to remain remember, got %q", updated.Metadata["source_system"])
	}
	if updated.Metadata["extraction_quality"] != "verified" {
		t.Fatalf("expected extraction_quality to remain verified, got %q", updated.Metadata["extraction_quality"])
	}
	for _, key := range []string{
		"entities_extracted",
		"entities_extracted_method",
		"claims_extracted",
		"embedded_at",
		"embedded_model",
		"lifecycle_reviewed_at",
		"lifecycle_reviewed_model",
		"narrative_included",
	} {
		if got := updated.Metadata[key]; got != "" {
			t.Fatalf("expected metadata key %s to be cleared, got %q", key, got)
		}
	}
}

func TestRepositoryIsInitializedClaimsAndArtifacts(t *testing.T) {
	repo, closeRepo := testRepo(t)
	ctx := context.Background()

	initialized, err := repo.IsInitialized(ctx)
	if err != nil {
		t.Fatalf("IsInitialized open/migrated: %v", err)
	}
	if !initialized {
		t.Fatal("expected IsInitialized=true after migrate")
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := repo.InsertMemory(ctx, &core.Memory{ID: "mem_claim", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "claim memory", TightDescription: "claim", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, SourceEventIDs: []string{}, SourceSummaryIDs: []string{}, SourceArtifactIDs: []string{}, Tags: []string{}, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatalf("insert memory for claim: %v", err)
	}
	if err := repo.InsertClaim(ctx, &core.Claim{ID: "clm_1", MemoryID: "mem_claim", Predicate: "likes", ObjectValue: "postgres", Confidence: 0.9}); err != nil {
		t.Fatalf("InsertClaim: %v", err)
	}
	claim, err := repo.GetClaim(ctx, "clm_1")
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	if claim.Predicate != "likes" || claim.ObjectValue != "postgres" || claim.Confidence != 0.9 {
		t.Fatalf("GetClaim mismatch: %+v", claim)
	}
	claims, err := repo.ListClaimsByMemory(ctx, "mem_claim")
	if err != nil {
		t.Fatalf("ListClaimsByMemory: %v", err)
	}
	if len(claims) != 1 || claims[0].ID != "clm_1" {
		t.Fatalf("expected one claim for memory, got %#v", claims)
	}

	artifact := &core.Artifact{ID: "art_1", Kind: "doc", SourceSystem: "test", Path: "/tmp/a.txt", Content: "artifact content", Metadata: map[string]string{"m": "1"}, CreatedAt: now}
	if err := repo.InsertArtifact(ctx, artifact); err != nil {
		t.Fatalf("InsertArtifact: %v", err)
	}
	ga, err := repo.GetArtifact(ctx, "art_1")
	if err != nil {
		t.Fatalf("GetArtifact: %v", err)
	}
	if ga.Kind != "doc" || ga.Metadata["m"] != "1" {
		t.Fatalf("artifact mismatch: %+v", ga)
	}

	closeRepo()
	unopened := NewRepository()
	initialized, err = unopened.IsInitialized(ctx)
	if err != nil {
		t.Fatalf("IsInitialized unopened: %v", err)
	}
	if initialized {
		t.Fatal("expected unopened repo to report uninitialized")
	}
}

func TestRepositoryCoverageSmoke(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	now := nowUTC()

	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("evt_cov_%d", i)
		mustInsertEvent(t, repo, &core.Event{ID: id, Kind: "message_user", SourceSystem: "cli", PrivacyLevel: core.PrivacyPrivate, Content: fmt.Sprintf("coverage event %d", i), OccurredAt: now.Add(time.Duration(i) * time.Second), IngestedAt: now})
	}
	list, err := repo.ListEvents(ctx, core.ListEventsOptions{AfterSequenceID: 0, Limit: 100})
	if err != nil {
		t.Fatalf("list events coverage smoke: %v", err)
	}
	if len(list) < 3 {
		t.Fatalf("expected >=3 events, got %d", len(list))
	}

	ids := []string{list[0].ID, list[1].ID, list[2].ID}
	sort.Strings(ids)
	if ids[0] == "" || ids[2] == "" {
		t.Fatalf("unexpected blank ids from list: %#v", ids)
	}
}

func TestRepositoryHelperFunctions(t *testing.T) {
	if got := defaultLimit(0); got != 100 {
		t.Fatalf("defaultLimit(0)=%d want 100", got)
	}
	if got := defaultLimit(7); got != 7 {
		t.Fatalf("defaultLimit(7)=%d want 7", got)
	}

	id := core.GenerateID("evt_")
	if len(id) <= len("evt_") || id[:4] != "evt_" {
		t.Fatalf("generateID prefix mismatch: %q", id)
	}
	if ph := placeholders(3, 4); ph != "$3,$4,$5,$6" {
		t.Fatalf("placeholders mismatch: %q", ph)
	}

	if got := sanitizeTSQuery("  hi  "); got != "hi" {
		t.Fatalf("sanitizeTSQuery mismatch: %q", got)
	}
	if got := tsQueryLanguage(); got != "simple" {
		t.Fatalf("tsQueryLanguage mismatch: %q", got)
	}

	if defaultPolicyMatchMode("") != "glob" {
		t.Fatal("expected empty match mode to default to glob")
	}
	if defaultPolicyMatchMode("regex") != "regex" {
		t.Fatal("expected explicit match mode to pass through")
	}

	if !matchesPolicy(core.IngestionPolicy{Pattern: "svc-*", MatchMode: "glob"}, "svc-a") {
		t.Fatal("expected glob match")
	}
	if !matchesPolicy(core.IngestionPolicy{Pattern: "^svc-[a-z]+$", MatchMode: "regex"}, "svc-ab") {
		t.Fatal("expected regex match")
	}
	if matchesPolicy(core.IngestionPolicy{Pattern: "(", MatchMode: "regex"}, "svc") {
		t.Fatal("expected invalid regex not to match")
	}
	if !matchesPolicy(core.IngestionPolicy{Pattern: "svc-a", MatchMode: "exact"}, "svc-a") {
		t.Fatal("expected exact match")
	}

	if got := normalizeRowsAffected(-1); got != 0 {
		t.Fatalf("normalizeRowsAffected(-1)=%d want 0", got)
	}
	if got := normalizeRowsAffected(2); got != 2 {
		t.Fatalf("normalizeRowsAffected(2)=%d want 2", got)
	}

	if v := decodeVector(nil); v != nil {
		t.Fatalf("decodeVector(nil)=%v want nil", v)
	}
	if v := decodeVector([]byte{1, 2, 3}); v != nil {
		t.Fatalf("decodeVector(short)=%v want nil", v)
	}
	vec := []float32{1.5, 2.5}
	if got := decodeVector(encodeVector(vec)); len(got) != 2 || got[0] != 1.5 || got[1] != 2.5 {
		t.Fatalf("encode/decode mismatch: %#v", got)
	}

	if got := parseMapJSON(nil); len(got) != 0 {
		t.Fatalf("parseMapJSON(nil)=%v", got)
	}
	if got := parseMapJSON([]byte("not-json")); len(got) != 0 {
		t.Fatalf("parseMapJSON(invalid)=%v", got)
	}
}

func TestRepositoryNotFoundAndErrorPaths(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()

	if _, err := repo.GetEvent(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetEvent missing expected ErrNotFound, got %v", err)
	}
	if _, err := repo.GetMemory(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetMemory missing expected ErrNotFound, got %v", err)
	}
	if _, err := repo.GetSummary(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetSummary missing expected ErrNotFound, got %v", err)
	}
	if _, err := repo.GetEpisode(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetEpisode missing expected ErrNotFound, got %v", err)
	}
	if _, err := repo.GetEntity(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetEntity missing expected ErrNotFound, got %v", err)
	}
	if _, err := repo.GetProject(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetProject missing expected ErrNotFound, got %v", err)
	}
	if _, err := repo.GetRelationship(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetRelationship missing expected ErrNotFound, got %v", err)
	}
	if _, err := repo.GetIngestionPolicy(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetIngestionPolicy missing expected ErrNotFound, got %v", err)
	}
	if _, err := repo.GetJob(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetJob missing expected ErrNotFound, got %v", err)
	}
	if _, err := repo.GetArtifact(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetArtifact missing expected ErrNotFound, got %v", err)
	}

	if err := repo.DeleteProject(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("DeleteProject missing expected ErrNotFound, got %v", err)
	}
	if err := repo.DeleteRelationship(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("DeleteRelationship missing expected ErrNotFound, got %v", err)
	}
	if err := repo.DeleteIngestionPolicy(ctx, "missing"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("DeleteIngestionPolicy missing expected ErrNotFound, got %v", err)
	}

	if rows, err := repo.ListMemoriesBySourceEventIDs(ctx, []string{"evt-none"}); err != nil || len(rows) != 0 {
		t.Fatalf("ListMemoriesBySourceEventIDs(non-empty no match) rows=%v err=%v", rows, err)
	}
	if err := repo.RecordRecallBatch(ctx, "sess-empty", nil); err != nil {
		t.Fatalf("RecordRecallBatch empty should succeed: %v", err)
	}
	if out, err := repo.CountExpandedFeedbackBatch(ctx, nil); err != nil || len(out) != 0 {
		t.Fatalf("CountExpandedFeedbackBatch empty out=%v err=%v", out, err)
	}

	if err := repo.DeleteEmbeddings(ctx, "missing", "memory", ""); err != nil {
		t.Fatalf("DeleteEmbeddings empty-model branch should not fail: %v", err)
	}
}

func TestMigrationAndConnectionPaths(t *testing.T) {
	repo, _ := testRepo(t)
	ctx := context.Background()
	if err := repo.Migrate(ctx); err != nil {
		t.Fatalf("second migrate should be idempotent: %v", err)
	}

	bad := NewRepository()
	if err := bad.Open(ctx, "postgres://invalid:invalid@127.0.0.1:1/invalid?sslmode=disable&connect_timeout=1"); err == nil {
		t.Fatal("expected Open to fail for invalid DSN")
	}

	closed := NewRepository()
	if err := closed.Close(); err != nil {
		t.Fatalf("Close on unopened repo should succeed: %v", err)
	}
}
