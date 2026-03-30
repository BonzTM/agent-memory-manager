package service

import (
	"context"
	"testing"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func insertGrepEvent(t *testing.T, repo core.Repository, id, sessionID, projectID, content string) *core.Event {
	t.Helper()
	evt := testEvent(t, id)
	evt.SessionID = sessionID
	evt.ProjectID = projectID
	evt.Content = content
	if err := repo.InsertEvent(context.Background(), evt); err != nil {
		t.Fatalf("insert event %s: %v", id, err)
	}
	return evt
}

func insertGrepSummary(t *testing.T, repo core.Repository, id, kind string, depth int) *core.Summary {
	t.Helper()
	sum := testSummary(t, id, kind, depth, kind)
	if kind == "leaf" {
		sum.CondensedKind = ""
	}
	if err := repo.InsertSummary(context.Background(), sum); err != nil {
		t.Fatalf("insert summary %s: %v", id, err)
	}
	return sum
}

func TestGrep_NoMatches(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	insertGrepEvent(t, repo, "evt_grep_none_1", "sess_grep_none", "prj_grep_none", "alpha content")

	result, err := svc.Grep(ctx, "needle_absent", core.GrepOptions{})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if result.TotalHits != 0 {
		t.Fatalf("expected TotalHits=0, got %d", result.TotalHits)
	}
	if len(result.Groups) != 0 {
		t.Fatalf("expected 0 groups, got %d", len(result.Groups))
	}
}

func TestGrep_MatchesWithoutSummaries(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	insertGrepEvent(t, repo, "evt_grep_orphan_1", "sess_grep_orphan", "prj_grep_orphan", "needle orphan one")
	insertGrepEvent(t, repo, "evt_grep_orphan_2", "sess_grep_orphan", "prj_grep_orphan", "needle orphan two")

	result, err := svc.Grep(ctx, "needle", core.GrepOptions{SessionID: "sess_grep_orphan"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if result.TotalHits != 2 {
		t.Fatalf("expected TotalHits=2, got %d", result.TotalHits)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result.Groups))
	}
	if result.Groups[0].Summary != nil {
		t.Fatalf("expected nil summary group")
	}
	if len(result.Groups[0].Matches) != 2 {
		t.Fatalf("expected 2 matches in ungrouped bucket, got %d", len(result.Groups[0].Matches))
	}
}

func TestGrep_MatchesGroupedBySummary(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	leafA := insertGrepSummary(t, repo, "sum_grep_leaf_a", "leaf", 0)
	leafB := insertGrepSummary(t, repo, "sum_grep_leaf_b", "leaf", 0)

	evtA1 := insertGrepEvent(t, repo, "evt_grep_a1", "sess_grep_group", "prj_grep_group", "needle apple")
	evtA2 := insertGrepEvent(t, repo, "evt_grep_a2", "sess_grep_group", "prj_grep_group", "needle apricot")
	evtB1 := insertGrepEvent(t, repo, "evt_grep_b1", "sess_grep_group", "prj_grep_group", "needle banana")

	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: leafA.ID, ChildKind: "event", ChildID: evtA1.ID, EdgeOrder: 0}); err != nil {
		t.Fatalf("insert edge: %v", err)
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: leafA.ID, ChildKind: "event", ChildID: evtA2.ID, EdgeOrder: 1}); err != nil {
		t.Fatalf("insert edge: %v", err)
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: leafB.ID, ChildKind: "event", ChildID: evtB1.ID, EdgeOrder: 0}); err != nil {
		t.Fatalf("insert edge: %v", err)
	}

	result, err := svc.Grep(ctx, "needle", core.GrepOptions{SessionID: "sess_grep_group"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if result.TotalHits != 3 {
		t.Fatalf("expected TotalHits=3, got %d", result.TotalHits)
	}
	if len(result.Groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(result.Groups))
	}

	foundA := false
	foundB := false
	for _, g := range result.Groups {
		switch g.SummaryID {
		case leafA.ID:
			foundA = true
			if len(g.Matches) != 2 {
				t.Fatalf("expected 2 matches in group A, got %d", len(g.Matches))
			}
		case leafB.ID:
			foundB = true
			if len(g.Matches) != 1 {
				t.Fatalf("expected 1 match in group B, got %d", len(g.Matches))
			}
		}
	}
	if !foundA || !foundB {
		t.Fatalf("expected groups for both summaries, foundA=%t foundB=%t", foundA, foundB)
	}
}

func TestGrep_GroupLimitRespected(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	for i := 1; i <= 3; i++ {
		sumID := "sum_grep_group_limit_" + string(rune('0'+i))
		evtID := "evt_grep_group_limit_" + string(rune('0'+i))
		sum := insertGrepSummary(t, repo, sumID, "leaf", 0)
		evt := insertGrepEvent(t, repo, evtID, "sess_grep_limit", "prj_grep_limit", "needle group")
		if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: sum.ID, ChildKind: "event", ChildID: evt.ID, EdgeOrder: 0}); err != nil {
			t.Fatalf("insert edge: %v", err)
		}
	}

	result, err := svc.Grep(ctx, "needle", core.GrepOptions{SessionID: "sess_grep_limit", GroupLimit: 2})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if result.TotalHits != 3 {
		t.Fatalf("expected TotalHits=3, got %d", result.TotalHits)
	}
	if len(result.Groups) != 2 {
		t.Fatalf("expected 2 groups due to GroupLimit, got %d", len(result.Groups))
	}
}

func TestGrep_MatchesPerGroupRespected(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	leaf := insertGrepSummary(t, repo, "sum_grep_match_limit", "leaf", 0)
	for i := 1; i <= 4; i++ {
		evtID := "evt_grep_match_limit_" + string(rune('0'+i))
		evt := insertGrepEvent(t, repo, evtID, "sess_grep_match_limit", "prj_grep_match_limit", "needle repeated")
		if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: leaf.ID, ChildKind: "event", ChildID: evt.ID, EdgeOrder: i}); err != nil {
			t.Fatalf("insert edge: %v", err)
		}
	}

	result, err := svc.Grep(ctx, "needle", core.GrepOptions{SessionID: "sess_grep_match_limit", MatchesPerGroup: 2})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if result.TotalHits != 4 {
		t.Fatalf("expected TotalHits=4, got %d", result.TotalHits)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result.Groups))
	}
	if len(result.Groups[0].Matches) != 2 {
		t.Fatalf("expected 2 matches due to MatchesPerGroup, got %d", len(result.Groups[0].Matches))
	}
}

func TestGrep_MaxGroupDepthShallowest(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	sessionSummary := insertGrepSummary(t, repo, "sum_grep_session_0", "session", 2)
	topicSummary := insertGrepSummary(t, repo, "sum_grep_topic_0", "topic", 1)
	leafSummary := insertGrepSummary(t, repo, "sum_grep_leaf_0", "leaf", 0)
	evt := insertGrepEvent(t, repo, "evt_grep_depth_0", "sess_grep_depth_0", "prj_grep_depth_0", "needle depth test")

	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: sessionSummary.ID, ChildKind: "summary", ChildID: topicSummary.ID, EdgeOrder: 0}); err != nil {
		t.Fatalf("insert edge: %v", err)
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: topicSummary.ID, ChildKind: "summary", ChildID: leafSummary.ID, EdgeOrder: 0}); err != nil {
		t.Fatalf("insert edge: %v", err)
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: leafSummary.ID, ChildKind: "event", ChildID: evt.ID, EdgeOrder: 0}); err != nil {
		t.Fatalf("insert edge: %v", err)
	}

	result, err := svc.Grep(ctx, "needle", core.GrepOptions{SessionID: "sess_grep_depth_0"})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result.Groups))
	}
	if result.Groups[0].SummaryID != leafSummary.ID {
		t.Fatalf("expected shallowest group summary %s, got %s", leafSummary.ID, result.Groups[0].SummaryID)
	}
}

func TestGrep_MaxGroupDepthDeeper(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()

	sessionSummary := insertGrepSummary(t, repo, "sum_grep_session_1", "session", 2)
	topicSummary := insertGrepSummary(t, repo, "sum_grep_topic_1", "topic", 1)
	leafSummary := insertGrepSummary(t, repo, "sum_grep_leaf_1", "leaf", 0)
	evt := insertGrepEvent(t, repo, "evt_grep_depth_1", "sess_grep_depth_1", "prj_grep_depth_1", "needle depth parent")

	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: sessionSummary.ID, ChildKind: "summary", ChildID: topicSummary.ID, EdgeOrder: 0}); err != nil {
		t.Fatalf("insert edge: %v", err)
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: topicSummary.ID, ChildKind: "summary", ChildID: leafSummary.ID, EdgeOrder: 0}); err != nil {
		t.Fatalf("insert edge: %v", err)
	}
	if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: leafSummary.ID, ChildKind: "event", ChildID: evt.ID, EdgeOrder: 0}); err != nil {
		t.Fatalf("insert edge: %v", err)
	}

	result, err := svc.Grep(ctx, "needle", core.GrepOptions{SessionID: "sess_grep_depth_1", MaxGroupDepth: 1})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(result.Groups))
	}
	if result.Groups[0].SummaryID != topicSummary.ID {
		t.Fatalf("expected parent-level summary %s, got %s", topicSummary.ID, result.Groups[0].SummaryID)
	}
}

func TestGrep_ScopedSearchOverfetchesBeforeFiltering(t *testing.T) {
	svc, repo := testServiceAndRepo(t)
	ctx := context.Background()
	pattern := "needle_scoped_overfetch"

	for i := 0; i < 12; i++ {
		insertGrepEvent(t, repo, "evt_grep_scope_other_"+string(rune('a'+i)), "sess_other", "prj_other", "prefix "+pattern+" suffix")
	}

	inScope := insertGrepEvent(t, repo, "evt_grep_scope_target", "sess_target", "prj_target", "prefix "+pattern+" suffix")

	result, err := svc.Grep(ctx, pattern, core.GrepOptions{SessionID: "sess_target", GroupLimit: 2, MatchesPerGroup: 1})
	if err != nil {
		t.Fatalf("Grep: %v", err)
	}
	if result.TotalHits != 1 {
		t.Fatalf("expected one in-scope hit, got %d", result.TotalHits)
	}
	found := false
	for _, group := range result.Groups {
		for _, match := range group.Matches {
			if match.EventID == inScope.ID {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("expected scoped hit %s in groups, got %+v", inScope.ID, result.Groups)
	}
}
