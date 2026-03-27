package service

import (
	"context"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestRecall_EntityRelationshipBoost(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	sqliteEntity := &core.Entity{ID: "ent_sqlite_phase3a", Type: "technology", CanonicalName: "SQLite", CreatedAt: now, UpdatedAt: now}
	driverEntity := &core.Entity{ID: "ent_modernc_sqlite_phase3a", Type: "technology", CanonicalName: "modernc-sqlite", CreatedAt: now, UpdatedAt: now}
	for _, entity := range []*core.Entity{sqliteEntity, driverEntity} {
		if err := repo.InsertEntity(ctx, entity); err != nil {
			t.Fatalf("insert entity %s: %v", entity.ID, err)
		}
	}

	memory, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "Use the modernc pure-Go driver for local persistence",
		TightDescription: "Go driver selection",
		Confidence:       0.9,
		Importance:       0.7,
	})
	if err != nil {
		t.Fatalf("remember memory: %v", err)
	}
	if err := repo.LinkMemoryEntity(ctx, memory.ID, driverEntity.ID, "mentioned"); err != nil {
		t.Fatalf("link memory entity: %v", err)
	}

	candidate := MemoryToCandidate(*memory, 0)
	candidates := []ScoringCandidate{candidate}
	svc.attachCandidateEntities(ctx, candidates)

	withoutRelationship := svc.buildScoringContext(ctx, "SQLite", core.RecallOptions{Mode: core.RecallModeFacts})
	withoutBreakdown := ScoreItem(candidates[0], withoutRelationship)

	if err := repo.InsertRelationship(ctx, &core.Relationship{
		ID:               "rel_modernc_sqlite_depends_sqlite_phase3a",
		FromEntityID:     driverEntity.ID,
		ToEntityID:       sqliteEntity.ID,
		RelationshipType: "depends-on",
		CreatedAt:        now,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("insert relationship: %v", err)
	}

	withRelationship := svc.buildScoringContext(ctx, "SQLite", core.RecallOptions{Mode: core.RecallModeFacts})
	withBreakdown := ScoreItem(candidates[0], withRelationship)

	if withBreakdown.EntityOverlap <= withoutBreakdown.EntityOverlap {
		t.Fatalf("expected entity overlap boost with relationship: without=%f with=%f", withoutBreakdown.EntityOverlap, withBreakdown.EntityOverlap)
	}
	if withBreakdown.FinalScore <= withoutBreakdown.FinalScore {
		t.Fatalf("expected final score boost with relationship: without=%f with=%f", withoutBreakdown.FinalScore, withBreakdown.FinalScore)
	}
}
