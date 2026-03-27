package service

import (
	"context"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

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

func TestRunJob_DispatchesArchiveKind(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

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
