package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func TestEventUpdateClaimAndListFilters(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	events := []*core.Event{
		{
			ID:           "evt_filter_1",
			Kind:         "message_user",
			SourceSystem: "test",
			SessionID:    "sess_filter_1",
			ProjectID:    "proj_filter_1",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      "deploy alpha",
			OccurredAt:   now,
			IngestedAt:   now,
		},
		{
			ID:           "evt_filter_2",
			Kind:         "message_user",
			SourceSystem: "test",
			SessionID:    "sess_filter_1",
			ProjectID:    "proj_filter_1",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      "deploy beta",
			OccurredAt:   now.Add(1 * time.Minute),
			IngestedAt:   now.Add(1 * time.Minute),
		},
		{
			ID:           "evt_filter_3",
			Kind:         "system",
			SourceSystem: "test",
			SessionID:    "sess_filter_2",
			ProjectID:    "proj_filter_2",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      "ops gamma",
			OccurredAt:   now.Add(2 * time.Minute),
			IngestedAt:   now.Add(2 * time.Minute),
		},
	}
	for _, evt := range events {
		if err := repo.InsertEvent(ctx, evt); err != nil {
			t.Fatalf("insert event %s: %v", evt.ID, err)
		}
	}

	reflectedAt := now.Add(10 * time.Minute)
	events[0].Content = "deploy alpha updated"
	events[0].Metadata = map[string]string{"state": "updated"}
	events[0].ReflectedAt = &reflectedAt
	if err := repo.UpdateEvent(ctx, events[0]); err != nil {
		t.Fatalf("update event: %v", err)
	}

	updated, err := repo.GetEvent(ctx, events[0].ID)
	if err != nil {
		t.Fatalf("get updated event: %v", err)
	}
	if updated.Content != "deploy alpha updated" || updated.Metadata["state"] != "updated" || updated.ReflectedAt == nil {
		t.Fatalf("unexpected updated event: %+v", updated)
	}

	count, err := repo.CountUnreflectedEvents(ctx)
	if err != nil {
		t.Fatalf("CountUnreflectedEvents: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 unreflected events, got %d", count)
	}

	claimed, err := repo.ClaimUnreflectedEvents(ctx, 1)
	if err != nil {
		t.Fatalf("ClaimUnreflectedEvents: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != events[1].ID {
		t.Fatalf("unexpected claimed events: %+v", claimed)
	}

	count, err = repo.CountUnreflectedEvents(ctx)
	if err != nil {
		t.Fatalf("CountUnreflectedEvents after claim: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 unreflected event after claim, got %d", count)
	}

	evt2, err := repo.GetEvent(ctx, events[1].ID)
	if err != nil {
		t.Fatalf("get claimed event: %v", err)
	}
	if evt2.ReflectedAt == nil {
		t.Fatal("expected claimed event to be marked reflected")
	}

	filtered, err := repo.ListEvents(ctx, core.ListEventsOptions{
		SessionID: "sess_filter_1",
		ProjectID: "proj_filter_1",
		Kind:      "message_user",
		After:     now.Add(-1 * time.Second).Format(time.RFC3339),
		Before:    now.Add(2 * time.Minute).Format(time.RFC3339),
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListEvents with time filters: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("expected 2 filtered events, got %+v", filtered)
	}

	seqLimited, err := repo.ListEvents(ctx, core.ListEventsOptions{BeforeSequenceID: evt2.SequenceID, Limit: 10})
	if err != nil {
		t.Fatalf("ListEvents with BeforeSequenceID: %v", err)
	}
	if len(seqLimited) != 2 || seqLimited[0].ID != events[0].ID || seqLimited[1].ID != events[1].ID {
		t.Fatalf("unexpected sequence-limited events: %+v", seqLimited)
	}

	unreflected, err := repo.ListEvents(ctx, core.ListEventsOptions{AfterSequenceID: updated.SequenceID, UnreflectedOnly: true, Limit: 10})
	if err != nil {
		t.Fatalf("ListEvents with AfterSequenceID+UnreflectedOnly: %v", err)
	}
	if len(unreflected) != 1 || unreflected[0].ID != events[2].ID {
		t.Fatalf("unexpected unreflected events: %+v", unreflected)
	}

	results, err := repo.SearchEvents(ctx, "gamma", 10)
	if err != nil {
		t.Fatalf("SearchEvents: %v", err)
	}
	if len(results) != 1 || results[0].ID != events[2].ID {
		t.Fatalf("unexpected search results: %+v", results)
	}

	emptyResults, err := repo.SearchEvents(ctx, "***", 10)
	if err != nil {
		t.Fatalf("SearchEvents special characters: %v", err)
	}
	if emptyResults != nil {
		t.Fatalf("expected nil search results for sanitized empty query, got %+v", emptyResults)
	}
}

func TestListParentedSummaryIDs(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	parent := &core.Summary{ID: "sum_parented_parent", Kind: "session", Scope: core.ScopeProject, ProjectID: "proj_parented", Body: "parent", TightDescription: "parent", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}
	child := &core.Summary{ID: "sum_parented_child", Kind: "leaf", Scope: core.ScopeProject, ProjectID: "proj_parented", Body: "child", TightDescription: "child", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	event := &core.Event{ID: "evt_parented_child", Kind: "message_user", SourceSystem: "test", PrivacyLevel: core.PrivacyPrivate, Content: "event child", OccurredAt: now, IngestedAt: now}

	if err := repo.InsertSummary(ctx, parent); err != nil {
		t.Fatalf("insert parent summary: %v", err)
	}
	if err := repo.InsertSummary(ctx, child); err != nil {
		t.Fatalf("insert child summary: %v", err)
	}
	if err := repo.InsertEvent(ctx, event); err != nil {
		t.Fatalf("insert child event: %v", err)
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: parent.ID, ChildKind: "summary", ChildID: child.ID, EdgeOrder: 1}); err != nil {
		t.Fatalf("insert summary child edge: %v", err)
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: parent.ID, ChildKind: "event", ChildID: event.ID, EdgeOrder: 2}); err != nil {
		t.Fatalf("insert event child edge: %v", err)
	}

	ids, err := repo.ListParentedSummaryIDs(ctx)
	if err != nil {
		t.Fatalf("ListParentedSummaryIDs: %v", err)
	}
	if len(ids) != 1 || !ids[child.ID] || ids[event.ID] {
		t.Fatalf("unexpected parented summary ids: %+v", ids)
	}
}

func TestMemoryBatchAndFuzzyQueries(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	m1 := &core.Memory{ID: "mem_batch_1", Type: core.MemoryTypeFact, Scope: core.ScopeProject, ProjectID: "proj_batch", AgentID: "agent_a", Body: "Alpha deployment note", TightDescription: "Alpha deployment", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, SourceEventIDs: []string{"evt_src_1"}, CreatedAt: now, UpdatedAt: now}
	m2 := &core.Memory{ID: "mem_batch_2", Type: core.MemoryTypeFact, Scope: core.ScopeProject, ProjectID: "proj_batch", AgentID: "other_agent", Body: "Alpha shared rollout note", TightDescription: "Alpha shared rollout", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyShared, Status: core.MemoryStatusActive, SourceEventIDs: []string{"evt_src_2"}, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	m3 := &core.Memory{ID: "mem_batch_3", Type: core.MemoryTypeFact, Scope: core.ScopeProject, ProjectID: "proj_batch", AgentID: "agent_a", Body: "Alpha archived note", TightDescription: "Alpha archived", Confidence: 0.7, Importance: 0.4, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusArchived, SourceEventIDs: []string{"evt_src_1"}, CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second)}
	for _, mem := range []*core.Memory{m1, m2, m3} {
		if err := repo.InsertMemory(ctx, mem); err != nil {
			t.Fatalf("insert memory %s: %v", mem.ID, err)
		}
	}

	memories, err := repo.GetMemoriesByIDs(ctx, []string{m1.ID, m2.ID, "mem_missing"})
	if err != nil {
		t.Fatalf("GetMemoriesByIDs: %v", err)
	}
	if len(memories) != 2 || memories[m1.ID] == nil || memories[m2.ID] == nil {
		t.Fatalf("unexpected batch memories: %+v", memories)
	}

	emptyBatch, err := repo.GetMemoriesByIDs(ctx, nil)
	if err != nil {
		t.Fatalf("GetMemoriesByIDs empty: %v", err)
	}
	if len(emptyBatch) != 0 {
		t.Fatalf("expected empty map for empty ids, got %+v", emptyBatch)
	}

	results, err := repo.SearchMemoriesFuzzy(ctx, "Alpha", core.ListMemoriesOptions{
		Type:      core.MemoryTypeFact,
		Scope:     core.ScopeProject,
		ProjectID: "proj_batch",
		AgentID:   "agent_a",
		Status:    core.MemoryStatusActive,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("SearchMemoriesFuzzy: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 fuzzy-search results, got %+v", results)
	}

	emptyResults, err := repo.SearchMemoriesFuzzy(ctx, "***", core.ListMemoriesOptions{Limit: 10})
	if err != nil {
		t.Fatalf("SearchMemoriesFuzzy empty query: %v", err)
	}
	if emptyResults != nil {
		t.Fatalf("expected nil fuzzy-search results for empty sanitized query, got %+v", emptyResults)
	}

	bySource, err := repo.ListMemoriesBySourceEventIDs(ctx, []string{"evt_src_1", "evt_missing"})
	if err != nil {
		t.Fatalf("ListMemoriesBySourceEventIDs: %v", err)
	}
	if len(bySource) != 1 || bySource[0].ID != m1.ID {
		t.Fatalf("unexpected memories by source event ids: %+v", bySource)
	}

	emptySource, err := repo.ListMemoriesBySourceEventIDs(ctx, nil)
	if err != nil {
		t.Fatalf("ListMemoriesBySourceEventIDs empty: %v", err)
	}
	if len(emptySource) != 0 {
		t.Fatalf("expected empty slice for empty source event ids, got %+v", emptySource)
	}
}

func TestEntityBatchLinksAndCounts(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	ent1 := &core.Entity{ID: "ent_batch_1", Type: "person", CanonicalName: "Alice", Aliases: []string{"ali"}, Description: "first", CreatedAt: now, UpdatedAt: now}
	ent2 := &core.Entity{ID: "ent_batch_2", Type: "service", CanonicalName: "AMM", CreatedAt: now, UpdatedAt: now}
	ent3 := &core.Entity{ID: "ent_batch_3", Type: "team", CanonicalName: "Platform", CreatedAt: now, UpdatedAt: now}
	for _, ent := range []*core.Entity{ent1, ent2, ent3} {
		if err := repo.InsertEntity(ctx, ent); err != nil {
			t.Fatalf("insert entity %s: %v", ent.ID, err)
		}
	}

	ent1.Description = "updated"
	ent1.Aliases = []string{"alice", "a"}
	ent1.Metadata = map[string]string{"owner": "platform"}
	ent1.UpdatedAt = now.Add(time.Minute)
	if err := repo.UpdateEntity(ctx, ent1); err != nil {
		t.Fatalf("UpdateEntity: %v", err)
	}

	updated, err := repo.GetEntity(ctx, ent1.ID)
	if err != nil {
		t.Fatalf("get updated entity: %v", err)
	}
	if updated.Description != "updated" || len(updated.Aliases) != 2 || updated.Metadata["owner"] != "platform" {
		t.Fatalf("unexpected updated entity: %+v", updated)
	}

	entities, err := repo.GetEntitiesByIDs(ctx, []string{ent1.ID, ent2.ID})
	if err != nil {
		t.Fatalf("GetEntitiesByIDs: %v", err)
	}
	if len(entities) != 2 {
		t.Fatalf("expected 2 entities, got %+v", entities)
	}

	emptyEntities, err := repo.GetEntitiesByIDs(ctx, nil)
	if err != nil {
		t.Fatalf("GetEntitiesByIDs empty: %v", err)
	}
	if len(emptyEntities) != 0 {
		t.Fatalf("expected empty entity slice, got %+v", emptyEntities)
	}

	mem1 := &core.Memory{ID: "mem_entity_batch_1", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "Alice owns AMM", TightDescription: "Alice owns AMM", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now}
	mem2 := &core.Memory{ID: "mem_entity_batch_2", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "AMM belongs to platform", TightDescription: "AMM belongs to platform", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	for _, mem := range []*core.Memory{mem1, mem2} {
		if err := repo.InsertMemory(ctx, mem); err != nil {
			t.Fatalf("insert memory %s: %v", mem.ID, err)
		}
	}

	links := []core.MemoryEntityLink{
		{MemoryID: mem1.ID, EntityID: ent1.ID, Role: "owner"},
		{MemoryID: mem1.ID, EntityID: ent2.ID, Role: "subject"},
		{MemoryID: mem1.ID, EntityID: "", Role: "skip"},
		{MemoryID: mem2.ID, EntityID: ent1.ID, Role: "owner"},
		{MemoryID: mem2.ID, EntityID: ent1.ID, Role: "owner"},
	}
	if err := repo.LinkMemoryEntitiesBatch(ctx, links); err != nil {
		t.Fatalf("LinkMemoryEntitiesBatch: %v", err)
	}
	if err := repo.LinkMemoryEntitiesBatch(ctx, nil); err != nil {
		t.Fatalf("LinkMemoryEntitiesBatch empty: %v", err)
	}

	batch, err := repo.GetMemoryEntitiesBatch(ctx, []string{mem1.ID, mem2.ID, "mem_entity_batch_missing"})
	if err != nil {
		t.Fatalf("GetMemoryEntitiesBatch: %v", err)
	}
	if len(batch[mem1.ID]) != 2 || len(batch[mem2.ID]) != 1 || batch["mem_entity_batch_missing"] != nil {
		t.Fatalf("unexpected memory-entity batch result: %+v", batch)
	}

	emptyLinks, err := repo.GetMemoryEntitiesBatch(ctx, nil)
	if err != nil {
		t.Fatalf("GetMemoryEntitiesBatch empty: %v", err)
	}
	if len(emptyLinks) != 0 {
		t.Fatalf("expected empty memory-entity batch result, got %+v", emptyLinks)
	}

	counts, err := repo.CountMemoryEntityLinksBatch(ctx, []string{ent1.ID, ent2.ID, ent3.ID})
	if err != nil {
		t.Fatalf("CountMemoryEntityLinksBatch: %v", err)
	}
	if counts[ent1.ID] != 2 || counts[ent2.ID] != 1 || counts[ent3.ID] != 0 {
		t.Fatalf("unexpected memory-entity counts: %+v", counts)
	}

	emptyCounts, err := repo.CountMemoryEntityLinksBatch(ctx, nil)
	if err != nil {
		t.Fatalf("CountMemoryEntityLinksBatch empty: %v", err)
	}
	if len(emptyCounts) != 0 {
		t.Fatalf("expected empty counts map, got %+v", emptyCounts)
	}
}

func TestRelationshipBatchQueries(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	entities := []*core.Entity{
		{ID: "ent_rel_batch_a", Type: "service", CanonicalName: "A", CreatedAt: now, UpdatedAt: now},
		{ID: "ent_rel_batch_b", Type: "service", CanonicalName: "B", CreatedAt: now, UpdatedAt: now},
		{ID: "ent_rel_batch_c", Type: "service", CanonicalName: "C", CreatedAt: now, UpdatedAt: now},
	}
	for _, ent := range entities {
		if err := repo.InsertEntity(ctx, ent); err != nil {
			t.Fatalf("insert entity %s: %v", ent.ID, err)
		}
	}

	rels := []*core.Relationship{
		{ID: "rel_batch_ab", FromEntityID: entities[0].ID, ToEntityID: entities[1].ID, RelationshipType: "uses", Metadata: map[string]string{"kind": "valid"}, CreatedAt: now, UpdatedAt: now},
		nil,
		{ID: "rel_batch_bc", FromEntityID: entities[1].ID, ToEntityID: entities[2].ID, RelationshipType: "depends-on", Metadata: map[string]string{"kind": "valid"}, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)},
		{ID: "rel_batch_skip", FromEntityID: entities[0].ID, ToEntityID: entities[2].ID, RelationshipType: "", CreatedAt: now, UpdatedAt: now},
	}
	if err := repo.InsertRelationshipsBatch(ctx, rels); err != nil {
		t.Fatalf("InsertRelationshipsBatch: %v", err)
	}
	if err := repo.InsertRelationshipsBatch(ctx, nil); err != nil {
		t.Fatalf("InsertRelationshipsBatch empty: %v", err)
	}

	listed, err := repo.ListRelationshipsByEntityIDs(ctx, []string{"", entities[0].ID, entities[0].ID, entities[2].ID})
	if err != nil {
		t.Fatalf("ListRelationshipsByEntityIDs: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected 2 relationships, got %+v", listed)
	}

	none, err := repo.ListRelationshipsByEntityIDs(ctx, []string{"   "})
	if err != nil {
		t.Fatalf("ListRelationshipsByEntityIDs blanks: %v", err)
	}
	if none != nil {
		t.Fatalf("expected nil for blank-only entity ids, got %+v", none)
	}

	empty, err := repo.ListRelationshipsByEntityIDs(ctx, nil)
	if err != nil {
		t.Fatalf("ListRelationshipsByEntityIDs empty: %v", err)
	}
	if empty != nil {
		t.Fatalf("expected nil for empty entity ids, got %+v", empty)
	}
}

func TestRecallFeedbackAndEmbeddingBatchQueries(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	m1 := &core.Memory{ID: "mem_feedback_1", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "feedback one", TightDescription: "feedback one", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now}
	m2 := &core.Memory{ID: "mem_feedback_2", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "feedback two", TightDescription: "feedback two", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	for _, mem := range []*core.Memory{m1, m2} {
		if err := repo.InsertMemory(ctx, mem); err != nil {
			t.Fatalf("insert memory %s: %v", mem.ID, err)
		}
	}

	if err := repo.RecordRecallBatch(ctx, "sess_feedback", []core.RecallRecord{{ItemID: m1.ID, ItemKind: "memory"}, {ItemID: m1.ID, ItemKind: "memory"}, {ItemID: "sum_feedback", ItemKind: "summary"}}); err != nil {
		t.Fatalf("RecordRecallBatch: %v", err)
	}
	if err := repo.RecordRecallBatch(ctx, "sess_feedback", nil); err != nil {
		t.Fatalf("RecordRecallBatch empty: %v", err)
	}

	stats, err := repo.ListMemoryAccessStats(ctx, time.Now().UTC().Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("ListMemoryAccessStats: %v", err)
	}
	if len(stats) != 1 || stats[0].MemoryID != m1.ID || stats[0].AccessCount != 2 {
		t.Fatalf("unexpected memory access stats: %+v", stats)
	}

	if err := repo.InsertRelevanceFeedback(ctx, "sess_feedback_a", m1.ID, "memory", "expanded"); err != nil {
		t.Fatalf("InsertRelevanceFeedback first expanded: %v", err)
	}
	if err := repo.InsertRelevanceFeedback(ctx, "sess_feedback_b", m1.ID, "memory", "expanded"); err != nil {
		t.Fatalf("InsertRelevanceFeedback second expanded: %v", err)
	}
	if err := repo.InsertRelevanceFeedback(ctx, "sess_feedback_c", m1.ID, "memory", "dismissed"); err != nil {
		t.Fatalf("InsertRelevanceFeedback dismissed: %v", err)
	}
	if err := repo.InsertRelevanceFeedback(ctx, "sess_feedback_d", m2.ID, "summary", "expanded"); err != nil {
		t.Fatalf("InsertRelevanceFeedback summary: %v", err)
	}

	feedback, err := repo.ListRelevanceFeedback(ctx, m1.ID)
	if err != nil {
		t.Fatalf("ListRelevanceFeedback: %v", err)
	}
	if len(feedback) != 3 {
		t.Fatalf("expected 3 feedback entries for memory, got %+v", feedback)
	}

	expandedCounts, err := repo.CountExpandedFeedbackBatch(ctx, []string{m1.ID, m2.ID})
	if err != nil {
		t.Fatalf("CountExpandedFeedbackBatch: %v", err)
	}
	if expandedCounts[m1.ID] != 2 || expandedCounts[m2.ID] != 0 {
		t.Fatalf("unexpected expanded feedback counts: %+v", expandedCounts)
	}

	emptyExpandedCounts, err := repo.CountExpandedFeedbackBatch(ctx, nil)
	if err != nil {
		t.Fatalf("CountExpandedFeedbackBatch empty: %v", err)
	}
	if len(emptyExpandedCounts) != 0 {
		t.Fatalf("expected empty expanded feedback map, got %+v", emptyExpandedCounts)
	}

	records := []*core.EmbeddingRecord{
		{ObjectID: m1.ID, ObjectKind: "memory", Model: "batch-model-v1", Vector: []float32{0.1, 0.2}, CreatedAt: now},
		{ObjectID: m2.ID, ObjectKind: "memory", Model: "batch-model-v1", Vector: []float32{0.3, 0.4}, CreatedAt: now.Add(time.Second)},
		{ObjectID: m1.ID, ObjectKind: "memory", Model: "batch-model-v2", Vector: []float32{0.9}, CreatedAt: now.Add(2 * time.Second)},
	}
	for _, rec := range records {
		if err := repo.UpsertEmbedding(ctx, rec); err != nil {
			t.Fatalf("upsert embedding %+v: %v", rec, err)
		}
	}

	embeddings, err := repo.GetEmbeddingsBatch(ctx, []string{m1.ID, m2.ID, "missing"}, "memory", "batch-model-v1")
	if err != nil {
		t.Fatalf("GetEmbeddingsBatch: %v", err)
	}
	if len(embeddings) != 2 || embeddings[m1.ID].Model != "batch-model-v1" || embeddings[m2.ID].Model != "batch-model-v1" {
		t.Fatalf("unexpected embedding batch: %+v", embeddings)
	}

	emptyEmbeddings, err := repo.GetEmbeddingsBatch(ctx, nil, "memory", "batch-model-v1")
	if err != nil {
		t.Fatalf("GetEmbeddingsBatch empty: %v", err)
	}
	if len(emptyEmbeddings) != 0 {
		t.Fatalf("expected empty embedding batch, got %+v", emptyEmbeddings)
	}

	listed, err := repo.ListEmbeddingsByKind(ctx, "memory", "batch-model-v1", 10)
	if err != nil {
		t.Fatalf("ListEmbeddingsByKind: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected 2 listed embeddings, got %+v", listed)
	}
}

func TestListUnembeddedObjects(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	memoryKeep := &core.Memory{ID: "mem_unembedded_keep", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "keep", TightDescription: "keep", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now, UpdatedAt: now}
	memoryEmbedded := &core.Memory{ID: "mem_unembedded_embedded", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "embedded", TightDescription: "embedded", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusActive, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	memoryArchived := &core.Memory{ID: "mem_unembedded_archived", Type: core.MemoryTypeFact, Scope: core.ScopeGlobal, Body: "archived", TightDescription: "archived", Confidence: 0.8, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, Status: core.MemoryStatusArchived, CreatedAt: now.Add(2 * time.Second), UpdatedAt: now.Add(2 * time.Second)}
	for _, mem := range []*core.Memory{memoryKeep, memoryEmbedded, memoryArchived} {
		if err := repo.InsertMemory(ctx, mem); err != nil {
			t.Fatalf("insert memory %s: %v", mem.ID, err)
		}
	}

	summaryKeep := &core.Summary{ID: "sum_unembedded_keep", Kind: "leaf", Scope: core.ScopeGlobal, Body: "keep summary", TightDescription: "keep summary", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}
	summaryEmbedded := &core.Summary{ID: "sum_unembedded_embedded", Kind: "leaf", Scope: core.ScopeGlobal, Body: "embedded summary", TightDescription: "embedded summary", PrivacyLevel: core.PrivacyPrivate, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	for _, summary := range []*core.Summary{summaryKeep, summaryEmbedded} {
		if err := repo.InsertSummary(ctx, summary); err != nil {
			t.Fatalf("insert summary %s: %v", summary.ID, err)
		}
	}

	episodeKeep := &core.Episode{ID: "epi_unembedded_keep", Title: "keep episode", Summary: "keep episode", TightDescription: "keep episode", Scope: core.ScopeGlobal, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, CreatedAt: now, UpdatedAt: now}
	episodeEmbedded := &core.Episode{ID: "epi_unembedded_embedded", Title: "embedded episode", Summary: "embedded episode", TightDescription: "embedded episode", Scope: core.ScopeGlobal, Importance: 0.5, PrivacyLevel: core.PrivacyPrivate, CreatedAt: now.Add(time.Second), UpdatedAt: now.Add(time.Second)}
	for _, episode := range []*core.Episode{episodeKeep, episodeEmbedded} {
		if err := repo.InsertEpisode(ctx, episode); err != nil {
			t.Fatalf("insert episode %s: %v", episode.ID, err)
		}
	}

	for _, rec := range []*core.EmbeddingRecord{
		{ObjectID: memoryEmbedded.ID, ObjectKind: "memory", Model: "unembedded-model", Vector: []float32{0.1}, CreatedAt: now},
		{ObjectID: summaryEmbedded.ID, ObjectKind: "summary", Model: "unembedded-model", Vector: []float32{0.2}, CreatedAt: now},
		{ObjectID: episodeEmbedded.ID, ObjectKind: "episode", Model: "unembedded-model", Vector: []float32{0.3}, CreatedAt: now},
	} {
		if err := repo.UpsertEmbedding(ctx, rec); err != nil {
			t.Fatalf("upsert embedding %+v: %v", rec, err)
		}
	}

	unembeddedMemories, err := repo.ListUnembeddedMemories(ctx, "unembedded-model", 10)
	if err != nil {
		t.Fatalf("ListUnembeddedMemories: %v", err)
	}
	if len(unembeddedMemories) != 1 || unembeddedMemories[0].ID != memoryKeep.ID {
		t.Fatalf("unexpected unembedded memories: %+v", unembeddedMemories)
	}

	unembeddedSummaries, err := repo.ListUnembeddedSummaries(ctx, "unembedded-model", 10)
	if err != nil {
		t.Fatalf("ListUnembeddedSummaries: %v", err)
	}
	if len(unembeddedSummaries) != 1 || unembeddedSummaries[0].ID != summaryKeep.ID {
		t.Fatalf("unexpected unembedded summaries: %+v", unembeddedSummaries)
	}

	unembeddedEpisodes, err := repo.ListUnembeddedEpisodes(ctx, "unembedded-model", 10)
	if err != nil {
		t.Fatalf("ListUnembeddedEpisodes: %v", err)
	}
	if len(unembeddedEpisodes) != 1 || unembeddedEpisodes[0].ID != episodeKeep.ID {
		t.Fatalf("unexpected unembedded episodes: %+v", unembeddedEpisodes)
	}
}
