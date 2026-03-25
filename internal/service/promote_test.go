//go:build fts5

package service

import (
	"context"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestPromoteHighValueMemories_PromotesQualifyingMemories(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "battle tested memory",
		TightDescription: "battle tested",
		Importance:       0.9,
		Confidence:       0.9,
	})
	if err != nil {
		t.Fatal(err)
	}

	mem.UpdatedAt = time.Now().UTC().Add(-48 * time.Hour)
	if err := repo.UpdateMemory(ctx, mem); err != nil {
		t.Fatal(err)
	}

	promoted, err := svc.PromoteHighValueMemories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if promoted != 1 {
		t.Fatalf("expected 1 promoted memory, got %d", promoted)
	}

	updated, err := repo.GetMemory(ctx, mem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Importance <= 0.9 || updated.Importance > 1.0 {
		t.Fatalf("expected promoted importance in (0.9, 1.0], got %f", updated.Importance)
	}
	if updated.LastConfirmedAt == nil {
		t.Fatal("expected LastConfirmedAt to be set after promotion")
	}
	if time.Since(updated.LastConfirmedAt.UTC()) > 5*time.Second {
		t.Fatalf("expected LastConfirmedAt near now, got %s", updated.LastConfirmedAt.UTC())
	}
}

func TestPromoteHighValueMemories_SkipsNonQualifyingMemories(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	lowImportance, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "low importance",
		TightDescription: "low importance",
		Importance:       0.7,
		Confidence:       0.95,
	})
	if err != nil {
		t.Fatal(err)
	}

	lowConfidence, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "low confidence",
		TightDescription: "low confidence",
		Importance:       0.95,
		Confidence:       0.7,
	})
	if err != nil {
		t.Fatal(err)
	}

	stale, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "stale memory",
		TightDescription: "stale",
		Importance:       0.95,
		Confidence:       0.95,
	})
	if err != nil {
		t.Fatal(err)
	}
	stale.UpdatedAt = time.Now().UTC().Add(-45 * 24 * time.Hour)
	if err := repo.UpdateMemory(ctx, stale); err != nil {
		t.Fatal(err)
	}

	identity, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeIdentity,
		Scope:            core.ScopeGlobal,
		Body:             "identity memory",
		TightDescription: "identity",
		Importance:       0.95,
		Confidence:       0.95,
	})
	if err != nil {
		t.Fatal(err)
	}

	promoted, err := svc.PromoteHighValueMemories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if promoted != 0 {
		t.Fatalf("expected 0 promoted memories, got %d", promoted)
	}

	for _, tc := range []struct {
		id   string
		want float64
	}{
		{id: lowImportance.ID, want: 0.7},
		{id: lowConfidence.ID, want: 0.95},
		{id: stale.ID, want: 0.95},
		{id: identity.ID, want: 0.95},
	} {
		mem, err := repo.GetMemory(ctx, tc.id)
		if err != nil {
			t.Fatal(err)
		}
		if mem.Importance != tc.want {
			t.Fatalf("memory %s importance changed unexpectedly: got %f want %f", tc.id, mem.Importance, tc.want)
		}
	}
}

func TestArchiveLowSalienceSessionTraces_ArchivesOldLowImportanceSessionMemories(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeTodo,
		Scope:            core.ScopeSession,
		SessionID:        "sess_archive",
		Body:             "ephemeral trace",
		TightDescription: "ephemeral trace",
		Importance:       0.2,
		Confidence:       0.8,
	})
	if err != nil {
		t.Fatal(err)
	}

	old := time.Now().UTC().Add(-10 * 24 * time.Hour)
	mem.UpdatedAt = old
	if err := repo.UpdateMemory(ctx, mem); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ExecContext(ctx, "UPDATE memories SET created_at=? WHERE id=?", old.Format(time.RFC3339), mem.ID); err != nil {
		t.Fatal(err)
	}

	archived, err := svc.ArchiveLowSalienceSessionTraces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if archived != 1 {
		t.Fatalf("expected 1 archived memory, got %d", archived)
	}

	updated, err := repo.GetMemory(ctx, mem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != core.MemoryStatusArchived {
		t.Fatalf("expected status archived, got %s", updated.Status)
	}
}

func TestArchiveLowSalienceSessionTraces_SkipsGlobalAndProjectMemories(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	globalMem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "old low global",
		TightDescription: "old low global",
		Importance:       0.1,
		Confidence:       0.8,
	})
	if err != nil {
		t.Fatal(err)
	}

	projectMem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeProject,
		ProjectID:        "proj_archive",
		Body:             "old low project",
		TightDescription: "old low project",
		Importance:       0.1,
		Confidence:       0.8,
	})
	if err != nil {
		t.Fatal(err)
	}

	archived, err := svc.ArchiveLowSalienceSessionTraces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if archived != 0 {
		t.Fatalf("expected 0 archived memories, got %d", archived)
	}

	for _, id := range []string{globalMem.ID, projectMem.ID} {
		mem, err := repo.GetMemory(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if mem.Status != core.MemoryStatusActive {
			t.Fatalf("memory %s should remain active, got %s", id, mem.Status)
		}
	}
}

func TestRunJob_DispatchesPromoteAndArchiveKinds(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	promoteMem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "promote candidate",
		TightDescription: "promote candidate",
		Importance:       0.9,
		Confidence:       0.9,
	})
	if err != nil {
		t.Fatal(err)
	}
	promoteMem.UpdatedAt = time.Now().UTC().Add(-24 * time.Hour)
	if err := repo.UpdateMemory(ctx, promoteMem); err != nil {
		t.Fatal(err)
	}

	archiveMem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeOpenLoop,
		Scope:            core.ScopeSession,
		SessionID:        "sess_dispatch_archive",
		Body:             "archive candidate",
		TightDescription: "archive candidate",
		Importance:       0.2,
		Confidence:       0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().UTC().Add(-10 * 24 * time.Hour)
	archiveMem.UpdatedAt = old
	if err := repo.UpdateMemory(ctx, archiveMem); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ExecContext(ctx, "UPDATE memories SET created_at=? WHERE id=?", old.Format(time.RFC3339), archiveMem.ID); err != nil {
		t.Fatal(err)
	}

	promoteJob, err := svc.RunJob(ctx, "promote_high_value")
	if err != nil {
		t.Fatal(err)
	}
	if promoteJob.Status != "completed" {
		t.Fatalf("expected completed promote job, got %+v", promoteJob)
	}
	if promoteJob.Result["action"] != "promote_high_value" || promoteJob.Result["memories_promoted"] != "1" {
		t.Fatalf("unexpected promote job result: %+v", promoteJob.Result)
	}

	archiveJob, err := svc.RunJob(ctx, "archive_session_traces")
	if err != nil {
		t.Fatal(err)
	}
	if archiveJob.Status != "completed" {
		t.Fatalf("expected completed archive job, got %+v", archiveJob)
	}
	if archiveJob.Result["action"] != "archive_session_traces" || archiveJob.Result["memories_archived"] != "1" {
		t.Fatalf("unexpected archive job result: %+v", archiveJob.Result)
	}

	updatedArchiveMem, err := repo.GetMemory(ctx, archiveMem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedArchiveMem.Status != core.MemoryStatusArchived {
		t.Fatalf("expected archived status from archive job, got %s", updatedArchiveMem.Status)
	}

}
