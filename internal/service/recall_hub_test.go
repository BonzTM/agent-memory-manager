//go:build fts5

package service

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestExpandQueryEntities_HubDampeningLowersWeight(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	hub := &core.Entity{ID: "ent_hub_go", Type: "technology", CanonicalName: "Go", CreatedAt: now, UpdatedAt: now}
	rare := &core.Entity{ID: "ent_rare_intel_provider", Type: "technology", CanonicalName: "IntelligenceProvider", CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEntity(ctx, hub); err != nil {
		t.Fatalf("insert hub entity: %v", err)
	}
	if err := repo.InsertEntity(ctx, rare); err != nil {
		t.Fatalf("insert rare entity: %v", err)
	}

	if err := seedActiveMemoriesWithEntityLinks(ctx, repo, now, 20, map[string]int{
		hub.ID:  12,
		rare.ID: 1,
	}); err != nil {
		t.Fatalf("seed memories and links: %v", err)
	}

	weights := svc.expandQueryEntities(ctx, []string{"Go", "IntelligenceProvider"})
	hubWeight := weights["go"]
	rareWeight := weights["intelligenceprovider"]

	if rareWeight != 1.0 {
		t.Fatalf("expected rare entity below threshold to keep full weight 1.0, got %f", rareWeight)
	}
	if hubWeight >= rareWeight {
		t.Fatalf("expected hub weight < rare weight, hub=%f rare=%f", hubWeight, rareWeight)
	}
	if hubWeight <= entityHubDampeningFloor {
		t.Fatalf("expected hub dampening above floor in this setup, got %f", hubWeight)
	}
}

func TestExpandQueryEntities_HubDampeningFloor(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	hub := &core.Entity{ID: "ent_hub_api", Type: "technology", CanonicalName: "API", CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEntity(ctx, hub); err != nil {
		t.Fatalf("insert entity: %v", err)
	}

	if err := seedActiveMemoriesWithEntityLinks(ctx, repo, now, 20, map[string]int{hub.ID: 20}); err != nil {
		t.Fatalf("seed memories and links: %v", err)
	}

	weights := svc.expandQueryEntities(ctx, []string{"API"})
	weight := weights["api"]
	if math.Abs(weight-entityHubDampeningFloor) > 1e-9 {
		t.Fatalf("expected dampening floor %f, got %f", entityHubDampeningFloor, weight)
	}
}

func TestExpandQueryEntities_BelowThresholdHasNoDampening(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	entity := &core.Entity{ID: "ent_threshold_sqlite", Type: "technology", CanonicalName: "SQLite", CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEntity(ctx, entity); err != nil {
		t.Fatalf("insert entity: %v", err)
	}

	if err := seedActiveMemoriesWithEntityLinks(ctx, repo, now, 30, map[string]int{entity.ID: entityHubThreshold - 1}); err != nil {
		t.Fatalf("seed memories and links: %v", err)
	}

	weights := svc.expandQueryEntities(ctx, []string{"SQLite"})
	if weights["sqlite"] != 1.0 {
		t.Fatalf("expected entity below threshold to keep full weight 1.0, got %f", weights["sqlite"])
	}
}

func seedActiveMemoriesWithEntityLinks(ctx context.Context, repo core.Repository, now time.Time, total int, linksByEntity map[string]int) error {
	for i := 0; i < total; i++ {
		memoryID := fmt.Sprintf("mem_hub_seed_%03d", i)
		if err := repo.InsertMemory(ctx, &core.Memory{
			ID:               memoryID,
			Type:             core.MemoryTypeFact,
			Scope:            core.ScopeGlobal,
			Body:             "seed memory",
			TightDescription: "seed memory",
			Confidence:       0.8,
			Importance:       0.5,
			PrivacyLevel:     core.PrivacyPrivate,
			Status:           core.MemoryStatusActive,
			CreatedAt:        now,
			UpdatedAt:        now,
		}); err != nil {
			return err
		}

		for entityID, linkedCount := range linksByEntity {
			if i >= linkedCount {
				continue
			}
			if err := repo.LinkMemoryEntity(ctx, memoryID, entityID, "mentioned"); err != nil {
				return err
			}
		}
	}
	return nil
}
