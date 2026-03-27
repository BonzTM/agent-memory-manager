package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestFindOrCreateEntityWithDetails_MergesAliases(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	seed := &core.Entity{ID: "ent_alias_merge", Type: "topic", CanonicalName: "AMM", CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEntity(ctx, seed); err != nil {
		t.Fatalf("insert seed entity: %v", err)
	}

	entity, err := svc.findOrCreateEntityWithDetails(ctx, core.EntityCandidate{
		CanonicalName: "agent-memory-manager",
		Type:          "technology",
		Aliases:       []string{"AMM"},
		Description:   "Agent Memory Manager",
	})
	if err != nil {
		t.Fatalf("findOrCreateEntityWithDetails: %v", err)
	}
	if entity == nil {
		t.Fatal("expected non-nil entity")
	}

	entities, err := repo.ListEntities(ctx, core.ListEntitiesOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list entities: %v", err)
	}
	if len(entities) != 1 {
		t.Fatalf("expected one merged entity, got %d", len(entities))
	}

	got := entities[0]
	if got.ID != seed.ID {
		t.Fatalf("expected merged entity to keep seed ID %s, got %s", seed.ID, got.ID)
	}
	if got.Type != "technology" {
		t.Fatalf("expected type upgrade to technology, got %q", got.Type)
	}
	if !containsFold(got.Aliases, "AMM") || !containsFold(got.Aliases, "agent-memory-manager") {
		t.Fatalf("expected aliases to include AMM and agent-memory-manager, got %v", got.Aliases)
	}
}

func TestFindOrCreateEntityWithDetails_UpgradesType(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	seed := &core.Entity{ID: "ent_type_upgrade", Type: "topic", CanonicalName: "AMM", CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEntity(ctx, seed); err != nil {
		t.Fatalf("insert seed entity: %v", err)
	}

	entity, err := svc.findOrCreateEntityWithDetails(ctx, core.EntityCandidate{CanonicalName: "AMM", Type: "technology"})
	if err != nil {
		t.Fatalf("findOrCreateEntityWithDetails: %v", err)
	}
	if entity == nil {
		t.Fatal("expected non-nil entity")
	}
	if entity.Type != "technology" {
		t.Fatalf("expected returned entity type technology, got %q", entity.Type)
	}

	updated, err := repo.GetEntity(ctx, seed.ID)
	if err != nil {
		t.Fatalf("get updated entity: %v", err)
	}
	if updated.Type != "technology" {
		t.Fatalf("expected persisted type technology, got %q", updated.Type)
	}
}

func TestFindOrCreateEntityWithDetails_FindsByAlias(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	seed := &core.Entity{ID: "ent_find_alias", Type: "service", CanonicalName: "AMM", Aliases: []string{"amm-tool"}, CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEntity(ctx, seed); err != nil {
		t.Fatalf("insert seed entity: %v", err)
	}

	entity, err := svc.findOrCreateEntityWithDetails(ctx, core.EntityCandidate{CanonicalName: "amm-tool", Type: "technology"})
	if err != nil {
		t.Fatalf("findOrCreateEntityWithDetails: %v", err)
	}
	if entity == nil {
		t.Fatal("expected existing entity")
	}
	if entity.ID != seed.ID {
		t.Fatalf("expected alias match to return %s, got %s", seed.ID, entity.ID)
	}

	entities, err := repo.ListEntities(ctx, core.ListEntitiesOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list entities: %v", err)
	}
	if len(entities) != 1 {
		t.Fatalf("expected alias match to avoid creating a new entity, got %d entities", len(entities))
	}
}

func TestLinkEntitiesFromAnalysis(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "AMM uses SQLite",
		TightDescription: "AMM uses SQLite",
	})
	if err != nil {
		t.Fatalf("remember memory: %v", err)
	}

	err = svc.linkEntitiesFromAnalysis(ctx, mem.ID, []core.EntityCandidate{
		{CanonicalName: "AMM", Type: "service", Aliases: []string{"agent-memory-manager"}},
		{CanonicalName: "SQLite", Type: "technology"},
	})
	if err != nil {
		t.Fatalf("linkEntitiesFromAnalysis: %v", err)
	}

	linked, err := repo.GetMemoryEntities(ctx, mem.ID)
	if err != nil {
		t.Fatalf("get linked entities: %v", err)
	}
	if len(linked) != 2 {
		t.Fatalf("expected 2 linked entities, got %d", len(linked))
	}

	names := map[string]bool{}
	for _, entity := range linked {
		names[strings.ToLower(entity.CanonicalName)] = true
	}
	if !names["amm"] || !names["sqlite"] {
		t.Fatalf("expected AMM and SQLite links, got %v", linked)
	}
}

func TestCreateRelationshipsFromAnalysis(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	from := &core.Entity{ID: "ent_rel_from", Type: "service", CanonicalName: "AMM", CreatedAt: now, UpdatedAt: now}
	to := &core.Entity{ID: "ent_rel_to", Type: "technology", CanonicalName: "SQLite", CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEntity(ctx, from); err != nil {
		t.Fatalf("insert from entity: %v", err)
	}
	if err := repo.InsertEntity(ctx, to); err != nil {
		t.Fatalf("insert to entity: %v", err)
	}

	err := svc.createRelationshipsFromAnalysis(ctx, []core.RelationshipCandidate{{
		FromEntity:  "AMM",
		ToEntity:    "SQLite",
		Type:        "uses",
		Description: "AMM uses SQLite",
	}})
	if err != nil {
		t.Fatalf("createRelationshipsFromAnalysis: %v", err)
	}

	rels, err := repo.ListRelationships(ctx, core.ListRelationshipsOptions{EntityID: from.ID, Limit: 10})
	if err != nil {
		t.Fatalf("list relationships: %v", err)
	}
	if len(rels) != 1 {
		t.Fatalf("expected 1 relationship, got %d", len(rels))
	}
	if rels[0].FromEntityID != from.ID || rels[0].ToEntityID != to.ID || rels[0].RelationshipType != "uses" {
		t.Fatalf("unexpected relationship: %+v", rels[0])
	}
}

func TestCreateRelationshipsFromAnalysis_SkipsUnknownEntities(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	known := &core.Entity{ID: "ent_rel_known", Type: "service", CanonicalName: "AMM", CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEntity(ctx, known); err != nil {
		t.Fatalf("insert known entity: %v", err)
	}

	err := svc.createRelationshipsFromAnalysis(ctx, []core.RelationshipCandidate{{
		FromEntity: "AMM",
		ToEntity:   "UnknownDB",
		Type:       "uses",
	}})
	if err != nil {
		t.Fatalf("createRelationshipsFromAnalysis should skip unknown entities without error: %v", err)
	}

	rels, err := repo.ListRelationships(ctx, core.ListRelationshipsOptions{EntityID: known.ID, Limit: 10})
	if err != nil {
		t.Fatalf("list relationships: %v", err)
	}
	if len(rels) != 0 {
		t.Fatalf("expected no relationships when a referenced entity is unknown, got %d", len(rels))
	}
}

func TestEntityOverlap_MatchesAliases(t *testing.T) {
	item := ScoringCandidate{
		EntityNames:   []string{"agent-memory-manager"},
		EntityAliases: []string{"AMM"},
	}

	score := signalEntityOverlap(item, []string{"AMM"}, nil)
	if score <= 0 {
		t.Fatalf("expected alias-based entity overlap match, got %f", score)
	}
}

func containsFold(values []string, needle string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(needle)) {
			return true
		}
	}
	return false
}
