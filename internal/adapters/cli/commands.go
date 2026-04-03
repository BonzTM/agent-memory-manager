package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	v1 "github.com/bonztm/agent-memory-manager/internal/contracts/v1"
	"github.com/bonztm/agent-memory-manager/internal/core"
)

type commandEntry struct {
	name        string
	aliases     []string
	runCLI      func(args []string) error
	runEnvelope func(ctx context.Context, svc core.Service, payload json.RawMessage) error
}

var commandRegistry = []commandEntry{
	{name: "init", aliases: []string{"init"}, runCLI: runInit, runEnvelope: runEnvelopeInit},
	{name: "ingest_event", aliases: []string{"ingest event"}, runCLI: runIngestEvent, runEnvelope: runEnvelopeIngestEvent},
	{name: "ingest_transcript", aliases: []string{"ingest transcript"}, runCLI: runIngestTranscript, runEnvelope: runEnvelopeIngestTranscript},
	{name: "remember", aliases: []string{"remember"}, runCLI: runRemember, runEnvelope: runEnvelopeRemember},
	{name: "recall", aliases: []string{"recall"}, runCLI: runRecall, runEnvelope: runEnvelopeRecall},
	{name: "describe", aliases: []string{"describe"}, runCLI: runDescribe, runEnvelope: runEnvelopeDescribe},
	{name: "expand", aliases: []string{"expand"}, runCLI: runExpand, runEnvelope: runEnvelopeExpand},
	{name: "format_context_window", aliases: []string{"context-window"}, runCLI: runContextWindow, runEnvelope: runEnvelopeFormatContextWindow},
	{name: "history", aliases: []string{"history"}, runCLI: runHistory, runEnvelope: runEnvelopeHistory},
	{name: "grep", aliases: []string{"grep"}, runCLI: runGrep, runEnvelope: runEnvelopeGrep},
	{name: "get_memory", aliases: []string{"memory", "memory show"}, runCLI: runGetMemory, runEnvelope: runEnvelopeGetMemory},
	{name: "update_memory", aliases: []string{"memory update"}, runCLI: runUpdateMemory, runEnvelope: runEnvelopeUpdateMemory},
	{name: "share", aliases: []string{"share"}, runCLI: runShare, runEnvelope: runEnvelopeShare},
	{name: "forget", aliases: []string{"forget"}, runCLI: runForget, runEnvelope: runEnvelopeForget},
	{name: "policy_list", aliases: []string{"policy list"}, runCLI: runPolicyList, runEnvelope: runEnvelopePolicyList},
	{name: "policy_add", aliases: []string{"policy add"}, runCLI: runPolicyAdd, runEnvelope: runEnvelopePolicyAdd},
	{name: "policy_remove", aliases: []string{"policy remove"}, runCLI: runPolicyRemove, runEnvelope: runEnvelopePolicyRemove},
	{name: "register_project", aliases: []string{"project add"}, runCLI: runProjectAdd, runEnvelope: runEnvelopeRegisterProject},
	{name: "get_project", aliases: []string{"project show"}, runCLI: runProjectShow, runEnvelope: runEnvelopeGetProject},
	{name: "list_projects", aliases: []string{"project list"}, runCLI: runProjectList, runEnvelope: runEnvelopeListProjects},
	{name: "remove_project", aliases: []string{"project remove"}, runCLI: runProjectRemove, runEnvelope: runEnvelopeRemoveProject},
	{name: "add_relationship", aliases: []string{"relationship add"}, runCLI: runRelationshipAdd, runEnvelope: runEnvelopeAddRelationship},
	{name: "get_relationship", aliases: []string{"relationship show"}, runCLI: runRelationshipShow, runEnvelope: runEnvelopeGetRelationship},
	{name: "list_relationships", aliases: []string{"relationship list"}, runCLI: runRelationshipList, runEnvelope: runEnvelopeListRelationships},
	{name: "remove_relationship", aliases: []string{"relationship remove"}, runCLI: runRelationshipRemove, runEnvelope: runEnvelopeRemoveRelationship},
	{name: "get_summary", aliases: []string{"summary", "summary show"}, runCLI: runSummaryShow, runEnvelope: runEnvelopeGetSummary},
	{name: "get_episode", aliases: []string{"episode", "episode show"}, runCLI: runEpisodeShow, runEnvelope: runEnvelopeGetEpisode},
	{name: "get_entity", aliases: []string{"entity", "entity show"}, runCLI: runEntityShow, runEnvelope: runEnvelopeGetEntity},
	{name: "run_job", aliases: []string{"jobs run"}, runCLI: runJob, runEnvelope: runEnvelopeRunJob},
	{name: "explain_recall", aliases: []string{"explain-recall"}, runCLI: runExplainRecall, runEnvelope: runEnvelopeExplainRecall},
	{name: "repair", aliases: []string{"repair"}, runCLI: runRepair, runEnvelope: runEnvelopeRepair},
	{name: "status", aliases: []string{"status"}, runCLI: runStatus, runEnvelope: runEnvelopeStatus},
	{name: "reset_derived", aliases: []string{"reset-derived"}, runCLI: runResetDerived, runEnvelope: runEnvelopeResetDerived},
}

func commandByName(name string) (commandEntry, bool) {
	for _, entry := range commandRegistry {
		if entry.name == name {
			return entry, true
		}
	}
	return commandEntry{}, false
}

func resolveTopLevelCommand(args []string) (commandEntry, []string, error) {
	if len(args) == 0 {
		return commandEntry{}, nil, fmt.Errorf("no command")
	}

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "init", "remember", "recall", "describe", "expand", "context-window", "history", "grep", "share", "forget", "explain-recall", "repair", "status", "reset-derived":
		for _, entry := range commandRegistry {
			for _, alias := range entry.aliases {
				if alias == cmd {
					return entry, cmdArgs, nil
				}
			}
		}
		return commandEntry{}, nil, fmt.Errorf("unknown command: %s", cmd)

	case "ingest":
		if len(cmdArgs) == 0 {
			return commandEntry{}, nil, fmt.Errorf("ingest requires a subcommand: event, transcript")
		}
		sub := "ingest " + cmdArgs[0]
		for _, entry := range commandRegistry {
			for _, alias := range entry.aliases {
				if alias == sub {
					return entry, cmdArgs[1:], nil
				}
			}
		}
		return commandEntry{}, nil, fmt.Errorf("unknown ingest subcommand: %s", cmdArgs[0])

	case "memory":
		if len(cmdArgs) > 0 {
			sub := "memory " + cmdArgs[0]
			for _, entry := range commandRegistry {
				for _, alias := range entry.aliases {
					if alias == sub {
						return entry, cmdArgs[1:], nil
					}
				}
			}
		}
		entry, _ := commandByName("get_memory")
		return entry, cmdArgs, nil

	case "policy":
		if len(cmdArgs) == 0 {
			return commandEntry{}, nil, fmt.Errorf("policy requires subcommand: list, add, remove")
		}
		sub := "policy " + cmdArgs[0]
		for _, entry := range commandRegistry {
			for _, alias := range entry.aliases {
				if alias == sub {
					return entry, cmdArgs[1:], nil
				}
			}
		}
		return commandEntry{}, nil, fmt.Errorf("unknown policy subcommand: %s", cmdArgs[0])

	case "project":
		if len(cmdArgs) == 0 {
			return commandEntry{}, nil, fmt.Errorf("project requires subcommand: add, show, list, remove")
		}
		sub := "project " + cmdArgs[0]
		for _, entry := range commandRegistry {
			for _, alias := range entry.aliases {
				if alias == sub {
					return entry, cmdArgs[1:], nil
				}
			}
		}
		return commandEntry{}, nil, fmt.Errorf("unknown project subcommand: %s", cmdArgs[0])

	case "relationship":
		if len(cmdArgs) == 0 {
			return commandEntry{}, nil, fmt.Errorf("relationship requires subcommand: add, show, list, remove")
		}
		sub := "relationship " + cmdArgs[0]
		for _, entry := range commandRegistry {
			for _, alias := range entry.aliases {
				if alias == sub {
					return entry, cmdArgs[1:], nil
				}
			}
		}
		return commandEntry{}, nil, fmt.Errorf("unknown relationship subcommand: %s", cmdArgs[0])

	case "summary", "episode", "entity":
		if len(cmdArgs) > 0 && cmdArgs[0] == "show" {
			for _, entry := range commandRegistry {
				for _, alias := range entry.aliases {
					if alias == cmd+" show" {
						return entry, cmdArgs[1:], nil
					}
				}
			}
		}
		for _, entry := range commandRegistry {
			for _, alias := range entry.aliases {
				if alias == cmd {
					return entry, cmdArgs, nil
				}
			}
		}
		return commandEntry{}, nil, fmt.Errorf("unknown command: %s", cmd)

	case "jobs":
		if len(cmdArgs) > 0 && cmdArgs[0] == "run" {
			entry, _ := commandByName("run_job")
			return entry, cmdArgs[1:], nil
		}
		return commandEntry{}, nil, fmt.Errorf("jobs requires subcommand: run")

	default:
		return commandEntry{}, nil, fmt.Errorf("unknown command: %s", cmd)
	}
}

func runEnvelopeInit(ctx context.Context, svc core.Service, _ json.RawMessage) error {
	status, err := svc.Status(ctx)
	if err != nil {
		fail("run", "INIT_ERROR", err.Error())
		return err
	}
	success("run", map[string]string{"status": "initialized", "db_path": status.DBPath})
	return nil
}

func runEnvelopeIngestEvent(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var evt core.Event
	if err := json.Unmarshal(payload, &evt); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	result, err := svc.IngestEvent(ctx, &evt)
	if err != nil {
		fail("run", "INGEST_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeIngestTranscript(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		Events []*core.Event `json:"events"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	slog.Debug("cli run envelope ingest_transcript", "event_count", len(req.Events))
	count, err := svc.IngestTranscript(ctx, req.Events)
	if err != nil {
		fail("run", "INGEST_ERROR", err.Error())
		return err
	}
	success("run", map[string]int{"ingested": count})
	return nil
}

func runEnvelopeRemember(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var mem core.Memory
	if err := json.Unmarshal(payload, &mem); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	result, err := svc.Remember(ctx, &mem)
	if err != nil {
		fail("run", "REMEMBER_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeRecall(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		Query string             `json:"query"`
		Opts  core.RecallOptions `json:"opts"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	if err := v1.ValidateRecall(&v1.RecallRequest{Query: req.Query, Mode: string(req.Opts.Mode), ProjectID: req.Opts.ProjectID, SessionID: req.Opts.SessionID, AgentID: req.Opts.AgentID, EntityIDs: req.Opts.EntityIDs, Limit: req.Opts.Limit, Explain: req.Opts.Explain, After: req.Opts.After, Before: req.Opts.Before}); err != nil {
		fail("run", "VALIDATION_ERROR", err.Error())
		return err
	}
	result, err := svc.Recall(ctx, req.Query, req.Opts)
	if err != nil {
		fail("run", "RECALL_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeDescribe(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	result, err := svc.Describe(ctx, req.IDs)
	if err != nil {
		fail("run", "DESCRIBE_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeExpand(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		ID              string `json:"id"`
		Kind            string `json:"kind"`
		SessionID       string `json:"session_id,omitempty"`
		DelegationDepth int    `json:"delegation_depth,omitempty"`
		MaxDepth        int    `json:"max_depth,omitempty"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	if req.Kind == "" {
		req.Kind = "memory"
	}
	if req.MaxDepth < 0 || req.MaxDepth > 5 {
		fail("run", "VALIDATION_ERROR", "max_depth must be between 0 and 5")
		return fmt.Errorf("max_depth must be between 0 and 5")
	}
	result, err := svc.Expand(ctx, req.ID, req.Kind, core.ExpandOptions{SessionID: req.SessionID, DelegationDepth: req.DelegationDepth, MaxDepth: req.MaxDepth})
	if err != nil {
		if errors.Is(err, core.ErrExpansionRecursionBlocked) {
			fail("run", "EXPANSION_RECURSION_BLOCKED", err.Error())
			return err
		}
		fail("run", "EXPAND_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeFormatContextWindow(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req v1.FormatContextWindowRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	if err := v1.ValidateFormatContextWindow(&req); err != nil {
		fail("run", "VALIDATION_ERROR", err.Error())
		return err
	}
	result, err := svc.FormatContextWindow(ctx, core.FormatContextWindowOptions{SessionID: req.SessionID, ProjectID: req.ProjectID, FreshTailCount: req.FreshTailCount, MaxSummaryDepth: req.MaxSummaryDepth, IncludeParentRefs: req.IncludeParentRefs})
	if err != nil {
		fail("run", "CONTEXT_WINDOW_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeHistory(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		Query string              `json:"query"`
		Opts  core.HistoryOptions `json:"opts"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	events, err := svc.History(ctx, req.Query, req.Opts)
	if err != nil {
		fail("run", "HISTORY_ERROR", err.Error())
		return err
	}
	success("run", map[string]interface{}{"events": events, "count": len(events)})
	return nil
}

func runEnvelopeGrep(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		Pattern string           `json:"pattern"`
		Opts    core.GrepOptions `json:"opts"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	result, err := svc.Grep(ctx, req.Pattern, req.Opts)
	if err != nil {
		fail("run", "GREP_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeGetMemory(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	mem, err := svc.GetMemory(ctx, req.ID)
	if err != nil {
		fail("run", "GET_ERROR", err.Error())
		return err
	}
	success("run", mem)
	return nil
}

func runEnvelopeUpdateMemory(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req v1.UpdateMemoryRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	if err := v1.ValidateUpdateMemory(&req); err != nil {
		fail("run", "VALIDATION_ERROR", err.Error())
		return err
	}
	mem, err := svc.GetMemory(ctx, req.ID)
	if err != nil {
		fail("run", "GET_ERROR", err.Error())
		return err
	}
	v1.ApplyMemoryUpdate(mem, req)
	updated, err := svc.UpdateMemory(ctx, mem)
	if err != nil {
		fail("run", "UPDATE_ERROR", err.Error())
		return err
	}
	success("run", updated)
	return nil
}

func runEnvelopeShare(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req v1.ShareRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	if err := v1.ValidateShare(req); err != nil {
		fail("run", "VALIDATION_ERROR", err.Error())
		return err
	}
	result, err := svc.ShareMemory(ctx, req.ID, core.PrivacyLevel(req.Privacy))
	if err != nil {
		fail("run", "SHARE_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeForget(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req v1.ForgetRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	if err := v1.ValidateForget(&req); err != nil {
		fail("run", "VALIDATION_ERROR", err.Error())
		return err
	}
	result, err := svc.ForgetMemory(ctx, req.ID)
	if err != nil {
		fail("run", "FORGET_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopePolicyList(ctx context.Context, svc core.Service, _ json.RawMessage) error {
	policies, err := svc.ListPolicies(ctx)
	if err != nil {
		fail("run", "LIST_ERROR", err.Error())
		return err
	}
	success("run", policies)
	return nil
}

func runEnvelopePolicyAdd(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var policy core.IngestionPolicy
	if err := json.Unmarshal(payload, &policy); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	result, err := svc.AddPolicy(ctx, &policy)
	if err != nil {
		fail("run", "ADD_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopePolicyRemove(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	if err := svc.RemovePolicy(ctx, req.ID); err != nil {
		fail("run", "REMOVE_ERROR", err.Error())
		return err
	}
	success("run", map[string]string{"id": req.ID, "status": "removed"})
	return nil
}

func runEnvelopeRegisterProject(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var project core.Project
	if err := json.Unmarshal(payload, &project); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	result, err := svc.RegisterProject(ctx, &project)
	if err != nil {
		fail("run", "ADD_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeGetProject(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	result, err := svc.GetProject(ctx, req.ID)
	if err != nil {
		fail("run", "GET_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeListProjects(ctx context.Context, svc core.Service, _ json.RawMessage) error {
	result, err := svc.ListProjects(ctx)
	if err != nil {
		fail("run", "LIST_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeRemoveProject(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	if err := svc.RemoveProject(ctx, req.ID); err != nil {
		fail("run", "REMOVE_ERROR", err.Error())
		return err
	}
	success("run", map[string]string{"id": req.ID, "status": "removed"})
	return nil
}

func runEnvelopeAddRelationship(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var rel core.Relationship
	if err := json.Unmarshal(payload, &rel); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	result, err := svc.AddRelationship(ctx, &rel)
	if err != nil {
		fail("run", "ADD_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeGetRelationship(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	result, err := svc.GetRelationship(ctx, req.ID)
	if err != nil {
		fail("run", "GET_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeListRelationships(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		EntityID         string `json:"entity_id,omitempty"`
		RelationshipType string `json:"relationship_type,omitempty"`
		Limit            int    `json:"limit,omitempty"`
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &req); err != nil {
			fail("run", "PARSE_ERROR", err.Error())
			return err
		}
	}
	result, err := svc.ListRelationships(ctx, core.ListRelationshipsOptions{EntityID: req.EntityID, RelationshipType: req.RelationshipType, Limit: req.Limit})
	if err != nil {
		fail("run", "LIST_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeRemoveRelationship(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	if err := svc.RemoveRelationship(ctx, req.ID); err != nil {
		fail("run", "REMOVE_ERROR", err.Error())
		return err
	}
	success("run", map[string]string{"id": req.ID, "status": "removed"})
	return nil
}

func runEnvelopeGetSummary(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	result, err := svc.GetSummary(ctx, req.ID)
	if err != nil {
		fail("run", "GET_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeGetEpisode(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	result, err := svc.GetEpisode(ctx, req.ID)
	if err != nil {
		fail("run", "GET_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeGetEntity(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	result, err := svc.GetEntity(ctx, req.ID)
	if err != nil {
		fail("run", "GET_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeRunJob(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	job, err := svc.RunJob(ctx, req.Kind)
	if err != nil {
		fail("run", "JOB_ERROR", err.Error())
		return err
	}
	success("run", job)
	return nil
}

func runEnvelopeExplainRecall(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		Query  string `json:"query"`
		ItemID string `json:"item_id"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	result, err := svc.ExplainRecall(ctx, req.Query, req.ItemID)
	if err != nil {
		fail("run", "EXPLAIN_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}

func runEnvelopeRepair(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req struct {
		Check bool   `json:"check"`
		Fix   string `json:"fix"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	report, err := svc.Repair(ctx, req.Check, req.Fix)
	if err != nil {
		fail("run", "REPAIR_ERROR", err.Error())
		return err
	}
	success("run", report)
	return nil
}

func runEnvelopeStatus(ctx context.Context, svc core.Service, _ json.RawMessage) error {
	status, err := svc.Status(ctx)
	if err != nil {
		fail("run", "STATUS_ERROR", err.Error())
		return err
	}
	success("run", status)
	return nil
}

func runEnvelopeResetDerived(ctx context.Context, svc core.Service, payload json.RawMessage) error {
	var req v1.ResetDerivedRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	if err := v1.ValidateResetDerived(&req); err != nil {
		fail("run", "VALIDATION_ERROR", err.Error())
		return err
	}
	result, err := svc.ResetDerived(ctx)
	if err != nil {
		fail("run", "RESET_DERIVED_ERROR", err.Error())
		return err
	}
	success("run", result)
	return nil
}
