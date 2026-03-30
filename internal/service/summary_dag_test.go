package service

import (
	"context"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func testSummary(t *testing.T, id, kind string, depth int, condensedKind string) *core.Summary {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	return &core.Summary{
		ID:               id,
		Kind:             kind,
		Depth:            depth,
		CondensedKind:    condensedKind,
		Scope:            core.ScopeSession,
		SessionID:        "sess_summary_dag",
		Body:             id + " body",
		TightDescription: id + " tight",
		PrivacyLevel:     core.PrivacyPrivate,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

func testEvent(t *testing.T, id string) *core.Event {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	return &core.Event{
		ID:           id,
		Kind:         "message",
		SourceSystem: "test",
		SessionID:    "sess_summary_dag",
		PrivacyLevel: core.PrivacyPrivate,
		Content:      id + " content",
		OccurredAt:   now,
		IngestedAt:   now,
	}
}

func TestSummaryEdge_InsertAndGetChildren(t *testing.T) {
	_, repo := testServiceAndRepo(t)
	ctx := context.Background()

	parent := testSummary(t, "sum_parent_order", "session", 2, "session")
	childA := testSummary(t, "sum_child_a", "topic", 1, "topic")
	childB := testSummary(t, "sum_child_b", "topic", 1, "topic")
	for _, s := range []*core.Summary{parent, childA, childB} {
		if err := repo.InsertSummary(ctx, s); err != nil {
			t.Fatalf("insert summary %s: %v", s.ID, err)
		}
	}

	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: parent.ID, ChildKind: "summary", ChildID: childB.ID, EdgeOrder: 1}); err != nil {
		t.Fatalf("insert edge childB: %v", err)
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: parent.ID, ChildKind: "summary", ChildID: childA.ID, EdgeOrder: 0}); err != nil {
		t.Fatalf("insert edge childA: %v", err)
	}

	edges, err := repo.GetSummaryChildren(ctx, parent.ID)
	if err != nil {
		t.Fatalf("get summary children: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("expected 2 child edges, got %d", len(edges))
	}
	if edges[0].ChildID != childA.ID || edges[0].EdgeOrder != 0 {
		t.Fatalf("expected first child edge to be %s/order0, got %+v", childA.ID, edges[0])
	}
	if edges[1].ChildID != childB.ID || edges[1].EdgeOrder != 1 {
		t.Fatalf("expected second child edge to be %s/order1, got %+v", childB.ID, edges[1])
	}
}

func TestSummaryEdge_ChildKindEvent(t *testing.T) {
	_, repo := testServiceAndRepo(t)
	ctx := context.Background()

	parent := testSummary(t, "sum_parent_events", "leaf", 0, "")
	if err := repo.InsertSummary(ctx, parent); err != nil {
		t.Fatalf("insert summary: %v", err)
	}

	evt1 := testEvent(t, "evt_child_1")
	evt2 := testEvent(t, "evt_child_2")
	for _, evt := range []*core.Event{evt1, evt2} {
		if err := repo.InsertEvent(ctx, evt); err != nil {
			t.Fatalf("insert event %s: %v", evt.ID, err)
		}
	}

	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: parent.ID, ChildKind: "event", ChildID: evt1.ID, EdgeOrder: 0}); err != nil {
		t.Fatalf("insert event edge 1: %v", err)
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: parent.ID, ChildKind: "event", ChildID: evt2.ID, EdgeOrder: 1}); err != nil {
		t.Fatalf("insert event edge 2: %v", err)
	}

	edges, err := repo.GetSummaryChildren(ctx, parent.ID)
	if err != nil {
		t.Fatalf("get summary children: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("expected 2 event child edges, got %d", len(edges))
	}
	if edges[0].ChildKind != "event" || edges[0].ChildID != evt1.ID {
		t.Fatalf("expected first event edge to %s, got %+v", evt1.ID, edges[0])
	}
	if edges[1].ChildKind != "event" || edges[1].ChildID != evt2.ID {
		t.Fatalf("expected second event edge to %s, got %+v", evt2.ID, edges[1])
	}
}

func TestSummaryEdge_ListParentedSummaryIDs(t *testing.T) {
	_, repo := testServiceAndRepo(t)
	ctx := context.Background()

	parent := testSummary(t, "sum_parent_ids", "session", 2, "session")
	child1 := testSummary(t, "sum_parented_1", "topic", 1, "topic")
	child2 := testSummary(t, "sum_parented_2", "leaf", 0, "")
	orphan := testSummary(t, "sum_orphan", "leaf", 0, "")
	for _, s := range []*core.Summary{parent, child1, child2, orphan} {
		if err := repo.InsertSummary(ctx, s); err != nil {
			t.Fatalf("insert summary %s: %v", s.ID, err)
		}
	}
	evt := testEvent(t, "evt_non_summary_child")
	if err := repo.InsertEvent(ctx, evt); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: parent.ID, ChildKind: "summary", ChildID: child1.ID, EdgeOrder: 0}); err != nil {
		t.Fatalf("insert child1 edge: %v", err)
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: child1.ID, ChildKind: "summary", ChildID: child2.ID, EdgeOrder: 0}); err != nil {
		t.Fatalf("insert child2 edge: %v", err)
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: parent.ID, ChildKind: "event", ChildID: evt.ID, EdgeOrder: 1}); err != nil {
		t.Fatalf("insert event edge: %v", err)
	}

	ids, err := repo.ListParentedSummaryIDs(ctx)
	if err != nil {
		t.Fatalf("list parented summary ids: %v", err)
	}
	if !ids[child1.ID] || !ids[child2.ID] {
		t.Fatalf("expected parented child summaries in map, got %+v", ids)
	}
	if ids[parent.ID] {
		t.Fatalf("did not expect parent summary id in parented set")
	}
	if ids[orphan.ID] {
		t.Fatalf("did not expect orphan summary id in parented set")
	}
	if ids[evt.ID] {
		t.Fatalf("did not expect event id in parented summary set")
	}
}

func TestSummaryEdge_MultiLevelDAG(t *testing.T) {
	_, repo := testServiceAndRepo(t)
	ctx := context.Background()

	sessionSummary := testSummary(t, "sum_session_lvl", "session", 2, "session")
	topicSummary := testSummary(t, "sum_topic_lvl", "topic", 1, "topic")
	leafSummary := testSummary(t, "sum_leaf_lvl", "leaf", 0, "")
	for _, s := range []*core.Summary{sessionSummary, topicSummary, leafSummary} {
		if err := repo.InsertSummary(ctx, s); err != nil {
			t.Fatalf("insert summary %s: %v", s.ID, err)
		}
	}

	evt1 := testEvent(t, "evt_leaf_1")
	evt2 := testEvent(t, "evt_leaf_2")
	for _, evt := range []*core.Event{evt1, evt2} {
		if err := repo.InsertEvent(ctx, evt); err != nil {
			t.Fatalf("insert event %s: %v", evt.ID, err)
		}
	}

	edgesToInsert := []*core.SummaryEdge{
		{ParentSummaryID: sessionSummary.ID, ChildKind: "summary", ChildID: topicSummary.ID, EdgeOrder: 0},
		{ParentSummaryID: topicSummary.ID, ChildKind: "summary", ChildID: leafSummary.ID, EdgeOrder: 0},
		{ParentSummaryID: leafSummary.ID, ChildKind: "event", ChildID: evt1.ID, EdgeOrder: 0},
		{ParentSummaryID: leafSummary.ID, ChildKind: "event", ChildID: evt2.ID, EdgeOrder: 1},
	}
	for _, edge := range edgesToInsert {
		if err := repo.InsertSummaryEdge(ctx, edge); err != nil {
			t.Fatalf("insert summary edge %+v: %v", edge, err)
		}
	}

	sessionChildren, err := repo.GetSummaryChildren(ctx, sessionSummary.ID)
	if err != nil {
		t.Fatalf("get session summary children: %v", err)
	}
	if len(sessionChildren) != 1 || sessionChildren[0].ChildKind != "summary" || sessionChildren[0].ChildID != topicSummary.ID {
		t.Fatalf("expected session->topic edge, got %+v", sessionChildren)
	}

	topicChildren, err := repo.GetSummaryChildren(ctx, topicSummary.ID)
	if err != nil {
		t.Fatalf("get topic summary children: %v", err)
	}
	if len(topicChildren) != 1 || topicChildren[0].ChildKind != "summary" || topicChildren[0].ChildID != leafSummary.ID {
		t.Fatalf("expected topic->leaf edge, got %+v", topicChildren)
	}

	leafChildren, err := repo.GetSummaryChildren(ctx, leafSummary.ID)
	if err != nil {
		t.Fatalf("get leaf summary children: %v", err)
	}
	if len(leafChildren) != 2 {
		t.Fatalf("expected leaf->event edges, got %+v", leafChildren)
	}
	if leafChildren[0].ChildKind != "event" || leafChildren[0].ChildID != evt1.ID {
		t.Fatalf("expected first leaf event edge to %s, got %+v", evt1.ID, leafChildren[0])
	}
	if leafChildren[1].ChildKind != "event" || leafChildren[1].ChildID != evt2.ID {
		t.Fatalf("expected second leaf event edge to %s, got %+v", evt2.ID, leafChildren[1])
	}
}

func TestExpandSummary_ShowsChildren(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	parent := testSummary(t, "sum_expand_parent_edges", "topic", 1, "topic")
	child := testSummary(t, "sum_expand_child_edges", "leaf", 0, "")
	if err := repo.InsertSummary(ctx, parent); err != nil {
		t.Fatalf("insert parent summary: %v", err)
	}
	if err := repo.InsertSummary(ctx, child); err != nil {
		t.Fatalf("insert child summary: %v", err)
	}

	evt := testEvent(t, "evt_expand_child")
	if err := repo.InsertEvent(ctx, evt); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: parent.ID, ChildKind: "summary", ChildID: child.ID, EdgeOrder: 0}); err != nil {
		t.Fatalf("insert summary child edge: %v", err)
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: parent.ID, ChildKind: "event", ChildID: evt.ID, EdgeOrder: 1}); err != nil {
		t.Fatalf("insert event child edge: %v", err)
	}

	expanded, err := svc.Expand(ctx, parent.ID, "summary", core.ExpandOptions{})
	if err != nil {
		t.Fatalf("expand summary: %v", err)
	}
	if expanded.Summary == nil || expanded.Summary.ID != parent.ID {
		t.Fatalf("expected expanded summary to be parent, got %+v", expanded.Summary)
	}
	if len(expanded.Children) != 1 || expanded.Children[0].ID != child.ID {
		t.Fatalf("expected expanded child summary %s, got %+v", child.ID, expanded.Children)
	}
	if len(expanded.Events) != 1 || expanded.Events[0].ID != evt.ID {
		t.Fatalf("expected expanded child event %s, got %+v", evt.ID, expanded.Events)
	}
}
