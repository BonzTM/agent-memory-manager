//go:build fts5

package service_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshd-04/agent-memory-manager/internal/adapters/sqlite"
	"github.com/joshd-04/agent-memory-manager/internal/core"
	"github.com/joshd-04/agent-memory-manager/internal/service"
)

func testService(t *testing.T) core.Service {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	err = sqlite.Migrate(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	repo := &sqlite.SQLiteRepository{DB: db}
	svc := service.New(repo, dbPath)
	t.Cleanup(func() { db.Close() })
	return svc
}

func TestInit(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "init_test.db")

	repo := sqlite.NewSQLiteRepository()
	svc := service.New(repo, dbPath)

	ctx := context.Background()
	if err := svc.Init(ctx, dbPath); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer repo.Close()

	// Verify we can get status (proves DB is up and migrated).
	status, err := svc.Status(ctx)
	if err != nil {
		t.Fatalf("Status after Init: %v", err)
	}
	if !status.Initialized {
		t.Error("expected Initialized=true after Init")
	}
	if status.DBPath != dbPath {
		t.Errorf("expected DBPath=%s, got %s", dbPath, status.DBPath)
	}
}

func TestIngestEvent(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	evt := &core.Event{
		Kind:         "message",
		SourceSystem: "test",
		SessionID:    "sess_1",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      "the user asked about deployment",
	}

	got, err := svc.IngestEvent(ctx, evt)
	if err != nil {
		t.Fatalf("IngestEvent: %v", err)
	}

	// ID should be auto-generated.
	if got.ID == "" {
		t.Error("expected auto-generated ID")
	}
	if got.IngestedAt.IsZero() {
		t.Error("expected IngestedAt to be set")
	}
	if got.OccurredAt.IsZero() {
		t.Error("expected OccurredAt to default to IngestedAt")
	}
}

func TestRemember(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	mem := &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "Go uses goroutines for concurrency",
		TightDescription: "Go concurrency via goroutines",
	}

	got, err := svc.Remember(ctx, mem)
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	// Check auto-generated ID.
	if got.ID == "" {
		t.Error("expected auto-generated ID")
	}

	// Check defaults.
	if got.Status != core.MemoryStatusActive {
		t.Errorf("expected default status active, got %s", got.Status)
	}
	if got.PrivacyLevel != core.PrivacyPrivate {
		t.Errorf("expected default privacy private, got %s", got.PrivacyLevel)
	}
	if got.Confidence != 0.8 {
		t.Errorf("expected default confidence 0.8, got %f", got.Confidence)
	}
	if got.Importance != 0.5 {
		t.Errorf("expected default importance 0.5, got %f", got.Importance)
	}
	if got.Scope != core.ScopeGlobal {
		t.Errorf("expected default scope global, got %s", got.Scope)
	}
	if got.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
	if got.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}
}

func TestRecallAmbient(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	// Remember something searchable.
	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "Terraform manages infrastructure as code",
		TightDescription: "Terraform infrastructure as code",
	})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	// Recall in ambient mode.
	result, err := svc.Recall(ctx, "Terraform", core.RecallOptions{
		Mode: core.RecallModeAmbient,
	})
	if err != nil {
		t.Fatalf("Recall ambient: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil RecallResult")
	}
	if result.Meta.Mode != core.RecallModeAmbient {
		t.Errorf("expected mode ambient, got %s", result.Meta.Mode)
	}
	if len(result.Items) == 0 {
		t.Error("expected at least one recall item for 'Terraform'")
	}

	// Verify the item references our memory.
	found := false
	for _, item := range result.Items {
		if item.Kind == "memory" && item.TightDescription == "Terraform infrastructure as code" {
			found = true
		}
	}
	if !found {
		t.Error("expected to find the Terraform memory in recall results")
	}
}

func TestRecallFacts(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	// Remember two facts.
	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "Redis is an in-memory data store",
		TightDescription: "Redis in-memory data store",
	})
	if err != nil {
		t.Fatalf("Remember redis: %v", err)
	}
	_, err = svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "PostgreSQL is a relational database",
		TightDescription: "PostgreSQL relational database",
	})
	if err != nil {
		t.Fatalf("Remember postgres: %v", err)
	}

	// Recall facts about Redis.
	result, err := svc.Recall(ctx, "Redis", core.RecallOptions{
		Mode: core.RecallModeFacts,
	})
	if err != nil {
		t.Fatalf("Recall facts: %v", err)
	}
	if len(result.Items) == 0 {
		t.Error("expected at least one fact recall item for 'Redis'")
	}
	for _, item := range result.Items {
		if item.Kind != "memory" {
			t.Errorf("expected all items to be kind=memory in facts mode, got %s", item.Kind)
		}
	}
}

func TestDescribe(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeDecision,
		Body:             "We decided to use Go for the backend",
		TightDescription: "Backend language decision: Go",
	})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	results, err := svc.Describe(ctx, []string{mem.ID})
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 describe result, got %d", len(results))
	}
	desc := results[0]
	if desc.ID != mem.ID {
		t.Errorf("expected ID %s, got %s", mem.ID, desc.ID)
	}
	if desc.Kind != "memory" {
		t.Errorf("expected kind memory, got %s", desc.Kind)
	}
	if desc.TightDescription != "Backend language decision: Go" {
		t.Errorf("unexpected tight_description: %q", desc.TightDescription)
	}
	if desc.Status != core.MemoryStatusActive {
		t.Errorf("expected status active, got %s", desc.Status)
	}
}

func TestExpand(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeProcedure,
		Body:             "To deploy, run make deploy in the repo root",
		TightDescription: "Deploy procedure: make deploy",
	})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	result, err := svc.Expand(ctx, mem.ID, "memory")
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if result.Memory == nil {
		t.Fatal("expected non-nil Memory in expand result")
	}
	if result.Memory.ID != mem.ID {
		t.Errorf("expected ID %s, got %s", mem.ID, result.Memory.ID)
	}
	if result.Memory.Body != "To deploy, run make deploy in the repo root" {
		t.Errorf("unexpected body: %q", result.Memory.Body)
	}
}

func TestHistory(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	// Ingest several events in a session.
	for i := 0; i < 3; i++ {
		_, err := svc.IngestEvent(ctx, &core.Event{
			Kind:         "message",
			SourceSystem: "test",
			SessionID:    "sess_hist",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      "history event " + string(rune('A'+i)),
			OccurredAt:   time.Now().UTC().Add(time.Duration(i) * time.Minute),
		})
		if err != nil {
			t.Fatalf("IngestEvent %d: %v", i, err)
		}
	}

	// Retrieve by session.
	events, err := svc.History(ctx, "", core.HistoryOptions{
		SessionID: "sess_hist",
	})
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("expected 3 history events, got %d", len(events))
	}

	// Retrieve with limit.
	limited, err := svc.History(ctx, "", core.HistoryOptions{
		SessionID: "sess_hist",
		Limit:     2,
	})
	if err != nil {
		t.Fatalf("History with limit: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("expected 2 limited events, got %d", len(limited))
	}
}

func TestStatus(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	now := time.Now().UTC()

	// Ingest one event and one memory.
	_, err := svc.IngestEvent(ctx, &core.Event{
		Kind: "message", SourceSystem: "test", PrivacyLevel: core.PrivacyPrivate,
		Content: "status test event", OccurredAt: now,
	})
	if err != nil {
		t.Fatalf("IngestEvent: %v", err)
	}
	_, err = svc.Remember(ctx, &core.Memory{
		Type: core.MemoryTypeFact, Body: "status test memory",
		TightDescription: "status test",
	})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	status, err := svc.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !status.Initialized {
		t.Error("expected Initialized=true")
	}
	if status.EventCount != 1 {
		t.Errorf("expected EventCount=1, got %d", status.EventCount)
	}
	if status.MemoryCount != 1 {
		t.Errorf("expected MemoryCount=1, got %d", status.MemoryCount)
	}
}

func TestRepairCheck(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	now := time.Now().UTC()

	// Add some data.
	_, err := svc.IngestEvent(ctx, &core.Event{
		Kind: "message", SourceSystem: "test", PrivacyLevel: core.PrivacyPrivate,
		Content: "repair check event", OccurredAt: now,
	})
	if err != nil {
		t.Fatalf("IngestEvent: %v", err)
	}

	report, err := svc.Repair(ctx, true, "")
	if err != nil {
		t.Fatalf("Repair --check: %v", err)
	}
	if report.Checked == 0 {
		t.Error("expected Checked > 0")
	}
	if len(report.Details) == 0 {
		t.Error("expected at least one detail line")
	}
}

func TestRepairFixIndexes(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	// Add a memory so there's data to rebuild.
	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "rebuild test memory",
		TightDescription: "rebuild test",
	})
	if err != nil {
		t.Fatalf("Remember: %v", err)
	}

	report, err := svc.Repair(ctx, false, "indexes")
	if err != nil {
		t.Fatalf("Repair --fix indexes: %v", err)
	}
	if report.Fixed != 1 {
		t.Errorf("expected Fixed=1, got %d", report.Fixed)
	}

	// Verify search still works after rebuild.
	result, err := svc.Recall(ctx, "rebuild", core.RecallOptions{
		Mode: core.RecallModeFacts,
	})
	if err != nil {
		t.Fatalf("Recall after repair: %v", err)
	}
	if len(result.Items) == 0 {
		t.Error("expected recall results after FTS index rebuild")
	}
}

func TestAddPolicy(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	policy := &core.IngestionPolicy{
		PatternType: "source",
		Pattern:     "svc-*",
		Mode:        "full",
	}

	created, err := svc.AddPolicy(ctx, policy)
	if err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected AddPolicy to generate ID")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatal("expected AddPolicy to set timestamps")
	}
}

func TestListPolicies(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	_, err := svc.AddPolicy(ctx, &core.IngestionPolicy{PatternType: "source", Pattern: "svc-*", Mode: "full"})
	if err != nil {
		t.Fatalf("AddPolicy 1: %v", err)
	}
	_, err = svc.AddPolicy(ctx, &core.IngestionPolicy{PatternType: "session", Pattern: "sess-*", Mode: "read_only"})
	if err != nil {
		t.Fatalf("AddPolicy 2: %v", err)
	}

	policies, err := svc.ListPolicies(ctx)
	if err != nil {
		t.Fatalf("ListPolicies: %v", err)
	}
	if len(policies) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(policies))
	}
}

func TestRemovePolicy(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()

	created, err := svc.AddPolicy(ctx, &core.IngestionPolicy{PatternType: "source", Pattern: "noisy-*", Mode: "ignore"})
	if err != nil {
		t.Fatalf("AddPolicy: %v", err)
	}

	if err := svc.RemovePolicy(ctx, created.ID); err != nil {
		t.Fatalf("RemovePolicy: %v", err)
	}

	policies, err := svc.ListPolicies(ctx)
	if err != nil {
		t.Fatalf("ListPolicies: %v", err)
	}
	if len(policies) != 0 {
		t.Fatalf("expected 0 policies after remove, got %d", len(policies))
	}

	if err := svc.RemovePolicy(ctx, "pol_missing"); err == nil {
		t.Fatal("expected RemovePolicy on missing id to fail")
	}
}
