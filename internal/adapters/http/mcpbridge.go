package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"

	v1 "github.com/bonztm/agent-memory-manager/internal/contracts/v1"
	"github.com/bonztm/agent-memory-manager/internal/core"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func newMCPBridge(svc core.Service, version string) *mcpserver.MCPServer {
	s := mcpserver.NewMCPServer("amm", version)
	registerAllTools(s, svc)
	return s
}

func registerAllTools(s *mcpserver.MCPServer, svc core.Service) {
	addJSONTool(s, toolInit(), func(ctx context.Context, _ map[string]any) (any, error) {
		status, err := svc.Status(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]any{"message": "already initialized", "status": status}, nil
	})

	addJSONTool(s, toolIngestEvent(), func(ctx context.Context, args map[string]any) (any, error) {
		var evt core.Event
		if err := decodeMap(args, &evt); err != nil {
			return nil, err
		}
		return svc.IngestEvent(ctx, &evt)
	})

	addJSONTool(s, toolIngestTranscript(), func(ctx context.Context, args map[string]any) (any, error) {
		var payload struct {
			Events []*core.Event `json:"events"`
		}
		if err := decodeMap(args, &payload); err != nil {
			return nil, err
		}
		return svc.IngestTranscript(ctx, payload.Events)
	})

	addJSONTool(s, toolRemember(), func(ctx context.Context, args map[string]any) (any, error) {
		var mem core.Memory
		if err := decodeMap(args, &mem); err != nil {
			return nil, err
		}
		return svc.Remember(ctx, &mem)
	})

	addJSONTool(s, toolRecall(), func(ctx context.Context, args map[string]any) (any, error) {
		query := stringArg(args, "query")
		opts := core.RecallOptions{}
		if optsMap, ok := args["opts"].(map[string]any); ok {
			if err := decodeMap(optsMap, &opts); err != nil {
				return nil, err
			}
		}
		if opts.AgentID == "" {
			opts.AgentID = stringArg(args, "agent_id")
		}
		if boolArg(args, "explain") {
			opts.Explain = true
		}
		if err := v1.ValidateRecall(&v1.RecallRequest{
			Query:     query,
			Mode:      string(opts.Mode),
			ProjectID: opts.ProjectID,
			SessionID: opts.SessionID,
			AgentID:   opts.AgentID,
			EntityIDs: opts.EntityIDs,
			Limit:     opts.Limit,
			Explain:   opts.Explain,
			After:     opts.After,
			Before:    opts.Before,
		}); err != nil {
			return nil, err
		}
		return svc.Recall(ctx, query, opts)
	})

	addJSONTool(s, toolDescribe(), func(ctx context.Context, args map[string]any) (any, error) {
		return svc.Describe(ctx, stringSliceArg(args, "ids"))
	})

	addJSONTool(s, toolExpand(), func(ctx context.Context, args map[string]any) (any, error) {
		id := stringArg(args, "id")
		kind := stringArg(args, "kind")
		if kind == "" {
			kind = "memory"
		}
		delegationDepth, err := nonNegativeIntArg(args, "delegation_depth")
		if err != nil {
			return nil, err
		}
		maxDepth, err := nonNegativeIntArg(args, "max_depth")
		if err != nil {
			return nil, err
		}
		if maxDepth > 5 {
			return nil, fmt.Errorf("max_depth must be between 0 and 5")
		}
		opts := core.ExpandOptions{SessionID: stringArg(args, "session_id"), DelegationDepth: delegationDepth, MaxDepth: maxDepth}
		if opts.DelegationDepth < 0 {
			return nil, fmt.Errorf("delegation_depth must be non-negative")
		}
		return svc.Expand(ctx, id, kind, opts)
	})

	addJSONTool(s, toolHistory(), func(ctx context.Context, args map[string]any) (any, error) {
		query := stringArg(args, "query")
		opts := core.HistoryOptions{}
		if optsMap, ok := args["opts"].(map[string]any); ok {
			if err := decodeMap(optsMap, &opts); err != nil {
				return nil, err
			}
		}
		return svc.History(ctx, query, opts)
	})

	addJSONTool(s, toolGetMemory(), func(ctx context.Context, args map[string]any) (any, error) {
		return svc.GetMemory(ctx, stringArg(args, "id"))
	})

	addJSONTool(s, toolUpdateMemory(), func(ctx context.Context, args map[string]any) (any, error) {
		id := stringArg(args, "id")
		mem, err := svc.GetMemory(ctx, id)
		if err != nil {
			return nil, err
		}
		if v, ok := args["body"].(string); ok {
			mem.Body = v
		}
		if v, ok := args["tight_description"].(string); ok {
			mem.TightDescription = v
		}
		if v, ok := args["status"].(string); ok {
			mem.Status = core.MemoryStatus(v)
		}
		if v, ok := args["subject"].(string); ok {
			mem.Subject = v
		}
		if v, ok := args["type"].(string); ok {
			mem.Type = core.MemoryType(v)
		}
		if v, ok := args["scope"].(string); ok {
			mem.Scope = core.Scope(v)
		}
		if md, ok := stringMapArg(args, "metadata"); ok {
			mem.Metadata = md
		}
		return svc.UpdateMemory(ctx, mem)
	})

	addJSONTool(s, toolShare(), func(ctx context.Context, args map[string]any) (any, error) {
		return svc.ShareMemory(ctx, stringArg(args, "id"), core.PrivacyLevel(stringArg(args, "privacy")))
	})

	addJSONTool(s, toolForget(), func(ctx context.Context, args map[string]any) (any, error) {
		return svc.ForgetMemory(ctx, stringArg(args, "id"))
	})

	addJSONTool(s, toolJobsRun(), func(ctx context.Context, args map[string]any) (any, error) {
		return svc.RunJob(ctx, stringArg(args, "kind"))
	})

	addJSONTool(s, toolExplainRecall(), func(ctx context.Context, args map[string]any) (any, error) {
		return svc.ExplainRecall(ctx, stringArg(args, "query"), stringArg(args, "item_id"))
	})

	addJSONTool(s, toolRepair(), func(ctx context.Context, args map[string]any) (any, error) {
		return svc.Repair(ctx, boolArg(args, "check"), stringArg(args, "fix"))
	})

	addJSONTool(s, toolStatus(), func(ctx context.Context, _ map[string]any) (any, error) {
		return svc.Status(ctx)
	})

	addJSONTool(s, toolResetDerived(), func(ctx context.Context, args map[string]any) (any, error) {
		if !boolArg(args, "confirm") {
			return nil, fmt.Errorf("confirm must be true to reset derived data")
		}
		return svc.ResetDerived(ctx)
	})

	addJSONTool(s, toolPolicyList(), func(ctx context.Context, _ map[string]any) (any, error) {
		return svc.ListPolicies(ctx)
	})

	addJSONTool(s, toolPolicyAdd(), func(ctx context.Context, args map[string]any) (any, error) {
		policy := &core.IngestionPolicy{
			PatternType: stringArg(args, "pattern_type"),
			Pattern:     stringArg(args, "pattern"),
			Mode:        stringArg(args, "mode"),
			Priority:    intArg(args, "priority"),
			MatchMode:   stringArg(args, "match_mode"),
		}
		return svc.AddPolicy(ctx, policy)
	})

	addJSONTool(s, toolPolicyRemove(), func(ctx context.Context, args map[string]any) (any, error) {
		id := stringArg(args, "id")
		if err := svc.RemovePolicy(ctx, id); err != nil {
			return nil, err
		}
		return map[string]any{"id": id, "status": "removed"}, nil
	})

	addJSONTool(s, toolRegisterProject(), func(ctx context.Context, args map[string]any) (any, error) {
		project := &core.Project{
			Name:        stringArg(args, "name"),
			Path:        stringArg(args, "path"),
			Description: stringArg(args, "description"),
		}
		return svc.RegisterProject(ctx, project)
	})

	addJSONTool(s, toolGetProject(), func(ctx context.Context, args map[string]any) (any, error) {
		return svc.GetProject(ctx, stringArg(args, "id"))
	})

	addJSONTool(s, toolListProjects(), func(ctx context.Context, _ map[string]any) (any, error) {
		return svc.ListProjects(ctx)
	})

	addJSONTool(s, toolRemoveProject(), func(ctx context.Context, args map[string]any) (any, error) {
		id := stringArg(args, "id")
		if err := svc.RemoveProject(ctx, id); err != nil {
			return nil, err
		}
		return map[string]any{"id": id, "status": "removed"}, nil
	})

	addJSONTool(s, toolAddRelationship(), func(ctx context.Context, args map[string]any) (any, error) {
		rel := &core.Relationship{
			FromEntityID:     stringArg(args, "from_entity_id"),
			ToEntityID:       stringArg(args, "to_entity_id"),
			RelationshipType: stringArg(args, "relationship_type"),
		}
		return svc.AddRelationship(ctx, rel)
	})

	addJSONTool(s, toolGetRelationship(), func(ctx context.Context, args map[string]any) (any, error) {
		return svc.GetRelationship(ctx, stringArg(args, "id"))
	})

	addJSONTool(s, toolListRelationships(), func(ctx context.Context, args map[string]any) (any, error) {
		opts := core.ListRelationshipsOptions{
			EntityID:         stringArg(args, "entity_id"),
			RelationshipType: stringArg(args, "relationship_type"),
			Limit:            intArg(args, "limit"),
		}
		return svc.ListRelationships(ctx, opts)
	})

	addJSONTool(s, toolRemoveRelationship(), func(ctx context.Context, args map[string]any) (any, error) {
		id := stringArg(args, "id")
		if err := svc.RemoveRelationship(ctx, id); err != nil {
			return nil, err
		}
		return map[string]any{"id": id, "status": "removed"}, nil
	})

	addJSONTool(s, toolGetSummary(), func(ctx context.Context, args map[string]any) (any, error) {
		return svc.GetSummary(ctx, stringArg(args, "id"))
	})

	addJSONTool(s, toolGetEpisode(), func(ctx context.Context, args map[string]any) (any, error) {
		return svc.GetEpisode(ctx, stringArg(args, "id"))
	})

	addJSONTool(s, toolGetEntity(), func(ctx context.Context, args map[string]any) (any, error) {
		return svc.GetEntity(ctx, stringArg(args, "id"))
	})

	addJSONTool(s, toolFormatContextWindow(), func(ctx context.Context, args map[string]any) (any, error) {
		freshTailCount, err := nonNegativeIntArg(args, "fresh_tail_count")
		if err != nil {
			return nil, err
		}
		maxSummaryDepth, err := nonNegativeIntArg(args, "max_summary_depth")
		if err != nil {
			return nil, err
		}
		req := v1.FormatContextWindowRequest{
			SessionID:         stringArg(args, "session_id"),
			ProjectID:         stringArg(args, "project_id"),
			FreshTailCount:    freshTailCount,
			MaxSummaryDepth:   maxSummaryDepth,
			IncludeParentRefs: boolArg(args, "include_parent_refs"),
		}
		if err := v1.ValidateFormatContextWindow(&req); err != nil {
			return nil, err
		}
		opts := core.FormatContextWindowOptions{
			SessionID:         req.SessionID,
			ProjectID:         req.ProjectID,
			FreshTailCount:    req.FreshTailCount,
			MaxSummaryDepth:   req.MaxSummaryDepth,
			IncludeParentRefs: req.IncludeParentRefs,
		}
		return svc.FormatContextWindow(ctx, opts)
	})

	addJSONTool(s, toolGrep(), func(ctx context.Context, args map[string]any) (any, error) {
		maxGroupDepth, err := nonNegativeIntArg(args, "max_group_depth")
		if err != nil {
			return nil, err
		}
		groupLimit, err := nonNegativeIntArg(args, "group_limit")
		if err != nil {
			return nil, err
		}
		matchesPerGroup, err := nonNegativeIntArg(args, "matches_per_group")
		if err != nil {
			return nil, err
		}
		req := v1.GrepRequest{
			Pattern:         stringArg(args, "pattern"),
			SessionID:       stringArg(args, "session_id"),
			ProjectID:       stringArg(args, "project_id"),
			MaxGroupDepth:   maxGroupDepth,
			GroupLimit:      groupLimit,
			MatchesPerGroup: matchesPerGroup,
		}
		if err := v1.ValidateGrep(&req); err != nil {
			return nil, err
		}
		opts := core.GrepOptions{
			SessionID:       req.SessionID,
			ProjectID:       req.ProjectID,
			MaxGroupDepth:   req.MaxGroupDepth,
			GroupLimit:      req.GroupLimit,
			MatchesPerGroup: req.MatchesPerGroup,
		}
		return svc.Grep(ctx, req.Pattern, opts)
	})
}

func addJSONTool(s *mcpserver.MCPServer, tool mcp.Tool, handler func(context.Context, map[string]any) (any, error)) {
	s.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		slog.Info("mcp tool call", "tool", req.Params.Name, "args_keys", mapKeys(args))
		result, err := handler(ctx, args)
		if err != nil {
			slog.Error("mcp tool error", "tool", req.Params.Name, "error", err)
			return mcp.NewToolResultError(err.Error()), nil
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			slog.Error("mcp tool error", "tool", req.Params.Name, "error", err)
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(encoded)), nil
	})
}

func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func decodeMap(in map[string]any, out any) error {
	b, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func boolArg(args map[string]any, key string) bool {
	v, _ := args[key].(bool)
	return v
}

func intArg(args map[string]any, key string) int {
	v, ok := args[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float32:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func intArgStrict(args map[string]any, key string) (int, error) {
	v, ok := args[key]
	if !ok {
		return 0, nil
	}
	switch n := v.(type) {
	case int:
		return n, nil
	case int32:
		return int(n), nil
	case int64:
		return int(n), nil
	case float32:
		if math.Trunc(float64(n)) != float64(n) {
			return 0, fmt.Errorf("%s must be a whole number", key)
		}
		return int(n), nil
	case float64:
		if math.Trunc(n) != n {
			return 0, fmt.Errorf("%s must be a whole number", key)
		}
		return int(n), nil
	default:
		return 0, fmt.Errorf("%s must be an integer", key)
	}
}

func nonNegativeIntArg(args map[string]any, key string) (int, error) {
	v, err := intArgStrict(args, key)
	if err != nil {
		return 0, err
	}
	if v < 0 {
		return 0, fmt.Errorf("%s must be non-negative", key)
	}
	return v, nil
}

func stringSliceArg(args map[string]any, key string) []string {
	v, ok := args[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(v))
	for _, item := range v {
		s, ok := item.(string)
		if ok {
			out = append(out, s)
		}
	}
	return out
}

func stringMapArg(args map[string]any, key string) (map[string]string, bool) {
	raw, ok := args[key].(map[string]any)
	if !ok {
		return nil, false
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		out[k] = s
	}
	return out, true
}

func toolInit() mcp.Tool {
	return mcp.NewTool("amm_init", mcp.WithDescription("Initialize the amm database"))
}

func toolIngestEvent() mcp.Tool {
	return mcp.NewTool(
		"amm_ingest_event",
		mcp.WithDescription("Append a raw event to history"),
		mcp.WithString("kind", mcp.Required(), mcp.Description("Event kind (e.g. message_user, message_assistant)")),
		mcp.WithString("source_system", mcp.Required(), mcp.Description("Source system identifier")),
		mcp.WithString("content", mcp.Required(), mcp.Description("Event content")),
		mcp.WithString("session_id", mcp.Description("Session identifier")),
		mcp.WithString("project_id", mcp.Description("Project identifier")),
		mcp.WithString("agent_id", mcp.Description("Agent identifier")),
		mcp.WithString("surface", mcp.Description("Interaction surface")),
		mcp.WithString("occurred_at", mcp.Description("When the event occurred (RFC3339)")),
	)
}

func toolIngestTranscript() mcp.Tool {
	eventObjectSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"kind":          map[string]any{"type": "string"},
			"source_system": map[string]any{"type": "string"},
			"content":       map[string]any{"type": "string"},
			"session_id":    map[string]any{"type": "string"},
			"project_id":    map[string]any{"type": "string"},
			"agent_id":      map[string]any{"type": "string"},
			"surface":       map[string]any{"type": "string"},
			"occurred_at":   map[string]any{"type": "string"},
		},
		"required": []string{"kind", "source_system", "content"},
	}
	return mcp.NewTool(
		"amm_ingest_transcript",
		mcp.WithDescription("Bulk ingest a sequence of events"),
		mcp.WithArray("events", mcp.Required(), mcp.Description("List of events to ingest"), mcp.Items(eventObjectSchema)),
	)
}

func toolRemember() mcp.Tool {
	return mcp.NewTool(
		"amm_remember",
		mcp.WithDescription("Commit a durable memory"),
		mcp.WithString("type", mcp.Required(), mcp.Description("Memory type")),
		mcp.WithString("body", mcp.Required(), mcp.Description("Memory body")),
		mcp.WithString("tight_description", mcp.Required(), mcp.Description("One-line summary")),
		mcp.WithString("scope", mcp.Description("Scope: global, project, session")),
		mcp.WithString("subject", mcp.Description("Subject of the memory")),
		mcp.WithString("project_id", mcp.Description("Project identifier")),
		mcp.WithString("session_id", mcp.Description("Session identifier")),
		mcp.WithString("agent_id", mcp.Description("Agent identifier")),
	)
}

func toolRecall() mcp.Tool {
	return mcp.NewTool(
		"amm_recall",
		mcp.WithDescription("Retrieve memories using various modes"),
		mcp.WithString("query", mcp.Description("Search query (optional for mode=sessions)")),
		mcp.WithString("agent_id", mcp.Description("Agent identifier")),
		mcp.WithBoolean("explain", mcp.Description("Include score signal breakdowns in each recall item")),
		mcp.WithObject("opts", mcp.Description("Recall options")),
	)
}

func toolDescribe() mcp.Tool {
	return mcp.NewTool(
		"amm_describe",
		mcp.WithDescription("Get thin descriptions of items"),
		mcp.WithArray("ids", mcp.Required(), mcp.Items(map[string]any{"type": "string"})),
	)
}

func toolExpand() mcp.Tool {
	return mcp.NewTool(
		"amm_expand",
		mcp.WithDescription("Expand an item to full detail"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Item ID to expand")),
		mcp.WithString("kind", mcp.Description("Item kind: memory, summary, episode (defaults to 'memory' if omitted)")),
		mcp.WithString("session_id", mcp.Description("Session identifier for relevance feedback")),
		mcp.WithNumber("delegation_depth", mcp.Description("Current delegation depth for recursion control")),
		mcp.WithNumber("max_depth", mcp.Description("Recursively expand child summaries up to this many levels deep (0 = no recursion, default; 1 = expand children one level; max 5)")),
	)
}

func toolHistory() mcp.Tool {
	return mcp.NewTool(
		"amm_history",
		mcp.WithDescription("Query raw interaction history"),
		mcp.WithString("query", mcp.Description("Search query")),
		mcp.WithObject("opts", mcp.Description("History options")),
	)
}

func toolGetMemory() mcp.Tool {
	return mcp.NewTool(
		"amm_get_memory",
		mcp.WithDescription("Get a single memory by ID"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Item ID")),
	)
}

func toolUpdateMemory() mcp.Tool {
	return mcp.NewTool(
		"amm_update_memory",
		mcp.WithDescription("Update an existing memory"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Memory ID to update")),
		mcp.WithString("body", mcp.Description("Updated memory body")),
		mcp.WithString("tight_description", mcp.Description("Updated one-line summary")),
		mcp.WithString("status", mcp.Description("Memory status: active, superseded, archived, retracted")),
		mcp.WithString("subject", mcp.Description("Subject of the memory")),
		mcp.WithString("type", mcp.Description("Memory type")),
		mcp.WithString("scope", mcp.Description("Scope of the memory")),
		mcp.WithObject("metadata", mcp.Description("Metadata object")),
	)
}

func toolShare() mcp.Tool {
	return mcp.NewTool(
		"amm_share",
		mcp.WithDescription("Update a memory privacy level"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Memory ID to share")),
		mcp.WithString("privacy", mcp.Required(), mcp.Description("Privacy level: private, shared, public_safe")),
	)
}

func toolForget() mcp.Tool {
	return mcp.NewTool(
		"amm_forget",
		mcp.WithDescription("Forget (retract) a memory by ID"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Item ID")),
	)
}

func toolJobsRun() mcp.Tool {
	return mcp.NewTool(
		"amm_jobs_run",
		mcp.WithDescription("Run a maintenance job"),
		mcp.WithString("kind", mcp.Required(), mcp.Description("Job kind to run")),
	)
}

func toolExplainRecall() mcp.Tool {
	return mcp.NewTool(
		"amm_explain_recall",
		mcp.WithDescription("Explain why an item surfaced"),
		mcp.WithString("query", mcp.Required(), mcp.Description("Search query")),
		mcp.WithString("item_id", mcp.Required(), mcp.Description("Item ID to explain")),
	)
}

func toolRepair() mcp.Tool {
	return mcp.NewTool(
		"amm_repair",
		mcp.WithDescription("Run integrity checks and repairs"),
		mcp.WithBoolean("check", mcp.Description("Check only mode")),
		mcp.WithString("fix", mcp.Description("What to fix: indexes, links, recall_history")),
	)
}

func toolStatus() mcp.Tool {
	return mcp.NewTool("amm_status", mcp.WithDescription("Get system status"))
}

func toolResetDerived() mcp.Tool {
	return mcp.NewTool(
		"amm_reset_derived",
		mcp.WithDescription("Purge all derived data while preserving events"),
		mcp.WithBoolean("confirm", mcp.Required(), mcp.Description("Confirmation flag")),
	)
}

func toolPolicyList() mcp.Tool {
	return mcp.NewTool("amm_policy_list", mcp.WithDescription("List all ingestion policies"))
}

func toolPolicyAdd() mcp.Tool {
	return mcp.NewTool(
		"amm_policy_add",
		mcp.WithDescription("Add an ingestion policy"),
		mcp.WithString("pattern_type", mcp.Required(), mcp.Description("Pattern type: kind, session, source, surface, agent, project, runtime")),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Policy pattern")),
		mcp.WithString("mode", mcp.Required(), mcp.Description("Ingestion mode: full, read_only, ignore")),
		mcp.WithNumber("priority", mcp.Description("Priority ordering (higher wins)")),
		mcp.WithString("match_mode", mcp.Description("Pattern match mode: exact, glob, regex")),
	)
}

func toolPolicyRemove() mcp.Tool {
	return mcp.NewTool(
		"amm_policy_remove",
		mcp.WithDescription("Remove an ingestion policy by ID"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Policy ID to remove")),
	)
}

func toolRegisterProject() mcp.Tool {
	return mcp.NewTool(
		"amm_register_project",
		mcp.WithDescription("Register a new project"),
		mcp.WithString("name", mcp.Required(), mcp.Description("Project name")),
		mcp.WithString("path", mcp.Description("Project path")),
		mcp.WithString("description", mcp.Description("Project description")),
	)
}

func toolGetProject() mcp.Tool {
	return mcp.NewTool(
		"amm_get_project",
		mcp.WithDescription("Get a project by ID"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Item ID")),
	)
}

func toolListProjects() mcp.Tool {
	return mcp.NewTool("amm_list_projects", mcp.WithDescription("List all projects"))
}

func toolRemoveProject() mcp.Tool {
	return mcp.NewTool(
		"amm_remove_project",
		mcp.WithDescription("Remove a project by ID"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Item ID")),
	)
}

func toolAddRelationship() mcp.Tool {
	return mcp.NewTool(
		"amm_add_relationship",
		mcp.WithDescription("Add an entity relationship"),
		mcp.WithString("from_entity_id", mcp.Required(), mcp.Description("Source entity ID")),
		mcp.WithString("to_entity_id", mcp.Required(), mcp.Description("Destination entity ID")),
		mcp.WithString("relationship_type", mcp.Required(), mcp.Description("Relationship type")),
	)
}

func toolGetRelationship() mcp.Tool {
	return mcp.NewTool(
		"amm_get_relationship",
		mcp.WithDescription("Get a relationship by ID"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Item ID")),
	)
}

func toolListRelationships() mcp.Tool {
	return mcp.NewTool(
		"amm_list_relationships",
		mcp.WithDescription("List relationships"),
		mcp.WithString("entity_id", mcp.Description("Filter by entity ID")),
		mcp.WithString("relationship_type", mcp.Description("Filter by relationship type")),
		mcp.WithNumber("limit", mcp.Description("Max results to return")),
	)
}

func toolRemoveRelationship() mcp.Tool {
	return mcp.NewTool(
		"amm_remove_relationship",
		mcp.WithDescription("Remove a relationship by ID"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Item ID")),
	)
}

func toolGetSummary() mcp.Tool {
	return mcp.NewTool(
		"amm_get_summary",
		mcp.WithDescription("Get a summary by ID"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Item ID")),
	)
}

func toolGetEpisode() mcp.Tool {
	return mcp.NewTool(
		"amm_get_episode",
		mcp.WithDescription("Get an episode by ID"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Item ID")),
	)
}

func toolGetEntity() mcp.Tool {
	return mcp.NewTool(
		"amm_get_entity",
		mcp.WithDescription("Get an entity by ID"),
		mcp.WithString("id", mcp.Required(), mcp.Description("Item ID")),
	)
}

func toolFormatContextWindow() mcp.Tool {
	return mcp.NewTool(
		"amm_format_context_window",
		mcp.WithDescription("Assemble deterministic context from summaries and fresh events"),
		mcp.WithString("session_id", mcp.Description("Session identifier")),
		mcp.WithString("project_id", mcp.Description("Project identifier")),
		mcp.WithNumber("fresh_tail_count", mcp.Description("Number of fresh events to include (default 32)")),
		mcp.WithNumber("max_summary_depth", mcp.Description("Maximum summary depth to include")),
		mcp.WithBoolean("include_parent_refs", mcp.Description("Include parent summary references")),
	)
}

func toolGrep() mcp.Tool {
	return mcp.NewTool(
		"amm_grep",
		mcp.WithDescription("Search events grouped by covering summary"),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Search pattern")),
		mcp.WithString("session_id", mcp.Description("Session identifier")),
		mcp.WithString("project_id", mcp.Description("Project identifier")),
		mcp.WithNumber("max_group_depth", mcp.Description("Summary grouping depth (0=shallowest)")),
		mcp.WithNumber("group_limit", mcp.Description("Maximum groups to return (default 10)")),
		mcp.WithNumber("matches_per_group", mcp.Description("Maximum matches per group (default 5)")),
	)
}
