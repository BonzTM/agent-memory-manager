package service

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestEnrichMemories_ExtractsEntitiesForRememberedMemory(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Subject:          "deployment",
		Body:             "Josh uses Kubernetes for AMM deployments",
		TightDescription: "Josh uses Kubernetes for AMM",
	})
	if err != nil {
		t.Fatalf("remember memory: %v", err)
	}

	count, err := svc.EnrichMemories(ctx)
	if err != nil {
		t.Fatalf("enrich memories: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 enriched memory, got %d", count)
	}

	updated, err := repo.GetMemory(ctx, mem.ID)
	if err != nil {
		t.Fatalf("get enriched memory: %v", err)
	}
	if updated.Metadata[MetaEntitiesExtracted] != "true" {
		t.Fatalf("expected %s=true, got %q", MetaEntitiesExtracted, updated.Metadata[MetaEntitiesExtracted])
	}

	entities, err := repo.GetMemoryEntities(ctx, mem.ID)
	if err != nil {
		t.Fatalf("get memory entities: %v", err)
	}
	if len(entities) == 0 {
		t.Fatal("expected linked entities for enriched memory")
	}

	linked := make(map[string]bool, len(entities))
	for _, ent := range entities {
		linked[strings.ToLower(ent.CanonicalName)] = true
	}
	if !linked["josh"] {
		t.Fatalf("expected Josh entity link, got %v", entities)
	}
	if !linked["kubernetes"] {
		t.Fatalf("expected Kubernetes entity link, got %v", entities)
	}
}

func TestEnrichMemories_SkipsAlreadyEnriched(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "Josh uses Kubernetes",
		TightDescription: "Josh uses Kubernetes",
	})
	if err != nil {
		t.Fatalf("remember memory: %v", err)
	}

	firstCount, err := svc.EnrichMemories(ctx)
	if err != nil {
		t.Fatalf("first enrich: %v", err)
	}
	if firstCount != 1 {
		t.Fatalf("expected first enrich to process 1 memory, got %d", firstCount)
	}

	secondCount, err := svc.EnrichMemories(ctx)
	if err != nil {
		t.Fatalf("second enrich: %v", err)
	}
	if secondCount != 0 {
		t.Fatalf("expected second enrich to process 0 memories, got %d", secondCount)
	}
}

func TestEnrichMemories_DoesNotModifyBody(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	original, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Subject:          "deployments",
		Body:             "Josh uses Kubernetes for AMM deployments",
		TightDescription: "deployment detail",
		Confidence:       0.91,
		Importance:       0.73,
	})
	if err != nil {
		t.Fatalf("remember memory: %v", err)
	}

	_, err = svc.EnrichMemories(ctx)
	if err != nil {
		t.Fatalf("enrich memories: %v", err)
	}

	updated, err := repo.GetMemory(ctx, original.ID)
	if err != nil {
		t.Fatalf("get enriched memory: %v", err)
	}

	if updated.Body != original.Body {
		t.Fatalf("expected body unchanged, got %q", updated.Body)
	}
	if updated.Subject != original.Subject {
		t.Fatalf("expected subject unchanged, got %q", updated.Subject)
	}
	if updated.Type != original.Type {
		t.Fatalf("expected type unchanged, got %s", updated.Type)
	}
	if updated.TightDescription != original.TightDescription {
		t.Fatalf("expected tight_description unchanged, got %q", updated.TightDescription)
	}
	if math.Abs(updated.Confidence-original.Confidence) > 0.000001 {
		t.Fatalf("expected confidence unchanged, got %f", updated.Confidence)
	}
	if math.Abs(updated.Importance-original.Importance) > 0.000001 {
		t.Fatalf("expected importance unchanged, got %f", updated.Importance)
	}
}

func TestEnrichMemories_CreatesRelationshipsFromAnalysis(t *testing.T) {
	svc, repo := testServiceAndRepoWithSummarizer(t, reflectTestSummarizer{})
	ctx := context.Background()
	svc.SetIntelligenceProvider(enrichAnalysisStub{
		isLLM: true,
		result: &core.AnalysisResult{
			Entities: []core.EntityCandidate{
				{CanonicalName: "API Gateway", Type: "service", Aliases: []string{"api gateway"}},
				{CanonicalName: "Redis Cache", Type: "technology", Aliases: []string{"redis cache"}},
			},
			Relationships: []core.RelationshipCandidate{{
				FromEntity: "API Gateway",
				ToEntity:   "Redis Cache",
				Type:       "uses",
			}},
		},
	})

	mem, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "API Gateway uses Redis Cache for throttling",
		TightDescription: "api gateway redis cache",
	})
	if err != nil {
		t.Fatalf("remember memory: %v", err)
	}

	count, err := svc.EnrichMemories(ctx)
	if err != nil {
		t.Fatalf("enrich memories: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 enriched memory, got %d", count)
	}

	updated, err := repo.GetMemory(ctx, mem.ID)
	if err != nil {
		t.Fatalf("get enriched memory: %v", err)
	}
	if updated.Metadata[MetaEntitiesExtractedMethod] != MethodLLM {
		t.Fatalf("expected llm entity extraction method, got %q", updated.Metadata[MetaEntitiesExtractedMethod])
	}

	apiEntities, err := repo.SearchEntities(ctx, "API Gateway", 10)
	if err != nil {
		t.Fatalf("search api gateway entity: %v", err)
	}
	redisEntities, err := repo.SearchEntities(ctx, "Redis Cache", 10)
	if err != nil {
		t.Fatalf("search redis cache entity: %v", err)
	}
	if len(apiEntities) == 0 || len(redisEntities) == 0 {
		t.Fatalf("expected analysis entities to be created, got api=%#v redis=%#v", apiEntities, redisEntities)
	}

	rels, err := repo.ListRelationshipsByEntityIDs(ctx, []string{apiEntities[0].ID, redisEntities[0].ID})
	if err != nil {
		t.Fatalf("list relationships by entity ids: %v", err)
	}
	foundUses := false
	for _, rel := range rels {
		if rel.FromEntityID == apiEntities[0].ID && rel.ToEntityID == redisEntities[0].ID && rel.RelationshipType == "uses" {
			foundUses = true
			break
		}
	}
	if !foundUses {
		t.Fatalf("expected analysis relationship API Gateway -> Redis Cache uses, got %#v", rels)
	}
}

func TestEnrichMemories_RunJob(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	_, err := svc.Remember(ctx, &core.Memory{
		Type:             core.MemoryTypeFact,
		Scope:            core.ScopeGlobal,
		Body:             "Josh uses Kubernetes",
		TightDescription: "Josh uses Kubernetes",
	})
	if err != nil {
		t.Fatalf("remember memory: %v", err)
	}

	job, err := svc.RunJob(ctx, "enrich_memories")
	if err != nil {
		t.Fatalf("run enrich_memories job: %v", err)
	}
	if job.Status != "completed" {
		t.Fatalf("expected completed job, got %s", job.Status)
	}
	if job.Result["action"] != "enrich_memories" {
		t.Fatalf("expected action enrich_memories, got %q", job.Result["action"])
	}
	if job.Result["memories_enriched"] != "1" {
		t.Fatalf("expected memories_enriched=1, got %q", job.Result["memories_enriched"])
	}
}
