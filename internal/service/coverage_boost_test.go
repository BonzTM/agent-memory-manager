package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

type adapterTestSummarizer struct {
	batch []core.MemoryCandidate
}

func (a adapterTestSummarizer) Summarize(_ context.Context, content string, maxLen int) (string, error) {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) <= maxLen {
		return trimmed, nil
	}
	return trimmed[:maxLen], nil
}

func (a adapterTestSummarizer) ExtractMemoryCandidate(context.Context, string) ([]core.MemoryCandidate, error) {
	return nil, nil
}

func (a adapterTestSummarizer) ExtractMemoryCandidateBatch(context.Context, []string) ([]core.MemoryCandidate, error) {
	return append([]core.MemoryCandidate(nil), a.batch...), nil
}

type enrichAnalysisStub struct {
	result *core.AnalysisResult
}

func (e enrichAnalysisStub) Summarize(context.Context, string, int) (string, error) {
	return "", nil
}

func (e enrichAnalysisStub) ExtractMemoryCandidate(context.Context, string) ([]core.MemoryCandidate, error) {
	return nil, nil
}

func (e enrichAnalysisStub) ExtractMemoryCandidateBatch(context.Context, []string) ([]core.MemoryCandidate, error) {
	return nil, nil
}

func (e enrichAnalysisStub) AnalyzeEvents(context.Context, []core.EventContent) (*core.AnalysisResult, error) {
	if e.result == nil {
		return &core.AnalysisResult{}, nil
	}
	return e.result, nil
}

func (e enrichAnalysisStub) TriageEvents(context.Context, []core.EventContent) (map[int]core.TriageDecision, error) {
	return map[int]core.TriageDecision{}, nil
}

func (e enrichAnalysisStub) ReviewMemories(context.Context, []core.MemoryReview) (*core.ReviewResult, error) {
	return &core.ReviewResult{}, nil
}

func (e enrichAnalysisStub) CompressEventBatches(context.Context, []core.EventChunk) ([]core.CompressionResult, error) {
	return nil, nil
}

func (e enrichAnalysisStub) SummarizeTopicBatches(context.Context, []core.TopicChunk) ([]core.CompressionResult, error) {
	return nil, nil
}

func (e enrichAnalysisStub) ConsolidateNarrative(context.Context, []core.EventContent, []core.MemorySummary) (*core.NarrativeResult, error) {
	return &core.NarrativeResult{}, nil
}

func TestService_SettersAndResetDerived(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	svc.SetReprocessBatchSize(7)
	if svc.reprocessBatchSize != 7 {
		t.Fatalf("expected reprocess batch size 7, got %d", svc.reprocessBatchSize)
	}
	svc.SetReprocessBatchSize(0)
	if svc.reprocessBatchSize != defaultBatchSize {
		t.Fatalf("expected default reprocess batch size %d, got %d", defaultBatchSize, svc.reprocessBatchSize)
	}

	svc.SetReflectBatchSize(9)
	svc.SetReflectLLMBatchSize(11)
	svc.SetCompressChunkSize(13)
	svc.SetCompressMaxEvents(15)
	svc.SetCompressBatchSize(17)
	svc.SetTopicBatchSize(19)
	svc.SetEmbeddingBatchSize(21)
	svc.SetCrossProjectSimilarityThreshold(0.42)

	if svc.reflectBatchSize != 9 || svc.reflectLLMBatchSize != 11 || svc.compressChunkSize != 13 || svc.compressMaxEvents != 15 || svc.compressBatchSize != 17 || svc.topicBatchSize != 19 || svc.embeddingBatchSize != 21 {
		t.Fatalf("setter write mismatch: %+v", svc)
	}
	if svc.crossProjectSimilarityThreshold != 0.42 {
		t.Fatalf("expected custom cross-project threshold, got %.2f", svc.crossProjectSimilarityThreshold)
	}

	svc.SetReflectBatchSize(-1)
	svc.SetReflectLLMBatchSize(-1)
	svc.SetCompressChunkSize(-1)
	svc.SetCompressMaxEvents(-1)
	svc.SetCompressBatchSize(-1)
	svc.SetTopicBatchSize(-1)
	svc.SetEmbeddingBatchSize(-1)
	svc.SetCrossProjectSimilarityThreshold(0)

	if svc.reflectBatchSize != defaultReflectBatchSize || svc.reflectLLMBatchSize != defaultReflectLLMBatchSize || svc.compressChunkSize != defaultCompressChunkSize || svc.compressMaxEvents != defaultCompressMaxEvents || svc.compressBatchSize != defaultCompressBatchSize || svc.topicBatchSize != defaultTopicBatchSize || svc.embeddingBatchSize != defaultEmbeddingBatchSize {
		t.Fatalf("expected defaults restored by non-positive setters")
	}
	if svc.crossProjectSimilarityThreshold != defaultCrossProjectSimilarityThreshold {
		t.Fatalf("expected default similarity threshold, got %f", svc.crossProjectSimilarityThreshold)
	}

	svc.SetIntelligenceProvider(nil)
	if svc.intelligence == nil {
		t.Fatal("expected default intelligence provider when setting nil")
	}
	custom := consolidateTestIntelligence{}
	svc.SetIntelligenceProvider(custom)
	if _, ok := svc.intelligence.(consolidateTestIntelligence); !ok {
		t.Fatalf("expected custom intelligence provider to be set, got %T", svc.intelligence)
	}

	if _, err := svc.ResetDerived(ctx); err != nil {
		t.Fatalf("ResetDerived: %v", err)
	}
}

func TestSummarizerIntelligenceAdapter_FullSurface(t *testing.T) {
	adapter := NewSummarizerIntelligenceAdapter(adapterTestSummarizer{batch: []core.MemoryCandidate{{
		Type:             core.MemoryTypeFact,
		Body:             "Redis is used for caching",
		TightDescription: "Redis cache",
		Confidence:       0.9,
		SourceEventNums:  []int{1},
	}}})

	ctx := context.Background()
	events := []core.EventContent{{Index: 1, Content: "API Gateway uses Redis Cache"}, {Index: 2, Content: "Kafka streams events"}}

	analysis, err := adapter.AnalyzeEvents(ctx, events)
	if err != nil {
		t.Fatalf("AnalyzeEvents: %v", err)
	}
	if len(analysis.Memories) != 1 || len(analysis.Entities) == 0 {
		t.Fatalf("expected memory and entities from adapter analysis, got %+v", analysis)
	}

	triage, err := adapter.TriageEvents(ctx, []core.EventContent{{Index: 1, Content: "heartbeat"}, {Index: 2, Content: "We decided to switch to Redis for cache"}})
	if err != nil {
		t.Fatalf("TriageEvents: %v", err)
	}
	if triage[1] != core.TriageSkip || triage[2] != core.TriageHighPriority {
		t.Fatalf("unexpected triage decisions: %#v", triage)
	}

	review, err := adapter.ReviewMemories(ctx, []core.MemoryReview{{ID: "mem_1", Body: "body"}})
	if err != nil {
		t.Fatalf("ReviewMemories: %v", err)
	}
	if review == nil {
		t.Fatal("expected non-nil review result")
	}

	compressed, err := adapter.CompressEventBatches(ctx, []core.EventChunk{{Index: 4, Contents: []string{"alpha", "beta"}}})
	if err != nil {
		t.Fatalf("CompressEventBatches: %v", err)
	}
	if len(compressed) != 1 || compressed[0].Index != 4 || compressed[0].Body == "" || compressed[0].TightDescription == "" {
		t.Fatalf("unexpected compressed result: %#v", compressed)
	}

	topics, err := adapter.SummarizeTopicBatches(ctx, []core.TopicChunk{{Index: 5, Title: "cache", Contents: []string{"redis", "eviction"}}})
	if err != nil {
		t.Fatalf("SummarizeTopicBatches: %v", err)
	}
	if len(topics) != 1 || topics[0].Index != 5 || topics[0].Body == "" || topics[0].TightDescription == "" {
		t.Fatalf("unexpected topic summary result: %#v", topics)
	}

	narrative, err := adapter.ConsolidateNarrative(ctx, []core.EventContent{{Index: 1, Content: "first"}, {Index: 2, Content: "second"}}, nil)
	if err != nil {
		t.Fatalf("ConsolidateNarrative: %v", err)
	}
	if narrative == nil || narrative.Summary == "" || narrative.TightDesc == "" {
		t.Fatalf("unexpected narrative result: %#v", narrative)
	}
}

func TestEnrichMemories_UsesAnalysisEntitiesAndFallbackHeuristic(t *testing.T) {
	svc, repo := testServiceAndRepoWithSummarizer(t, reflectTestSummarizer{})
	ctx := context.Background()
	svc.hasLLMSummarizer = true
	svc.SetIntelligenceProvider(enrichAnalysisStub{result: &core.AnalysisResult{Entities: []core.EntityCandidate{
		{CanonicalName: "API Gateway", Type: "service", Aliases: []string{"api"}},
		{CanonicalName: "Redis Cache", Type: "technology", Aliases: []string{"redis"}},
	}}})

	withAliases, err := svc.Remember(ctx, &core.Memory{Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "API uses redis for request throttling", TightDescription: "api redis"})
	if err != nil {
		t.Fatalf("remember with aliases: %v", err)
	}
	withoutAliases, err := svc.Remember(ctx, &core.Memory{Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "scheduler writes cron plans", TightDescription: "scheduler"})
	if err != nil {
		t.Fatalf("remember without aliases: %v", err)
	}

	enriched, err := svc.EnrichMemories(ctx)
	if err != nil {
		t.Fatalf("EnrichMemories: %v", err)
	}
	if enriched != 2 {
		t.Fatalf("expected 2 enriched memories, got %d", enriched)
	}

	updatedWithAliases, err := repo.GetMemory(ctx, withAliases.ID)
	if err != nil {
		t.Fatalf("get enriched alias memory: %v", err)
	}
	if updatedWithAliases.Metadata[MetaEntitiesExtractedMethod] != MethodLLM {
		t.Fatalf("expected llm entity extraction method, got %q", updatedWithAliases.Metadata[MetaEntitiesExtractedMethod])
	}

	updatedWithoutAliases, err := repo.GetMemory(ctx, withoutAliases.ID)
	if err != nil {
		t.Fatalf("get enriched fallback memory: %v", err)
	}
	if updatedWithoutAliases.Metadata[MetaEntitiesExtractedMethod] != MethodHeuristic {
		t.Fatalf("expected heuristic entity extraction fallback, got %q", updatedWithoutAliases.Metadata[MetaEntitiesExtractedMethod])
	}
}

func TestRecallEntityExpansionAndHubDampening(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	api := &core.Entity{ID: "ent_recall_api", Type: "service", CanonicalName: "API Gateway", Aliases: []string{"api"}, CreatedAt: now, UpdatedAt: now}
	redis := &core.Entity{ID: "ent_recall_redis", Type: "technology", CanonicalName: "Redis Cache", Aliases: []string{"redis"}, CreatedAt: now, UpdatedAt: now}
	rate := &core.Entity{ID: "ent_recall_rate", Type: "component", CanonicalName: "Rate Limiter", CreatedAt: now, UpdatedAt: now}
	for _, entity := range []*core.Entity{api, redis, rate} {
		if err := repo.InsertEntity(ctx, entity); err != nil {
			t.Fatalf("insert entity %s: %v", entity.ID, err)
		}
	}

	for i := 0; i < 12; i++ {
		mem := &core.Memory{ID: fmt.Sprintf("mem_recall_hub_%d", i), Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "hub memory", TightDescription: "hub", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now}
		if err := repo.InsertMemory(ctx, mem); err != nil {
			t.Fatalf("insert hub memory %d: %v", i, err)
		}
		if err := repo.LinkMemoryEntity(ctx, mem.ID, api.ID, "mentioned"); err != nil {
			t.Fatalf("link hub memory %d to api entity: %v", i, err)
		}
	}

	for _, rel := range []*core.Relationship{
		{ID: "rel_api_redis", FromEntityID: api.ID, ToEntityID: redis.ID, RelationshipType: "uses", CreatedAt: now, UpdatedAt: now},
		{ID: "rel_redis_rate", FromEntityID: redis.ID, ToEntityID: rate.ID, RelationshipType: "supports", CreatedAt: now, UpdatedAt: now},
	} {
		if err := repo.InsertRelationship(ctx, rel); err != nil {
			t.Fatalf("insert relationship %s: %v", rel.ID, err)
		}
	}

	if err := repo.RebuildEntityGraphProjection(ctx); err != nil {
		t.Fatalf("rebuild entity graph projection: %v", err)
	}

	related, err := svc.listRelatedEntitiesForRecall(ctx, api.ID)
	if err != nil {
		t.Fatalf("listRelatedEntitiesForRecall: %v", err)
	}
	if len(related) == 0 {
		t.Fatalf("expected projected related entities, got %#v", related)
	}

	weights := svc.expandQueryEntities(ctx, []string{"API Gateway"})
	if len(weights) == 0 {
		t.Fatalf("expected expanded entity weights, got %#v", weights)
	}
	if _, ok := weights[normalizeEntityTerm("redis")]; !ok {
		t.Fatalf("expected related entity alias in expanded weights, got %#v", weights)
	}
	if hubWeight, ok := weights[normalizeEntityTerm("api gateway")]; !ok || hubWeight >= 1.0 {
		t.Fatalf("expected dampened api gateway weight < 1.0, got %f in %#v", hubWeight, weights)
	}
}

func TestMemoryCandidateEmbeddingDuplicateHelpers(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	candidate := core.Memory{ID: "mem_candidate_cov", Type: core.MemoryTypeFact, Scope: core.ScopeProject, ProjectID: "proj_cov", Body: "Use Redis cache", TightDescription: "redis cache", Status: core.MemoryStatusActive}
	existingSame := &core.Memory{ID: "mem_existing_same", Type: core.MemoryTypeFact, Scope: core.ScopeProject, ProjectID: "proj_cov", Body: "Use Redis cache", TightDescription: "redis cache", Confidence: 0.7, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now}
	existingDifferent := &core.Memory{ID: "mem_existing_diff", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "different", TightDescription: "different", Confidence: 0.7, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now}
	for _, mem := range []*core.Memory{existingSame, existingDifferent} {
		if err := repo.InsertMemory(ctx, mem); err != nil {
			t.Fatalf("insert memory %s: %v", mem.ID, err)
		}
	}

	candidateText := buildMemoryEmbeddingText(&candidate)
	provider := staticEmbeddingProvider{model: "cov-model", vectors: map[string][]float32{candidateText: {1, 0, 0}}}
	svc.embeddingProvider = provider

	if err := repo.UpsertEmbedding(ctx, &core.EmbeddingRecord{ObjectID: existingSame.ID, ObjectKind: "memory", Model: provider.Model(), Vector: []float32{1, 0, 0}, CreatedAt: now}); err != nil {
		t.Fatalf("upsert embedding existing same: %v", err)
	}
	if err := repo.UpsertEmbedding(ctx, &core.EmbeddingRecord{ObjectID: existingDifferent.ID, ObjectKind: "memory", Model: provider.Model(), Vector: []float32{1, 0, 0}, CreatedAt: now}); err != nil {
		t.Fatalf("upsert embedding existing different: %v", err)
	}

	dupesByGenerated := svc.findDuplicatesByEmbedding(ctx, candidate, []*core.Memory{existingSame, existingDifferent})
	if len(dupesByGenerated) != 1 || dupesByGenerated[0].ID != existingSame.ID {
		t.Fatalf("expected only same-scope embedding duplicate, got %#v", dupesByGenerated)
	}

	candidate.CreatedAt = now
	candidate.UpdatedAt = now
	candidate.Confidence = 0.8
	candidate.Importance = 0.5
	candidate.PrivacyLevel = core.PrivacyPrivate
	if err := repo.InsertMemory(ctx, &candidate); err != nil {
		t.Fatalf("insert candidate memory: %v", err)
	}
	if err := repo.UpsertEmbedding(ctx, &core.EmbeddingRecord{ObjectID: candidate.ID, ObjectKind: "memory", Model: provider.Model(), Vector: []float32{1, 0, 0}, CreatedAt: now}); err != nil {
		t.Fatalf("upsert candidate embedding: %v", err)
	}

	dupesByStored := svc.findDuplicatesByStoredEmbedding(ctx, candidate, []*core.Memory{&candidate, existingSame, existingDifferent})
	if len(dupesByStored) != 1 || dupesByStored[0].ID != existingSame.ID {
		t.Fatalf("expected only same-scope stored duplicate, got %#v", dupesByStored)
	}
}

func TestRecallModes_IncludeEmbeddingOnlyCandidates(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	query := "semantic embedding query"
	vector := []float32{0.7, 0.2, 0.1}

	mem := &core.Memory{ID: "mem_embed_only", Type: core.MemoryTypeFact, Scope: core.ScopeProject, ProjectID: "proj_embed", AgentID: "agent-a", Body: "non matching lexical body", TightDescription: "semantic memory", Confidence: 0.9, Importance: 0.6, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertMemory(ctx, mem); err != nil {
		t.Fatalf("insert memory: %v", err)
	}
	summary := &core.Summary{ID: "sum_embed_only", Kind: "leaf", Scope: core.ScopeProject, ProjectID: "proj_embed", Title: "summary title", Body: "summary body", TightDescription: "summary tight", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertSummary(ctx, summary); err != nil {
		t.Fatalf("insert summary: %v", err)
	}
	episode := &core.Episode{ID: "epi_embed_only", Scope: core.ScopeProject, ProjectID: "proj_embed", Title: "episode title", Summary: "episode summary", TightDescription: "episode tight", Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEpisode(ctx, episode); err != nil {
		t.Fatalf("insert episode: %v", err)
	}
	entity := &core.Entity{ID: "ent_embed_only", Type: "service", CanonicalName: "SemanticNode", Aliases: []string{"semantic node"}, CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEntity(ctx, entity); err != nil {
		t.Fatalf("insert entity: %v", err)
	}

	provider := staticEmbeddingProvider{model: "embed-recall-model", vectors: map[string][]float32{
		query:                             vector,
		buildMemoryEmbeddingText(mem):     vector,
		buildSummaryEmbeddingText(summary): vector,
		buildEpisodeEmbeddingText(episode): vector,
	}}
	svc.embeddingProvider = provider

	for _, record := range []*core.EmbeddingRecord{
		{ObjectID: mem.ID, ObjectKind: "memory", Model: provider.Model(), Vector: vector, CreatedAt: now},
		{ObjectID: summary.ID, ObjectKind: "summary", Model: provider.Model(), Vector: vector, CreatedAt: now},
		{ObjectID: episode.ID, ObjectKind: "episode", Model: provider.Model(), Vector: vector, CreatedAt: now},
	} {
		if err := repo.UpsertEmbedding(ctx, record); err != nil {
			t.Fatalf("upsert embedding %s: %v", record.ObjectID, err)
		}
	}

	ambient, err := svc.Recall(ctx, query, core.RecallOptions{Mode: core.RecallModeAmbient, Limit: 10, AgentID: "agent-a", ProjectID: "proj_embed"})
	if err != nil || len(ambient.Items) == 0 {
		t.Fatalf("ambient recall failed: err=%v items=%d", err, len(ambient.Items))
	}

	project, err := svc.Recall(ctx, query, core.RecallOptions{Mode: core.RecallModeProject, Limit: 10, AgentID: "agent-a", ProjectID: "proj_embed"})
	if err != nil || len(project.Items) == 0 {
		t.Fatalf("project recall failed: err=%v items=%d", err, len(project.Items))
	}

	entityRecall, err := svc.Recall(ctx, "SemanticNode", core.RecallOptions{Mode: core.RecallModeEntity, Limit: 10, AgentID: "agent-a"})
	if err != nil || len(entityRecall.Items) == 0 {
		t.Fatalf("entity recall failed: err=%v items=%d", err, len(entityRecall.Items))
	}

	hybrid, err := svc.Recall(ctx, query, core.RecallOptions{Mode: core.RecallModeHybrid, Limit: 10, AgentID: "agent-a"})
	if err != nil || len(hybrid.Items) == 0 {
		t.Fatalf("hybrid recall failed: err=%v items=%d", err, len(hybrid.Items))
	}
}

func TestEmbeddings_RebuildFullAndSummaryUpsertHelpers(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	provider := staticEmbeddingProvider{model: "embed-full-model", vectors: map[string][]float32{}}
	svc.embeddingProvider = provider
	svc.SetEmbeddingBatchSize(2)

	memory := &core.Memory{ID: "mem_embed_full", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Subject: "subj", Body: "body", TightDescription: "tight", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertMemory(ctx, memory); err != nil {
		t.Fatalf("insert memory: %v", err)
	}
	summary := &core.Summary{ID: "sum_embed_full", Kind: "leaf", Scope: core.ScopeGlobal, Title: "sum title", Body: "sum body", TightDescription: "sum tight", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertSummary(ctx, summary); err != nil {
		t.Fatalf("insert summary: %v", err)
	}
	episode := &core.Episode{ID: "epi_embed_full", Scope: core.ScopeGlobal, Title: "episode title", Summary: "episode body", TightDescription: "episode tight", Importance: 0.4, PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEpisode(ctx, episode); err != nil {
		t.Fatalf("insert episode: %v", err)
	}

	provider.vectors[buildMemoryEmbeddingText(memory)] = []float32{1, 0}
	provider.vectors[buildSummaryEmbeddingText(summary)] = []float32{0, 1}
	provider.vectors[buildEpisodeEmbeddingText(episode)] = []float32{0.5, 0.5}
	svc.embeddingProvider = provider

	if got := buildEpisodeEmbeddingText(nil); got != "" {
		t.Fatalf("expected empty embedding text for nil episode, got %q", got)
	}
	if got := buildEpisodeEmbeddingText(episode); got == "" {
		t.Fatal("expected non-empty episode embedding text")
	}

	svc.upsertSummaryEmbeddingBestEffort(ctx, summary)
	if _, err := repo.GetEmbedding(ctx, summary.ID, "summary", provider.Model()); err != nil {
		t.Fatalf("expected summary embedding from helper upsert: %v", err)
	}

	if err := svc.rebuildEmbeddings(ctx, true); err != nil {
		t.Fatalf("rebuildEmbeddings forceAll: %v", err)
	}

	if _, err := repo.GetEmbedding(ctx, memory.ID, "memory", provider.Model()); err != nil {
		t.Fatalf("expected rebuilt memory embedding: %v", err)
	}
	if _, err := repo.GetEmbedding(ctx, episode.ID, "episode", provider.Model()); err != nil {
		t.Fatalf("expected rebuilt episode embedding: %v", err)
	}
}

func TestService_ErrorAndValidationBranches(t *testing.T) {
	svc, _ := testServiceAndRepo(t)
	ctx := context.Background()

	if _, err := svc.GetMemory(ctx, "missing-memory-id"); err == nil {
		t.Fatal("expected GetMemory to fail for missing id")
	}
	if _, err := svc.GetSummary(ctx, "missing-summary-id"); err == nil {
		t.Fatal("expected GetSummary to fail for missing id")
	}
	if _, err := svc.GetEpisode(ctx, "missing-episode-id"); err == nil {
		t.Fatal("expected GetEpisode to fail for missing id")
	}
	if _, err := svc.GetEntity(ctx, "missing-entity-id"); err == nil {
		t.Fatal("expected GetEntity to fail for missing id")
	}

	if _, err := svc.ShareMemory(ctx, "", core.PrivacyShared); err == nil {
		t.Fatal("expected ShareMemory to fail for empty id")
	}
	if _, err := svc.ShareMemory(ctx, "missing-memory-id", core.PrivacyLevel("invalid")); err == nil {
		t.Fatal("expected ShareMemory to fail for invalid privacy")
	}
	if _, err := svc.ShareMemory(ctx, "missing-memory-id", core.PrivacyShared); err == nil {
		t.Fatal("expected ShareMemory to fail for missing memory")
	}

	if _, err := svc.ForgetMemory(ctx, ""); err == nil {
		t.Fatal("expected ForgetMemory to fail for empty id")
	}
	if _, err := svc.ForgetMemory(ctx, "missing-memory-id"); err == nil {
		t.Fatal("expected ForgetMemory to fail for missing memory")
	}

	events := []*core.Event{
		{ID: "evt_transcript_dup", Kind: "message_user", SourceSystem: "test", PrivacyLevel: core.PrivacyPrivate, Content: "valid event", OccurredAt: time.Now().UTC()},
		{ID: "evt_transcript_dup", Kind: "message_user", SourceSystem: "test", PrivacyLevel: core.PrivacyPrivate, Content: "duplicate id event", OccurredAt: time.Now().UTC()},
	}
	ingested, err := svc.IngestTranscript(ctx, events)
	if err == nil {
		t.Fatal("expected IngestTranscript to fail on invalid second event")
	}
	if ingested != 1 {
		t.Fatalf("expected one ingested event before transcript failure, got %d", ingested)
	}

	if _, err := svc.CheckIngestionPolicy(ctx, &core.Event{Kind: "message_user", SourceSystem: "test", Content: "policy content"}); err != nil {
		t.Fatalf("CheckIngestionPolicy should succeed: %v", err)
	}
}

func TestMemoryCandidateHelperBranches(t *testing.T) {
	now := time.Now().UTC()
	retracted := &core.Memory{ID: "mem_retracted", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "redis for cache", TightDescription: "redis cache", Status: core.MemoryStatusRetracted, CreatedAt: now}
	active := &core.Memory{ID: "mem_active", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "postgres for storage", TightDescription: "postgres", Status: core.MemoryStatusActive, Confidence: 0.7, CreatedAt: now.Add(-time.Hour)}
	newer := &core.Memory{ID: "mem_newer", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "postgres for storage", TightDescription: "postgres", Status: core.MemoryStatusActive, Confidence: 0.7, CreatedAt: now}

	if !matchesRetractedMemory([]*core.Memory{retracted, active}, core.Memory{Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "redis for cache", TightDescription: "redis cache"}) {
		t.Fatal("expected duplicate candidate to match retracted memory")
	}
	if keeper := selectDuplicateKeeper([]*core.Memory{active, newer}); keeper == nil || keeper.ID != newer.ID {
		t.Fatalf("expected newer equal-confidence memory as keeper, got %#v", keeper)
	}
	if !shouldUpgradeDuplicateContent(active, core.Memory{Confidence: 0.8}, MethodHeuristic) {
		t.Fatal("expected confidence-based duplicate upgrade")
	}
	if !shouldUpgradeDuplicateContent(&core.Memory{Metadata: map[string]string{MetaExtractionMethod: MethodHeuristic}}, core.Memory{Confidence: 0.1}, MethodLLM) {
		t.Fatal("expected LLM duplicate upgrade over heuristic extraction")
	}

	if clampUnit(-1) != 0 || clampUnit(2) != 1 || clampUnit(0.3) != 0.3 {
		t.Fatalf("unexpected clampUnit behavior")
	}

	imp := 1.4
	if got := importanceForCandidate(core.MemoryCandidate{Type: core.MemoryTypeDecision, Importance: &imp}); got != 1.0 {
		t.Fatalf("expected explicit importance to clamp to 1.0, got %f", got)
	}
	if got := importanceForCandidate(core.MemoryCandidate{Type: core.MemoryTypePreference}); got <= 0 {
		t.Fatalf("expected derived importance > 0, got %f", got)
	}
}

func TestLifecycleReviewAndLLMUtilityHelpers(t *testing.T) {
	svc, _ := testServiceAndRepo(t)

	svc.SetLifecycleReviewBatchSize(0)
	if svc.lifecycleReviewBatchSize != defaultLifecycleReviewBatchSize {
		t.Fatalf("expected default lifecycle review batch size, got %d", svc.lifecycleReviewBatchSize)
	}
	svc.SetLifecycleReviewBatchSize(7)
	if svc.lifecycleReviewBatchSize != 7 {
		t.Fatalf("expected custom lifecycle review batch size, got %d", svc.lifecycleReviewBatchSize)
	}

	if maxFloat(0.2, 0.8) != 0.8 || minFloat(0.2, 0.8) != 0.2 {
		t.Fatal("unexpected min/max float helpers")
	}

	if got := memoryAgeHint(""); got != "unknown" {
		t.Fatalf("expected unknown age hint for empty timestamp, got %q", got)
	}
	if got := memoryAgeHint("bad timestamp"); got != "unknown" {
		t.Fatalf("expected unknown age hint for invalid timestamp, got %q", got)
	}
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	if got := memoryAgeHint(future); got != "0d" {
		t.Fatalf("expected future timestamp to map to 0d, got %q", got)
	}

	provider := NewLLMIntelligenceProviderWithReviewConfig(NewLLMSummarizer("http://example.com", "k", "m"), "http://review.example.com", "", "")
	if provider == nil || provider.reviewChatComplete == nil {
		t.Fatal("expected review-configured LLM intelligence provider")
	}
}

func TestRecallInternalModes_EmbeddingAndVisibilityBranches(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	visible := &core.Memory{ID: "mem_recall_visible", Type: core.MemoryTypeFact, Scope: core.ScopeProject, ProjectID: "proj_recall", AgentID: "agent-a", Body: "visible memory", TightDescription: "visible", Confidence: 0.9, Importance: 0.6, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now}
	shared := &core.Memory{ID: "mem_recall_shared", Type: core.MemoryTypeFact, Scope: core.ScopeProject, ProjectID: "proj_recall", AgentID: "agent-b", Body: "shared memory", TightDescription: "shared", Confidence: 0.9, Importance: 0.6, PrivacyLevel: core.PrivacyShared, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now}
	hidden := &core.Memory{ID: "mem_recall_hidden", Type: core.MemoryTypeFact, Scope: core.ScopeProject, ProjectID: "proj_recall", AgentID: "agent-b", Body: "hidden memory", TightDescription: "hidden", Confidence: 0.9, Importance: 0.6, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now}
	otherProject := &core.Memory{ID: "mem_recall_other_project", Type: core.MemoryTypeFact, Scope: core.ScopeProject, ProjectID: "proj_other", AgentID: "agent-a", Body: "other project memory", TightDescription: "other", Confidence: 0.9, Importance: 0.6, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now}
	for _, mem := range []*core.Memory{visible, shared, hidden, otherProject} {
		if err := repo.InsertMemory(ctx, mem); err != nil {
			t.Fatalf("insert memory %s: %v", mem.ID, err)
		}
	}

	summary := &core.Summary{ID: "sum_recall_embed", Kind: "leaf", Scope: core.ScopeProject, ProjectID: "proj_recall", Title: "embedded summary", Body: "summary body", TightDescription: "summary tight", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}
	episode := &core.Episode{ID: "epi_recall_embed", Scope: core.ScopeProject, ProjectID: "proj_recall", Title: "embedded episode", Summary: "episode body", TightDescription: "episode tight", Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}
	entity := &core.Entity{ID: "ent_recall_embed", Type: "service", CanonicalName: "RecallNode", CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertSummary(ctx, summary); err != nil {
		t.Fatalf("insert summary: %v", err)
	}
	if err := repo.InsertEpisode(ctx, episode); err != nil {
		t.Fatalf("insert episode: %v", err)
	}
	if err := repo.InsertEntity(ctx, entity); err != nil {
		t.Fatalf("insert entity: %v", err)
	}

	query := "recall-embedding-query"
	vector := []float32{1, 0, 0}
	provider := staticEmbeddingProvider{model: "recall-internal-model", vectors: map[string][]float32{query: vector}}
	svc.embeddingProvider = provider

	for _, rec := range []struct {
		id   string
		kind string
	}{
		{visible.ID, "memory"},
		{shared.ID, "memory"},
		{hidden.ID, "memory"},
		{otherProject.ID, "memory"},
		{summary.ID, "summary"},
		{episode.ID, "episode"},
	} {
		if err := repo.UpsertEmbedding(ctx, &core.EmbeddingRecord{ObjectID: rec.id, ObjectKind: rec.kind, Model: provider.Model(), Vector: vector, CreatedAt: now}); err != nil {
			t.Fatalf("upsert embedding %s/%s: %v", rec.kind, rec.id, err)
		}
	}

	weights := DefaultScoringWeights()
	sctx := ScoringContext{
		Query:              query,
		QueryEmbedding:     vector,
		QueryEntities:      []string{"recallnode"},
		QueryEntityWeights: map[string]float64{"recallnode": 1.0},
		ProjectID:          "proj_recall",
		SessionID:          "sess_recall_internal",
		RecentRecalls:      map[string]bool{},
		Now:                now,
		Weights:            &weights,
	}
	baseOpts := core.RecallOptions{Limit: 20, AgentID: "agent-a", ProjectID: "proj_recall"}

	factsItems, err := svc.recallFacts(ctx, query, baseOpts, sctx)
	if err != nil {
		t.Fatalf("recallFacts: %v", err)
	}
	if len(factsItems) == 0 {
		t.Fatal("expected recallFacts results")
	}
	foundVisible, foundShared, foundHidden := false, false, false
	for _, item := range factsItems {
		switch item.ID {
		case visible.ID:
			foundVisible = true
		case shared.ID:
			foundShared = true
		case hidden.ID:
			foundHidden = true
		}
	}
	if !foundVisible || !foundShared || foundHidden {
		t.Fatalf("unexpected visibility in facts recall: visible=%v shared=%v hidden=%v items=%#v", foundVisible, foundShared, foundHidden, factsItems)
	}

	projectItems, err := svc.recallProject(ctx, query, baseOpts, sctx)
	if err != nil {
		t.Fatalf("recallProject: %v", err)
	}
	for _, item := range projectItems {
		if item.ID == otherProject.ID {
			t.Fatalf("expected project recall to exclude other project memory, got %#v", projectItems)
		}
	}

	ambientItems, err := svc.recallAmbient(ctx, query, baseOpts, sctx)
	if err != nil {
		t.Fatalf("recallAmbient: %v", err)
	}
	if len(ambientItems) == 0 {
		t.Fatal("expected ambient recall results")
	}

	entityItems, err := svc.recallEntity(ctx, "RecallNode", baseOpts, sctx)
	if err != nil {
		t.Fatalf("recallEntity: %v", err)
	}
	foundEntity := false
	for _, item := range entityItems {
		if item.Kind == "entity" && item.ID == entity.ID {
			foundEntity = true
			break
		}
	}
	if !foundEntity {
		t.Fatalf("expected entity item in recallEntity, got %#v", entityItems)
	}
}

func TestRecall_ListRelatedEntitiesFallbackPaths(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	entity := &core.Entity{ID: "ent_related_source", Type: "service", CanonicalName: "Source", CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEntity(ctx, entity); err != nil {
		t.Fatalf("insert source entity: %v", err)
	}

	if _, err := repo.ExecContext(ctx, `
		INSERT INTO entity_graph_projection (entity_id, related_entity_id, hop_distance, relationship_path, score, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, entity.ID, "ent_missing_target", 1, "uses", 0.9, now.Format(time.RFC3339)); err != nil {
		t.Fatalf("insert synthetic projection row: %v", err)
	}

	related, err := svc.listRelatedEntitiesForRecall(ctx, entity.ID)
	if err != nil {
		t.Fatalf("listRelatedEntitiesForRecall fallback: %v", err)
	}
	if len(related) != 0 {
		t.Fatalf("expected empty related set for dangling projection target, got %#v", related)
	}

	if _, err := svc.listRelatedEntitiesForRecall(ctx, "ent_no_projection"); err != nil {
		t.Fatalf("listRelatedEntitiesForRecall without projections: %v", err)
	}
}

func TestLLMAndHeuristicUtilityBranchHelpers(t *testing.T) {
	if _, err := parseCompressionResults(`[{"index":1,"body":"ok","tight_description":"tight"}]`, []int{1, 2}); err == nil {
		t.Fatal("expected parseCompressionResults to fail when required index is missing")
	}
	parsed, err := parseCompressionResults("```json\n[{\"index\":1,\"body\":\" body \",\"tight_description\":\" tight \"}]\n```", []int{1})
	if err != nil {
		t.Fatalf("parseCompressionResults with fenced json: %v", err)
	}
	if len(parsed) != 1 || parsed[0].Body != "body" || parsed[0].TightDescription != "tight" {
		t.Fatalf("unexpected parsed compression result: %#v", parsed)
	}

	h := NewHeuristicIntelligenceProvider()
	review, err := h.ReviewMemories(context.Background(), []core.MemoryReview{{ID: "m", Body: "b"}})
	if err != nil {
		t.Fatalf("heuristic ReviewMemories: %v", err)
	}
	if review == nil {
		t.Fatal("expected non-nil heuristic review result")
	}
}
