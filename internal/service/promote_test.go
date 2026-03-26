//go:build fts5

package service

import (
	"context"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestPromoteHighValueMemories_NoHighRecallReturnsZero(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	promoted, err := svc.PromoteHighValueMemories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if promoted != 0 {
		t.Fatalf("expected 0 promoted (no high-recall memories), got %d", promoted)
	}
}

func TestPromoteHighValue_BoostsHighRecallMemories(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "high recall promotion candidate",
		TightDescription: "high recall promotion candidate",
		Importance:       0.6,
		Confidence:       0.8,
	})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		if err := repo.RecordRecall(ctx, "sess-promote", mem.ID, "memory"); err != nil {
			t.Fatal(err)
		}
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
	if updated.Importance <= 0.6 {
		t.Fatalf("expected boosted importance above 0.6, got %f", updated.Importance)
	}
}

func TestPromoteHighValue_SkipsAlreadyHighImportance(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Body:             "high importance memory",
		TightDescription: "high importance memory",
		Importance:       0.9,
		Confidence:       0.8,
	})
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 6; i++ {
		if err := repo.RecordRecall(ctx, "sess-skip", mem.ID, "memory"); err != nil {
			t.Fatal(err)
		}
	}

	promoted, err := svc.PromoteHighValueMemories(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if promoted != 0 {
		t.Fatalf("expected 0 promoted memories, got %d", promoted)
	}

	updated, err := repo.GetMemory(ctx, mem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Importance != 0.9 {
		t.Fatalf("expected unchanged importance 0.9, got %f", updated.Importance)
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
	if promoteJob.Result["action"] != "promote_high_value" || promoteJob.Result["memories_promoted"] != "0" {
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
