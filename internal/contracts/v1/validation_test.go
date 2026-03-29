package v1

import (
	"strings"
	"testing"
)

func TestValidateIngestEvent(t *testing.T) {
	valid := &IngestEventRequest{Kind: "message_user", SourceSystem: "codex", Content: "hello"}
	if err := ValidateIngestEvent(valid); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}

	tests := []struct {
		name     string
		req      *IngestEventRequest
		contains string
	}{
		{name: "nil request", req: nil, contains: "request is nil"},
		{name: "missing kind", req: &IngestEventRequest{SourceSystem: "codex", Content: "x"}, contains: "kind is required"},
		{name: "missing source", req: &IngestEventRequest{Kind: "k", Content: "x"}, contains: "source_system is required"},
		{name: "missing content", req: &IngestEventRequest{Kind: "k", SourceSystem: "codex"}, contains: "content is required"},
		{name: "invalid privacy", req: &IngestEventRequest{Kind: "k", SourceSystem: "codex", Content: "x", PrivacyLevel: "secret"}, contains: "invalid privacy_level"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIngestEvent(tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("expected error containing %q, got %v", tt.contains, err)
			}
		})
	}
}

func TestValidateIngestTranscript(t *testing.T) {
	valid := &IngestTranscriptRequest{Events: []IngestEventRequest{{Kind: "message_user", SourceSystem: "codex", Content: "hi"}}}
	if err := ValidateIngestTranscript(valid); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}

	tests := []struct {
		name     string
		req      *IngestTranscriptRequest
		contains string
	}{
		{name: "nil request", req: nil, contains: "request is nil"},
		{name: "empty events", req: &IngestTranscriptRequest{}, contains: "events list is empty"},
		{name: "invalid event wrapped", req: &IngestTranscriptRequest{Events: []IngestEventRequest{{SourceSystem: "codex", Content: "x"}}}, contains: "event[0]: kind is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIngestTranscript(tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("expected error containing %q, got %v", tt.contains, err)
			}
		})
	}
}

func TestValidateRemember(t *testing.T) {
	valid := &RememberRequest{Type: "fact", Body: "b", TightDescription: "t", Scope: "project", PrivacyLevel: "shared"}
	if err := ValidateRemember(valid); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}

	tests := []struct {
		name     string
		req      *RememberRequest
		contains string
	}{
		{name: "nil request", req: nil, contains: "request is nil"},
		{name: "missing type", req: &RememberRequest{Body: "b", TightDescription: "t"}, contains: "type is required"},
		{name: "invalid type", req: &RememberRequest{Type: "bad", Body: "b", TightDescription: "t"}, contains: "invalid type"},
		{name: "missing body", req: &RememberRequest{Type: "fact", TightDescription: "t"}, contains: "body is required"},
		{name: "missing tight", req: &RememberRequest{Type: "fact", Body: "b"}, contains: "tight_description is required"},
		{name: "invalid scope", req: &RememberRequest{Type: "fact", Body: "b", TightDescription: "t", Scope: "user"}, contains: "invalid scope"},
		{name: "invalid privacy", req: &RememberRequest{Type: "fact", Body: "b", TightDescription: "t", PrivacyLevel: "top_secret"}, contains: "invalid privacy_level"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRemember(tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("expected error containing %q, got %v", tt.contains, err)
			}
		})
	}
}

func TestValidateRecall(t *testing.T) {
	if err := ValidateRecall(&RecallRequest{Query: "q", Mode: "hybrid", Limit: 10}); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}

	tests := []struct {
		name     string
		req      *RecallRequest
		contains string
	}{
		{name: "nil request", req: nil, contains: "request is nil"},
		{name: "missing query", req: &RecallRequest{}, contains: "query is required"},
		{name: "invalid mode", req: &RecallRequest{Query: "q", Mode: "bad"}, contains: "invalid mode"},
		{name: "negative limit", req: &RecallRequest{Query: "q", Limit: -1}, contains: "limit must be non-negative"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRecall(tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("expected error containing %q, got %v", tt.contains, err)
			}
		})
	}
}

func TestValidateDescribe(t *testing.T) {
	if err := ValidateDescribe(&DescribeRequest{IDs: []string{"a", "b"}}); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}

	tests := []struct {
		name     string
		req      *DescribeRequest
		contains string
	}{
		{name: "nil request", req: nil, contains: "request is nil"},
		{name: "empty ids", req: &DescribeRequest{}, contains: "ids list is empty"},
		{name: "blank id", req: &DescribeRequest{IDs: []string{"a", "  "}}, contains: "ids[1] is empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDescribe(tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("expected error containing %q, got %v", tt.contains, err)
			}
		})
	}
}

func TestValidateExpand(t *testing.T) {
	for _, kind := range []string{"memory", "summary", "episode"} {
		if err := ValidateExpand(&ExpandRequest{ID: "x", Kind: kind}); err != nil {
			t.Fatalf("expected valid kind %q, got %v", kind, err)
		}
	}

	tests := []struct {
		name     string
		req      *ExpandRequest
		contains string
	}{
		{name: "nil request", req: nil, contains: "request is nil"},
		{name: "missing id", req: &ExpandRequest{Kind: "memory"}, contains: "id is required"},
		{name: "missing kind", req: &ExpandRequest{ID: "x"}, contains: "kind is required"},
		{name: "invalid kind", req: &ExpandRequest{ID: "x", Kind: "entity"}, contains: "invalid kind"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateExpand(tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("expected error containing %q, got %v", tt.contains, err)
			}
		})
	}
}

func TestValidateHistory(t *testing.T) {
	if err := ValidateHistory(&HistoryRequest{Limit: 0}); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}

	tests := []struct {
		name     string
		req      *HistoryRequest
		contains string
	}{
		{name: "nil request", req: nil, contains: "request is nil"},
		{name: "negative limit", req: &HistoryRequest{Limit: -2}, contains: "limit must be non-negative"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHistory(tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("expected error containing %q, got %v", tt.contains, err)
			}
		})
	}
}

func TestValidateRunJob(t *testing.T) {
	validKinds := []string{
		"reflect",
		"compress_history",
		"consolidate_sessions",
		"build_topic_summaries",
		"rebuild_indexes",
		"rebuild_indexes_full",
		"extract_claims",
		"enrich_memories",
		"rebuild_entity_graph",
		"form_episodes",
		"detect_contradictions",
		"decay_stale_memory",
		"merge_duplicates",
		"cleanup_recall_history",
		"reprocess",
		"reprocess_all",
		"lifecycle_review",
		"cross_project_transfer",
		"archive_session_traces",
		"update_ranking_weights",
	}
	for _, kind := range validKinds {
		if err := ValidateRunJob(&RunJobRequest{Kind: kind}); err != nil {
			t.Fatalf("expected %s to be valid, got %v", kind, err)
		}
	}

	tests := []struct {
		name     string
		req      *RunJobRequest
		contains string
	}{
		{name: "nil request", req: nil, contains: "request is nil"},
		{name: "missing kind", req: &RunJobRequest{}, contains: "kind is required"},
		{name: "invalid kind", req: &RunJobRequest{Kind: "repair_links"}, contains: "invalid job kind"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRunJob(tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("expected error containing %q, got %v", tt.contains, err)
			}
		})
	}
}

func TestValidateRepair(t *testing.T) {
	if err := ValidateRepair(&RepairRequest{}); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}
	if err := ValidateRepair(nil); err == nil || !strings.Contains(err.Error(), "request is nil") {
		t.Fatalf("expected nil request error, got %v", err)
	}
}

func TestValidateExplainRecall(t *testing.T) {
	if err := ValidateExplainRecall(&ExplainRecallRequest{Query: "q", ItemID: "m1"}); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}

	tests := []struct {
		name     string
		req      *ExplainRecallRequest
		contains string
	}{
		{name: "nil request", req: nil, contains: "request is nil"},
		{name: "missing query", req: &ExplainRecallRequest{ItemID: "m1"}, contains: "query is required"},
		{name: "missing item", req: &ExplainRecallRequest{Query: "q"}, contains: "item_id is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateExplainRecall(tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("expected error containing %q, got %v", tt.contains, err)
			}
		})
	}
}

func TestValidateGetMemory(t *testing.T) {
	if err := ValidateGetMemory(&GetMemoryRequest{ID: "m1"}); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}

	if err := ValidateGetMemory(nil); err == nil || !strings.Contains(err.Error(), "request is nil") {
		t.Fatalf("expected nil request error, got %v", err)
	}
	if err := ValidateGetMemory(&GetMemoryRequest{}); err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("expected missing id error, got %v", err)
	}
}

func TestValidateUpdateMemory(t *testing.T) {
	if err := ValidateUpdateMemory(&UpdateMemoryRequest{ID: "m1", Status: "archived"}); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}

	tests := []struct {
		name     string
		req      *UpdateMemoryRequest
		contains string
	}{
		{name: "nil request", req: nil, contains: "request is nil"},
		{name: "missing id", req: &UpdateMemoryRequest{}, contains: "id is required"},
		{name: "invalid type", req: &UpdateMemoryRequest{ID: "m1", Type: "invalid"}, contains: "invalid type"},
		{name: "invalid scope", req: &UpdateMemoryRequest{ID: "m1", Scope: "workspace"}, contains: "invalid scope"},
		{name: "invalid status", req: &UpdateMemoryRequest{ID: "m1", Status: "deleted"}, contains: "invalid status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateUpdateMemory(tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("expected error containing %q, got %v", tt.contains, err)
			}
		})
	}
}

func TestValidateShare(t *testing.T) {
	if err := ValidateShare(ShareRequest{ID: "m1", Privacy: "shared"}); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}

	tests := []struct {
		name     string
		req      ShareRequest
		contains string
	}{
		{name: "missing id", req: ShareRequest{Privacy: "shared"}, contains: "id is required"},
		{name: "invalid privacy", req: ShareRequest{ID: "m1", Privacy: "team_only"}, contains: "invalid privacy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateShare(tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("expected error containing %q, got %v", tt.contains, err)
			}
		})
	}
}

func TestValidateForget(t *testing.T) {
	if err := ValidateForget(&ForgetRequest{ID: "m1"}); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}

	if err := ValidateForget(nil); err == nil || !strings.Contains(err.Error(), "request is nil") {
		t.Fatalf("expected nil request error, got %v", err)
	}
	if err := ValidateForget(&ForgetRequest{}); err == nil || !strings.Contains(err.Error(), "id is required") {
		t.Fatalf("expected missing id error, got %v", err)
	}
}

func TestValidatePolicyAdd(t *testing.T) {
	if err := ValidatePolicyAdd(&PolicyAddRequest{PatternType: "source", Pattern: "svc-*", Mode: "read_only"}); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}
	if err := ValidatePolicyAdd(&PolicyAddRequest{PatternType: "source", Pattern: "^svc-[a-z]+$", Mode: "read_only", MatchMode: "regex"}); err != nil {
		t.Fatalf("expected valid regex request, got %v", err)
	}

	tests := []struct {
		name     string
		req      *PolicyAddRequest
		contains string
	}{
		{name: "nil request", req: nil, contains: "request is nil"},
		{name: "missing pattern type", req: &PolicyAddRequest{Pattern: "svc-*", Mode: "full"}, contains: "pattern_type is required"},
		{name: "invalid pattern type", req: &PolicyAddRequest{PatternType: "unknown", Pattern: "svc-*", Mode: "full"}, contains: "invalid pattern_type"},
		{name: "missing pattern", req: &PolicyAddRequest{PatternType: "source", Mode: "full"}, contains: "pattern is required"},
		{name: "missing mode", req: &PolicyAddRequest{PatternType: "source", Pattern: "svc-*"}, contains: "mode is required"},
		{name: "invalid mode", req: &PolicyAddRequest{PatternType: "source", Pattern: "svc-*", Mode: "drop"}, contains: "invalid mode"},
		{name: "invalid match mode", req: &PolicyAddRequest{PatternType: "source", Pattern: "svc-*", Mode: "full", MatchMode: "wildcard"}, contains: "invalid match_mode"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePolicyAdd(tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("expected error containing %q, got %v", tt.contains, err)
			}
		})
	}
}

func TestValidatePolicyAdd_AcceptsKindPatternType(t *testing.T) {
	if err := ValidatePolicyAdd(&PolicyAddRequest{PatternType: "kind", Pattern: "tool_result", Mode: "ignore"}); err != nil {
		t.Fatalf("expected kind pattern_type to be valid, got %v", err)
	}
	if err := ValidatePolicyAdd(&PolicyAddRequest{PatternType: "kind", Pattern: "tool_*", Mode: "ignore", MatchMode: "glob"}); err != nil {
		t.Fatalf("expected kind glob pattern_type to be valid, got %v", err)
	}
}

func TestValidateResetDerived(t *testing.T) {
	if err := ValidateResetDerived(&ResetDerivedRequest{Confirm: true}); err != nil {
		t.Fatalf("expected valid request, got %v", err)
	}

	tests := []struct {
		name     string
		req      *ResetDerivedRequest
		contains string
	}{
		{name: "nil request", req: nil, contains: "request is nil"},
		{name: "confirm false", req: &ResetDerivedRequest{Confirm: false}, contains: "confirm must be true"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResetDerived(tt.req)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("expected error containing %q, got %v", tt.contains, err)
			}
		})
	}
}

func TestValidateProjectAndRelationshipAndLookupRequests(t *testing.T) {
	t.Run("register project", func(t *testing.T) {
		if err := ValidateRegisterProject(&RegisterProjectRequest{Name: "proj"}); err != nil {
			t.Fatalf("expected valid register project, got %v", err)
		}
		if err := ValidateRegisterProject(nil); err == nil || !strings.Contains(err.Error(), "request is nil") {
			t.Fatalf("expected nil request error, got %v", err)
		}
		if err := ValidateRegisterProject(&RegisterProjectRequest{}); err == nil || !strings.Contains(err.Error(), "name is required") {
			t.Fatalf("expected missing name error, got %v", err)
		}
	})

	t.Run("project id requests", func(t *testing.T) {
		if err := ValidateGetProject(&GetProjectRequest{ID: "proj_1"}); err != nil {
			t.Fatalf("expected valid get project request, got %v", err)
		}
		if err := ValidateRemoveProject(&RemoveProjectRequest{ID: "proj_1"}); err != nil {
			t.Fatalf("expected valid remove project request, got %v", err)
		}
		if err := ValidateGetProject(&GetProjectRequest{}); err == nil || !strings.Contains(err.Error(), "id is required") {
			t.Fatalf("expected missing id error, got %v", err)
		}
		if err := ValidateRemoveProject(nil); err == nil || !strings.Contains(err.Error(), "request is nil") {
			t.Fatalf("expected nil request error, got %v", err)
		}
	})

	t.Run("add relationship", func(t *testing.T) {
		if err := ValidateAddRelationship(&AddRelationshipRequest{FromEntityID: "ent_a", ToEntityID: "ent_b", RelationshipType: "related_to"}); err != nil {
			t.Fatalf("expected valid add relationship request, got %v", err)
		}
		if err := ValidateAddRelationship(&AddRelationshipRequest{ToEntityID: "ent_b", RelationshipType: "related_to"}); err == nil || !strings.Contains(err.Error(), "from_entity_id is required") {
			t.Fatalf("expected missing from_entity_id error, got %v", err)
		}
		if err := ValidateAddRelationship(&AddRelationshipRequest{FromEntityID: "ent_a", RelationshipType: "related_to"}); err == nil || !strings.Contains(err.Error(), "to_entity_id is required") {
			t.Fatalf("expected missing to_entity_id error, got %v", err)
		}
		if err := ValidateAddRelationship(&AddRelationshipRequest{FromEntityID: "ent_a", ToEntityID: "ent_b"}); err == nil || !strings.Contains(err.Error(), "relationship_type is required") {
			t.Fatalf("expected missing relationship_type error, got %v", err)
		}
	})

	t.Run("relationship list and id requests", func(t *testing.T) {
		if err := ValidateListRelationships(&ListRelationshipsRequest{EntityID: "ent", RelationshipType: "related_to", Limit: 0}); err != nil {
			t.Fatalf("expected valid list relationships request, got %v", err)
		}
		if err := ValidateListRelationships(&ListRelationshipsRequest{Limit: -1}); err == nil || !strings.Contains(err.Error(), "limit must be non-negative") {
			t.Fatalf("expected negative limit error, got %v", err)
		}
		if err := ValidateGetRelationship(&GetRelationshipRequest{ID: "rel_1"}); err != nil {
			t.Fatalf("expected valid get relationship request, got %v", err)
		}
		if err := ValidateRemoveRelationship(&RemoveRelationshipRequest{ID: "rel_1"}); err != nil {
			t.Fatalf("expected valid remove relationship request, got %v", err)
		}
		if err := ValidateGetRelationship(nil); err == nil || !strings.Contains(err.Error(), "request is nil") {
			t.Fatalf("expected nil request error, got %v", err)
		}
		if err := ValidateRemoveRelationship(&RemoveRelationshipRequest{}); err == nil || !strings.Contains(err.Error(), "id is required") {
			t.Fatalf("expected missing id error, got %v", err)
		}
	})

	t.Run("summary episode entity id requests", func(t *testing.T) {
		if err := ValidateGetSummary(&GetSummaryRequest{ID: "sum_1"}); err != nil {
			t.Fatalf("expected valid get summary request, got %v", err)
		}
		if err := ValidateGetEpisode(&GetEpisodeRequest{ID: "ep_1"}); err != nil {
			t.Fatalf("expected valid get episode request, got %v", err)
		}
		if err := ValidateGetEntity(&GetEntityRequest{ID: "ent_1"}); err != nil {
			t.Fatalf("expected valid get entity request, got %v", err)
		}
		if err := ValidateGetSummary(nil); err == nil || !strings.Contains(err.Error(), "request is nil") {
			t.Fatalf("expected nil summary request error, got %v", err)
		}
		if err := ValidateGetEpisode(&GetEpisodeRequest{}); err == nil || !strings.Contains(err.Error(), "id is required") {
			t.Fatalf("expected missing episode id error, got %v", err)
		}
		if err := ValidateGetEntity(&GetEntityRequest{}); err == nil || !strings.Contains(err.Error(), "id is required") {
			t.Fatalf("expected missing entity id error, got %v", err)
		}
	})
}
