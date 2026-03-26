//go:build fts5

package service

import (
	"context"
	"testing"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestCrossProjectTransfer_PromotesSimilarMemories(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	memA, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeProject,
		ProjectID:        "project-a",
		Body:             "Use gofmt before committing Go code to keep formatting consistent",
		TightDescription: "Run gofmt before commits",
		Importance:       0.8,
		Confidence:       0.85,
		SourceEventIDs:   []string{"evt_a"},
		Metadata: map[string]string{
			MetaExtractionMethod: MethodHeuristic,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	memB, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeProject,
		ProjectID:        "project-b",
		Body:             "Use gofmt before committing Go code to keep formatting consistent and readable",
		TightDescription: "Always run gofmt before committing",
		Importance:       0.75,
		Confidence:       0.93,
		SourceEventIDs:   []string{"evt_b"},
		Metadata: map[string]string{
			MetaExtractionMethod: MethodLLM,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	promoted, err := svc.CrossProjectTransfer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if promoted != 1 {
		t.Fatalf("expected 1 promoted memory, got %d", promoted)
	}

	globals, err := repo.ListMemories(ctx, core.ListMemoriesOptions{
		Scope:  core.ScopeGlobal,
		Status: core.MemoryStatusActive,
		Limit:  10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(globals) != 1 {
		t.Fatalf("expected 1 active global memory, got %d", len(globals))
	}

	global := globals[0]
	if global.ProjectID != "" {
		t.Fatalf("expected promoted global memory with empty project_id, got %q", global.ProjectID)
	}
	if global.Body != memB.Body {
		t.Fatalf("expected promoted body from best memory, got %q", global.Body)
	}
	if global.TightDescription != memB.TightDescription {
		t.Fatalf("expected promoted tight_description from best memory, got %q", global.TightDescription)
	}
	if global.Metadata[MetaExtractionQuality] != QualityVerified {
		t.Fatalf("expected extraction_quality=verified, got %q", global.Metadata[MetaExtractionQuality])
	}
	if global.Metadata[MetaExtractionMethod] != MethodLLM {
		t.Fatalf("expected extraction_method from best memory to be llm, got %q", global.Metadata[MetaExtractionMethod])
	}
	if len(global.SourceEventIDs) != 2 {
		t.Fatalf("expected merged source_event_ids from both originals, got %v", global.SourceEventIDs)
	}

	updatedA, err := repo.GetMemory(ctx, memA.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedA.Status != core.MemoryStatusSuperseded || updatedA.SupersededBy != global.ID {
		t.Fatalf("expected memory A to be superseded by %s, got status=%s superseded_by=%q", global.ID, updatedA.Status, updatedA.SupersededBy)
	}

	updatedB, err := repo.GetMemory(ctx, memB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedB.Status != core.MemoryStatusSuperseded || updatedB.SupersededBy != global.ID {
		t.Fatalf("expected memory B to be superseded by %s, got status=%s superseded_by=%q", global.ID, updatedB.Status, updatedB.SupersededBy)
	}
}

func TestCrossProjectTransfer_KeepsProjectSpecific(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeProject,
		ProjectID:        "project-a",
		Body:             "This repository uses CircleCI workflows with custom contexts",
		TightDescription: "Project A CircleCI setup",
		Importance:       0.8,
		Confidence:       0.9,
	})
	if err != nil {
		t.Fatal(err)
	}

	promoted, err := svc.CrossProjectTransfer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if promoted != 0 {
		t.Fatalf("expected 0 promoted memories, got %d", promoted)
	}

	globals, err := repo.ListMemories(ctx, core.ListMemoriesOptions{
		Scope: core.ScopeGlobal,
		Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(globals) != 0 {
		t.Fatalf("expected no global memories to be created, got %d", len(globals))
	}
}

func TestCrossProjectTransfer_SkipsLowImportance(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeProject,
		ProjectID:        "project-a",
		Body:             "Use staticcheck in CI for extra lint coverage",
		TightDescription: "Use staticcheck in CI",
		Importance:       0.6,
		Confidence:       0.9,
	})
	if err != nil {
		t.Fatal(err)
	}

	memB, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeProject,
		ProjectID:        "project-b",
		Body:             "Use staticcheck in CI for lint coverage",
		TightDescription: "Run staticcheck in CI",
		Importance:       0.65,
		Confidence:       0.91,
	})
	if err != nil {
		t.Fatal(err)
	}

	promoted, err := svc.CrossProjectTransfer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if promoted != 0 {
		t.Fatalf("expected 0 promoted memories for low-importance inputs, got %d", promoted)
	}

	updatedB, err := repo.GetMemory(ctx, memB.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedB.Status != core.MemoryStatusActive {
		t.Fatalf("expected low-importance memory to remain active, got %s", updatedB.Status)
	}
}

func TestCrossProjectTransfer_RunJob(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeProject,
		ProjectID:        "project-a",
		Body:             "Prefer context.Context as the first argument in Go APIs",
		TightDescription: "Context first in Go APIs",
		Importance:       0.8,
		Confidence:       0.88,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeProject,
		ProjectID:        "project-b",
		Body:             "Prefer context.Context as the first argument in Go APIs and handlers",
		TightDescription: "Context first argument",
		Importance:       0.78,
		Confidence:       0.9,
	})
	if err != nil {
		t.Fatal(err)
	}

	job, err := svc.RunJob(ctx, "cross_project_transfer")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" {
		t.Fatalf("expected completed cross_project_transfer job, got %+v", job)
	}
	if job.Result["action"] != "cross_project_transfer" {
		t.Fatalf("unexpected cross_project_transfer job action: %+v", job.Result)
	}
	if job.Result["memories_promoted"] != "1" {
		t.Fatalf("expected memories_promoted=1, got %+v", job.Result)
	}
}
