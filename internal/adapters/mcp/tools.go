package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	v1 "github.com/bonztm/agent-memory-manager/internal/contracts/v1"
	"github.com/bonztm/agent-memory-manager/internal/core"
)

type toolEntry struct {
	tool    Tool
	handler func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error)
}

type invalidToolArgsError struct {
	err error
}

func (e invalidToolArgsError) Error() string {
	return e.err.Error()
}

func (e invalidToolArgsError) Unwrap() error {
	return e.err
}

func invalidToolArgs(err error) error {
	if err == nil {
		return nil
	}
	return invalidToolArgsError{err: err}
}

var errUnknownTool = errors.New("unknown tool")

var toolRegistry = []toolEntry{
	{
		tool: Tool{Name: "amm_init", Description: "Initialize the amm database", InputSchema: emptySchema()},
		handler: func(ctx context.Context, svc core.Service, _ json.RawMessage) (interface{}, error) {
			status, err := svc.Status(ctx)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"message": "already initialized", "status": status}, nil
		},
	},
	{
		tool: Tool{Name: "amm_ingest_event", Description: "Append a raw event to history", InputSchema: eventSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var evt core.Event
			if err := json.Unmarshal(args, &evt); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.IngestEvent(ctx, &evt)
		},
	},
	{
		tool: Tool{Name: "amm_remember", Description: "Commit a durable memory", InputSchema: rememberSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var mem core.Memory
			if err := json.Unmarshal(args, &mem); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.Remember(ctx, &mem)
		},
	},
	{
		tool: Tool{Name: "amm_recall", Description: "Retrieve memories using various modes", InputSchema: recallSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				Query   string             `json:"query"`
				AgentID string             `json:"agent_id"`
				Explain bool               `json:"explain"`
				Opts    core.RecallOptions `json:"opts"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			if req.Opts.AgentID == "" && req.AgentID != "" {
				req.Opts.AgentID = req.AgentID
			}
			if req.Explain {
				req.Opts.Explain = true
			}
			if err := v1.ValidateRecall(&v1.RecallRequest{
				Query:     req.Query,
				Mode:      string(req.Opts.Mode),
				ProjectID: req.Opts.ProjectID,
				SessionID: req.Opts.SessionID,
				AgentID:   req.Opts.AgentID,
				EntityIDs: req.Opts.EntityIDs,
				Limit:     req.Opts.Limit,
				Explain:   req.Opts.Explain,
				After:     req.Opts.After,
				Before:    req.Opts.Before,
			}); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.Recall(ctx, req.Query, req.Opts)
		},
	},
	{
		tool: Tool{Name: "amm_describe", Description: "Get thin descriptions of items", InputSchema: describeSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				IDs []string `json:"ids"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.Describe(ctx, req.IDs)
		},
	},
	{
		tool: Tool{Name: "amm_expand", Description: "Expand an item to full detail", InputSchema: expandSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				ID              string `json:"id"`
				Kind            string `json:"kind"`
				SessionID       string `json:"session_id,omitempty"`
				DelegationDepth int    `json:"delegation_depth,omitempty"`
				MaxDepth        int    `json:"max_depth,omitempty"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			if req.Kind == "" {
				req.Kind = "memory"
			}
			if req.DelegationDepth < 0 {
				return nil, invalidToolArgs(fmt.Errorf("delegation_depth must be non-negative"))
			}
			if req.MaxDepth < 0 || req.MaxDepth > 5 {
				return nil, invalidToolArgs(fmt.Errorf("max_depth must be between 0 and 5"))
			}
			return svc.Expand(ctx, req.ID, req.Kind, core.ExpandOptions{SessionID: req.SessionID, DelegationDepth: req.DelegationDepth, MaxDepth: req.MaxDepth})
		},
	},
	{
		tool: Tool{Name: "amm_format_context_window", Description: "Assemble deterministic context from summaries and fresh events", InputSchema: formatContextWindowSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req v1.FormatContextWindowRequest
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			if err := v1.ValidateFormatContextWindow(&req); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.FormatContextWindow(ctx, core.FormatContextWindowOptions{
				SessionID:         req.SessionID,
				ProjectID:         req.ProjectID,
				FreshTailCount:    req.FreshTailCount,
				MaxSummaryDepth:   req.MaxSummaryDepth,
				IncludeParentRefs: req.IncludeParentRefs,
			})
		},
	},
	{
		tool: Tool{Name: "amm_history", Description: "Query raw interaction history", InputSchema: historySchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				Query string              `json:"query"`
				Opts  core.HistoryOptions `json:"opts"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.History(ctx, req.Query, req.Opts)
		},
	},
	{
		tool: Tool{Name: "amm_grep", Description: "Search events grouped by covering summary", InputSchema: grepSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req v1.GrepRequest
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			if err := v1.ValidateGrep(&req); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.Grep(ctx, req.Pattern, core.GrepOptions{
				SessionID:       req.SessionID,
				ProjectID:       req.ProjectID,
				MaxGroupDepth:   req.MaxGroupDepth,
				GroupLimit:      req.GroupLimit,
				MatchesPerGroup: req.MatchesPerGroup,
			})
		},
	},
	{
		tool: Tool{Name: "amm_get_memory", Description: "Get a single memory by ID", InputSchema: idSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.GetMemory(ctx, req.ID)
		},
	},
	{
		tool: Tool{Name: "amm_jobs_run", Description: "Run a maintenance job", InputSchema: jobSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				Kind string `json:"kind"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.RunJob(ctx, req.Kind)
		},
	},
	{
		tool: Tool{Name: "amm_share", Description: "Update a memory privacy level", InputSchema: shareSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req v1.ShareRequest
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			if err := v1.ValidateShare(req); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.ShareMemory(ctx, req.ID, core.PrivacyLevel(req.Privacy))
		},
	},
	{
		tool: Tool{Name: "amm_explain_recall", Description: "Explain why an item surfaced", InputSchema: explainSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				Query  string `json:"query"`
				ItemID string `json:"item_id"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.ExplainRecall(ctx, req.Query, req.ItemID)
		},
	},
	{
		tool: Tool{Name: "amm_repair", Description: "Run integrity checks and repairs", InputSchema: repairSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				Check bool   `json:"check"`
				Fix   string `json:"fix"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.Repair(ctx, req.Check, req.Fix)
		},
	},
	{
		tool: Tool{Name: "amm_status", Description: "Get system status", InputSchema: emptySchema()},
		handler: func(ctx context.Context, svc core.Service, _ json.RawMessage) (interface{}, error) {
			return svc.Status(ctx)
		},
	},
	{
		tool: Tool{Name: "amm_ingest_transcript", Description: "Bulk ingest a sequence of events", InputSchema: transcriptSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				Events []*core.Event `json:"events"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.IngestTranscript(ctx, req.Events)
		},
	},
	{
		tool: Tool{Name: "amm_update_memory", Description: "Update an existing memory", InputSchema: updateMemorySchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req v1.UpdateMemoryRequest
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			if err := v1.ValidateUpdateMemory(&req); err != nil {
				return nil, invalidToolArgs(err)
			}
			mem, err := svc.GetMemory(ctx, req.ID)
			if err != nil {
				return nil, err
			}
			v1.ApplyMemoryUpdate(mem, req)
			return svc.UpdateMemory(ctx, mem)
		},
	},
	{
		tool: Tool{Name: "amm_policy_list", Description: "List all ingestion policies", InputSchema: policyListSchema()},
		handler: func(ctx context.Context, svc core.Service, _ json.RawMessage) (interface{}, error) {
			return svc.ListPolicies(ctx)
		},
	},
	{
		tool: Tool{Name: "amm_policy_add", Description: "Add an ingestion policy", InputSchema: policyAddSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var policy core.IngestionPolicy
			if err := json.Unmarshal(args, &policy); err != nil {
				return nil, invalidToolArgs(err)
			}
			if err := v1.ValidatePolicyAdd(&v1.PolicyAddRequest{
				PatternType: policy.PatternType,
				Pattern:     policy.Pattern,
				Mode:        policy.Mode,
				Priority:    policy.Priority,
				MatchMode:   policy.MatchMode,
				Metadata:    policy.Metadata,
			}); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.AddPolicy(ctx, &policy)
		},
	},
	{
		tool: Tool{Name: "amm_policy_remove", Description: "Remove an ingestion policy by ID", InputSchema: policyRemoveSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			if err := svc.RemovePolicy(ctx, req.ID); err != nil {
				return nil, err
			}
			return map[string]string{"id": req.ID, "status": "removed"}, nil
		},
	},
	{
		tool: Tool{Name: "amm_register_project", Description: "Register a new project", InputSchema: projectSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var project core.Project
			if err := json.Unmarshal(args, &project); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.RegisterProject(ctx, &project)
		},
	},
	{
		tool: Tool{Name: "amm_get_project", Description: "Get a project by ID", InputSchema: idSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.GetProject(ctx, req.ID)
		},
	},
	{
		tool: Tool{Name: "amm_list_projects", Description: "List all projects", InputSchema: emptySchema()},
		handler: func(ctx context.Context, svc core.Service, _ json.RawMessage) (interface{}, error) {
			return svc.ListProjects(ctx)
		},
	},
	{
		tool: Tool{Name: "amm_remove_project", Description: "Remove a project by ID", InputSchema: idSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			if err := svc.RemoveProject(ctx, req.ID); err != nil {
				return nil, err
			}
			return map[string]string{"id": req.ID, "status": "removed"}, nil
		},
	},
	{
		tool: Tool{Name: "amm_add_relationship", Description: "Add an entity relationship", InputSchema: addRelationshipSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var rel core.Relationship
			if err := json.Unmarshal(args, &rel); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.AddRelationship(ctx, &rel)
		},
	},
	{
		tool: Tool{Name: "amm_get_relationship", Description: "Get a relationship by ID", InputSchema: idSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.GetRelationship(ctx, req.ID)
		},
	},
	{
		tool: Tool{Name: "amm_list_relationships", Description: "List relationships", InputSchema: listRelationshipsSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				EntityID         string `json:"entity_id"`
				RelationshipType string `json:"relationship_type"`
				Limit            int    `json:"limit"`
			}
			_ = json.Unmarshal(args, &req)
			return svc.ListRelationships(ctx, core.ListRelationshipsOptions{
				EntityID:         req.EntityID,
				RelationshipType: req.RelationshipType,
				Limit:            req.Limit,
			})
		},
	},
	{
		tool: Tool{Name: "amm_remove_relationship", Description: "Remove a relationship by ID", InputSchema: idSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			if err := svc.RemoveRelationship(ctx, req.ID); err != nil {
				return nil, err
			}
			return map[string]string{"id": req.ID, "status": "removed"}, nil
		},
	},
	{
		tool: Tool{Name: "amm_get_summary", Description: "Get a summary by ID", InputSchema: idSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.GetSummary(ctx, req.ID)
		},
	},
	{
		tool: Tool{Name: "amm_get_episode", Description: "Get an episode by ID", InputSchema: idSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.GetEpisode(ctx, req.ID)
		},
	},
	{
		tool: Tool{Name: "amm_get_entity", Description: "Get an entity by ID", InputSchema: idSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.GetEntity(ctx, req.ID)
		},
	},
	{
		tool: Tool{Name: "amm_forget", Description: "Forget (retract) a memory by ID", InputSchema: idSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req v1.ForgetRequest
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			if err := v1.ValidateForget(&req); err != nil {
				return nil, invalidToolArgs(err)
			}
			return svc.ForgetMemory(ctx, req.ID)
		},
	},
	{
		tool: Tool{Name: "amm_reset_derived", Description: "Purge all derived data while preserving events", InputSchema: resetDerivedSchema()},
		handler: func(ctx context.Context, svc core.Service, args json.RawMessage) (interface{}, error) {
			var req struct {
				Confirm bool `json:"confirm"`
			}
			if err := json.Unmarshal(args, &req); err != nil {
				return nil, invalidToolArgs(err)
			}
			if !req.Confirm {
				return nil, fmt.Errorf("confirm must be true to reset derived data")
			}
			return svc.ResetDerived(ctx)
		},
	},
}

func tools() []Tool {
	out := make([]Tool, 0, len(toolRegistry))
	for _, entry := range toolRegistry {
		out = append(out, entry.tool)
	}
	return out
}

func dispatchTool(ctx context.Context, svc core.Service, name string, args json.RawMessage) (interface{}, error) {
	for _, entry := range toolRegistry {
		if entry.tool.Name == name {
			return entry.handler(ctx, svc, args)
		}
	}
	return nil, fmt.Errorf("%w: %s", errUnknownTool, name)
}
