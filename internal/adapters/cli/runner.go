package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	v1 "github.com/bonztm/agent-memory-manager/internal/contracts/v1"
	"github.com/bonztm/agent-memory-manager/internal/core"
	"github.com/bonztm/agent-memory-manager/internal/runtime"
)

// Version is set at build time via ldflags
var Version = "dev"

// Envelope is the standard JSON wrapper emitted by the CLI for successful and
// failed command execution.
type Envelope struct {
	OK        bool        `json:"ok"`
	Command   string      `json:"command"`
	Timestamp string      `json:"timestamp"`
	Result    interface{} `json:"result,omitempty"`
	Error     *EnvError   `json:"error,omitempty"`
}

// EnvError carries the machine-readable error payload embedded in an Envelope.
type EnvError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func logCLIError(msg string, err error, attrs ...any) {
	slog.Error(msg, append(attrs, "error", err)...)
}

func writeJSON(w io.Writer, env Envelope) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(env)
}

func success(cmd string, result interface{}) {
	writeJSON(os.Stdout, Envelope{
		OK:        true,
		Command:   cmd,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Result:    result,
	})
}

func fail(cmd string, code string, msg string) {
	writeJSON(os.Stderr, Envelope{
		OK:        false,
		Command:   cmd,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Error:     &EnvError{Code: code, Message: msg},
	})
}

// Run dispatches the amm CLI command named in args and writes any structured
// output envelopes to stdout or stderr.
func Run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	cmd := args[0]
	cmdArgs := args[1:]

	switch cmd {
	case "init":
		return runInit(cmdArgs)
	case "ingest":
		if len(cmdArgs) == 0 {
			return fmt.Errorf("ingest requires a subcommand: event, transcript")
		}
		switch cmdArgs[0] {
		case "event":
			return runIngestEvent(cmdArgs[1:])
		case "transcript":
			return runIngestTranscript(cmdArgs[1:])
		default:
			return fmt.Errorf("unknown ingest subcommand: %s", cmdArgs[0])
		}
	case "remember":
		return runRemember(cmdArgs)
	case "recall":
		return runRecall(cmdArgs)
	case "describe":
		return runDescribe(cmdArgs)
	case "expand":
		return runExpand(cmdArgs)
	case "history":
		return runHistory(cmdArgs)
	case "memory":
		if len(cmdArgs) > 0 {
			switch cmdArgs[0] {
			case "show":
				return runGetMemory(cmdArgs[1:])
			case "update":
				return runUpdateMemory(cmdArgs[1:])
			}
		}
		return runGetMemory(cmdArgs)
	case "policy":
		if len(cmdArgs) == 0 {
			return fmt.Errorf("policy requires subcommand: list, add, remove")
		}
		switch cmdArgs[0] {
		case "list":
			return runPolicyList(cmdArgs[1:])
		case "add":
			return runPolicyAdd(cmdArgs[1:])
		case "remove":
			return runPolicyRemove(cmdArgs[1:])
		default:
			return fmt.Errorf("unknown policy subcommand: %s", cmdArgs[0])
		}
	case "jobs":
		if len(cmdArgs) > 0 && cmdArgs[0] == "run" {
			return runJob(cmdArgs[1:])
		}
		return fmt.Errorf("jobs requires subcommand: run")
	case "explain-recall":
		return runExplainRecall(cmdArgs)
	case "repair":
		return runRepair(cmdArgs)
	case "status":
		return runStatus(cmdArgs)
	case "run":
		return runEnvelope(cmdArgs)
	case "validate":
		return validateEnvelope(cmdArgs)
	case "version", "--version", "-v":
		printVersion()
		return nil
	case "help", "--help", "-h":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

func getService() (core.Service, func(), error) {
	cfg := runtime.LoadConfigWithEnv()
	return runtime.NewService(cfg)
}

func parseFlags(args []string) map[string]string {
	flags := make(map[string]string)
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			key := strings.TrimPrefix(args[i], "--")
			if boolFlags[key] {
				flags[key] = "true"
			} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				flags[key] = args[i+1]
				i++
			} else {
				flags[key] = "true"
			}
		}
	}
	return flags
}

var boolFlags = map[string]bool{
	"json":  true,
	"check": true,
}

func positionalArgs(args []string) []string {
	var pos []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			key := strings.TrimPrefix(args[i], "--")
			if !boolFlags[key] && i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				i++
			}
			continue
		}
		pos = append(pos, args[i])
	}
	return pos
}

func printUsage() {
	fmt.Println(`amm - Agent Memory Manager

Usage: amm <command> [options]

Core Commands:
  init                    Initialize the database
  ingest event            Append a raw event
  ingest transcript       Bulk ingest events
  remember                Commit a durable memory
  recall                  Retrieve memories
  describe                Describe items by ID
  expand                  Expand an item to full detail
  history                 Query raw history
  memory [show] <id>      Show a memory
  memory update <id>      Update a memory
  policy list             List ingestion policies
  policy add              Add an ingestion policy
  policy remove <id>      Remove an ingestion policy
  jobs run <kind>         Run a maintenance job
  explain-recall          Explain why an item surfaced
  repair                  Run integrity checks/repairs
  status                  Show system status

Automation Commands:
  run --in <file>         Execute a full v1 command envelope
  validate --in <file>    Validate a command envelope

Info Commands:
  version, --version, -v  Show version
  help, --help, -h        Show this help

Environment:
  AMM_DB_PATH             Database path (default: ~/.amm/amm.db)

Use 'amm <command> --help' for command-specific flags.`)
}

func printVersion() {
	fmt.Printf("amm version %s\n", Version)
}

func runInit(args []string) error {
	flags := parseFlags(args)
	dbPath := flags["db"]

	// Parse --db flag first and propagate via environment so getService
	// opens the correct database, avoiding a redundant double-open.
	if dbPath != "" {
		os.Setenv("AMM_DB_PATH", dbPath)
	} else {
		dbPath = runtime.LoadConfigWithEnv().Storage.DBPath
	}
	slog.Debug("cli init start", "db_path", dbPath)

	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli init failed", err, "db_path", dbPath)
		fail("init", "INIT_ERROR", err.Error())
		return err
	}
	defer cleanup()

	// The factory already opens and migrates; just confirm status.
	ctx := context.Background()
	status, err := svc.Status(ctx)
	if err != nil {
		logCLIError("cli init failed", err, "db_path", dbPath)
		fail("init", "INIT_ERROR", err.Error())
		return err
	}
	slog.Debug("cli init succeeded", "db_path", status.DBPath)
	success("init", map[string]string{"status": "initialized", "db_path": status.DBPath})
	return nil
}

func runIngestEvent(args []string) error {
	flags := parseFlags(args)
	slog.Debug("cli ingest_event start")
	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli ingest_event failed", err)
		fail("ingest_event", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	var input io.Reader = os.Stdin
	if inFile := flags["in"]; inFile != "" && inFile != "-" {
		f, err := os.Open(inFile)
		if err != nil {
			logCLIError("cli ingest_event failed", err)
			fail("ingest_event", "FILE_ERROR", err.Error())
			return err
		}
		defer f.Close()
		input = f
	}

	var req v1.IngestEventRequest
	if err := json.NewDecoder(input).Decode(&req); err != nil {
		logCLIError("cli ingest_event failed", err)
		fail("ingest_event", "PARSE_ERROR", err.Error())
		return err
	}

	if err := v1.ValidateIngestEvent(&req); err != nil {
		logCLIError("cli ingest_event failed", err)
		fail("ingest_event", "VALIDATION_ERROR", err.Error())
		return err
	}

	event := &core.Event{
		Kind:         req.Kind,
		SourceSystem: req.SourceSystem,
		Surface:      req.Surface,
		SessionID:    req.SessionID,
		ProjectID:    req.ProjectID,
		AgentID:      req.AgentID,
		ActorType:    req.ActorType,
		ActorID:      req.ActorID,
		PrivacyLevel: core.PrivacyLevel(req.PrivacyLevel),
		Content:      req.Content,
		Metadata:     req.Metadata,
	}
	if req.OccurredAt != "" {
		t, err := time.Parse(time.RFC3339, req.OccurredAt)
		if err != nil {
			logCLIError("cli ingest_event failed", err, "event_id", event.ID)
			fail("ingest_event", "VALIDATION_ERROR", fmt.Sprintf("invalid occurred_at %q: %v", req.OccurredAt, err))
			return fmt.Errorf("invalid occurred_at: %w", err)
		}
		event.OccurredAt = t
	}

	ctx := context.Background()
	result, err := svc.IngestEvent(ctx, event)
	if err != nil {
		logCLIError("cli ingest_event failed", err, "event_id", event.ID)
		fail("ingest_event", "INGEST_ERROR", err.Error())
		return err
	}
	slog.Debug("cli ingest_event succeeded", "event_id", result.ID)
	success("ingest_event", result)
	return nil
}

func runIngestTranscript(args []string) error {
	flags := parseFlags(args)
	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli ingest_transcript failed", err)
		fail("ingest_transcript", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	var input io.Reader = os.Stdin
	if inFile := flags["in"]; inFile != "" && inFile != "-" {
		f, err := os.Open(inFile)
		if err != nil {
			logCLIError("cli ingest_transcript failed", err)
			fail("ingest_transcript", "FILE_ERROR", err.Error())
			return err
		}
		defer f.Close()
		input = f
	}

	// Support three input formats:
	// 1. Wrapped: {"events": [...]}
	// 2. Plain JSON array: [...]
	// 3. JSONL: one JSON object per line (streaming)
	data, err := io.ReadAll(input)
	if err != nil {
		logCLIError("cli ingest_transcript failed", err)
		fail("ingest_transcript", "READ_ERROR", err.Error())
		return err
	}

	var events []*core.Event

	// Try wrapped format {"events": [...]} first.
	var wrapped struct {
		Events []*core.Event `json:"events"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil && len(wrapped.Events) > 0 {
		events = wrapped.Events
	} else if err := json.Unmarshal(data, &events); err != nil {
		// Fall back to JSONL streaming.
		events = nil
		dec := json.NewDecoder(bytes.NewReader(data))
		for dec.More() {
			var evt core.Event
			if err := dec.Decode(&evt); err != nil {
				logCLIError("cli ingest_transcript failed", err)
				fail("ingest_transcript", "PARSE_ERROR", err.Error())
				return err
			}
			events = append(events, &evt)
		}
	}
	slog.Debug("cli ingest_transcript start", "event_count", len(events))

	ctx := context.Background()
	count, err := svc.IngestTranscript(ctx, events)
	if err != nil {
		logCLIError("cli ingest_transcript failed", err, "event_count", len(events))
		fail("ingest_transcript", "INGEST_ERROR", err.Error())
		return err
	}
	slog.Debug("cli ingest_transcript succeeded", "ingested", count)
	success("ingest_transcript", map[string]int{"ingested": count})
	return nil
}

func runRemember(args []string) error {
	flags := parseFlags(args)
	slog.Debug("cli remember start", "memory_type", flags["type"])
	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli remember failed", err, "memory_type", flags["type"])
		fail("remember", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	memory := &core.Memory{
		Type:             core.MemoryType(flags["type"]),
		Scope:            core.Scope(flags["scope"]),
		ProjectID:        flags["project"],
		Subject:          flags["subject"],
		Body:             flags["body"],
		TightDescription: flags["tight"],
	}

	if err := v1.ValidateRemember(&v1.RememberRequest{
		Type:             string(memory.Type),
		Scope:            string(memory.Scope),
		Body:             memory.Body,
		TightDescription: memory.TightDescription,
	}); err != nil {
		logCLIError("cli remember failed", err, "memory_type", memory.Type)
		fail("remember", "VALIDATION_ERROR", err.Error())
		return err
	}

	ctx := context.Background()
	result, err := svc.Remember(ctx, memory)
	if err != nil {
		logCLIError("cli remember failed", err, "memory_type", memory.Type)
		fail("remember", "REMEMBER_ERROR", err.Error())
		return err
	}
	slog.Debug("cli remember succeeded", "memory_type", memory.Type, "memory_id", result.ID)
	success("remember", result)
	return nil
}

func runRecall(args []string) error {
	flags := parseFlags(args)
	pos := positionalArgs(args)
	query := strings.Join(pos, " ")

	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli recall failed", err, "query", query)
		fail("recall", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	mode := core.RecallMode(flags["mode"])
	if mode == "" {
		mode = core.RecallModeHybrid
	}
	slog.Debug("cli recall start", "query", query, "mode", mode)

	opts := core.RecallOptions{
		Mode:      mode,
		ProjectID: flags["project"],
		SessionID: flags["session"],
	}

	ctx := context.Background()
	result, err := svc.Recall(ctx, query, opts)
	if err != nil {
		logCLIError("cli recall failed", err, "query", query, "mode", mode)
		fail("recall", "RECALL_ERROR", err.Error())
		return err
	}
	slog.Debug("cli recall succeeded", "query", query, "mode", mode, "result_count", len(result.Items))
	success("recall", result)
	return nil
}

func runDescribe(args []string) error {
	pos := positionalArgs(args)
	slog.Debug("cli describe start", "id_count", len(pos))
	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli describe failed", err, "id_count", len(pos))
		fail("describe", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	ctx := context.Background()
	results, err := svc.Describe(ctx, pos)
	if err != nil {
		logCLIError("cli describe failed", err, "id_count", len(pos))
		fail("describe", "DESCRIBE_ERROR", err.Error())
		return err
	}
	slog.Debug("cli describe succeeded", "id_count", len(pos), "result_count", len(results))
	success("describe", results)
	return nil
}

func runExpand(args []string) error {
	pos := positionalArgs(args)
	flags := parseFlags(args)

	if len(pos) == 0 {
		logCLIError("cli expand failed", fmt.Errorf("item ID required"))
		fail("expand", "VALIDATION_ERROR", "item ID required")
		return fmt.Errorf("item ID required")
	}

	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli expand failed", err, "id", pos[0])
		fail("expand", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	kind := flags["kind"]
	if kind == "" {
		// Infer kind from ID prefix
		id := pos[0]
		switch {
		case strings.HasPrefix(id, "mem_"):
			kind = "memory"
		case strings.HasPrefix(id, "sum_"):
			kind = "summary"
		case strings.HasPrefix(id, "ep_"):
			kind = "episode"
		default:
			kind = "memory"
		}
	}
	slog.Debug("cli expand start", "id", pos[0], "kind", kind)

	ctx := context.Background()
	result, err := svc.Expand(ctx, pos[0], kind)
	if err != nil {
		logCLIError("cli expand failed", err, "id", pos[0], "kind", kind)
		fail("expand", "EXPAND_ERROR", err.Error())
		return err
	}
	slog.Debug("cli expand succeeded", "id", pos[0], "kind", kind)
	success("expand", result)
	return nil
}

func runHistory(args []string) error {
	flags := parseFlags(args)
	pos := positionalArgs(args)
	query := strings.Join(pos, " ")

	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli history failed", err, "query", query)
		fail("history", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	slog.Debug("cli history start", "query", query)
	opts := core.HistoryOptions{
		SessionID: flags["session"],
		ProjectID: flags["project"],
	}

	ctx := context.Background()
	events, err := svc.History(ctx, query, opts)
	if err != nil {
		logCLIError("cli history failed", err, "query", query)
		fail("history", "HISTORY_ERROR", err.Error())
		return err
	}
	slog.Debug("cli history succeeded", "query", query, "result_count", len(events))
	success("history", map[string]interface{}{"events": events, "count": len(events)})
	return nil
}

func runGetMemory(args []string) error {
	pos := positionalArgs(args)
	if len(pos) == 0 {
		logCLIError("cli memory get failed", fmt.Errorf("memory ID required"))
		fail("memory", "VALIDATION_ERROR", "memory ID required")
		return fmt.Errorf("memory ID required")
	}
	slog.Debug("cli memory get start", "id", pos[0])

	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli memory get failed", err, "id", pos[0])
		fail("memory", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	ctx := context.Background()
	mem, err := svc.GetMemory(ctx, pos[0])
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			slog.Error("cli memory get not found", "id", pos[0], "found", false, "error", err)
		} else {
			logCLIError("cli memory get failed", err, "id", pos[0], "found", false)
		}
		fail("memory", "GET_ERROR", err.Error())
		return err
	}
	slog.Debug("cli memory get succeeded", "id", pos[0], "found", true)
	success("memory", mem)
	return nil
}

func runUpdateMemory(args []string) error {
	flags := parseFlags(args)
	pos := positionalArgs(args)
	if len(pos) == 0 {
		logCLIError("cli memory update failed", fmt.Errorf("memory ID required"))
		fail("memory_update", "VALIDATION_ERROR", "memory ID required")
		return fmt.Errorf("memory ID required")
	}
	slog.Debug("cli memory update start", "id", pos[0])

	if err := v1.ValidateUpdateMemory(&v1.UpdateMemoryRequest{
		ID:               pos[0],
		Body:             flags["body"],
		TightDescription: flags["tight"],
		Type:             flags["type"],
		Scope:            flags["scope"],
		Status:           flags["status"],
	}); err != nil {
		logCLIError("cli memory update failed", err, "id", pos[0])
		fail("memory_update", "VALIDATION_ERROR", err.Error())
		return err
	}

	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli memory update failed", err, "id", pos[0])
		fail("memory_update", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	ctx := context.Background()
	existing, err := svc.GetMemory(ctx, pos[0])
	if err != nil {
		logCLIError("cli memory update failed", err, "id", pos[0])
		fail("memory_update", "GET_ERROR", err.Error())
		return err
	}

	if v := flags["body"]; v != "" {
		existing.Body = v
	}
	if v := flags["tight"]; v != "" {
		existing.TightDescription = v
	}
	if v := flags["status"]; v != "" {
		existing.Status = core.MemoryStatus(v)
	}
	if v := flags["type"]; v != "" {
		existing.Type = core.MemoryType(v)
	}
	if v := flags["scope"]; v != "" {
		existing.Scope = core.Scope(v)
	}

	updated, err := svc.UpdateMemory(ctx, existing)
	if err != nil {
		logCLIError("cli memory update failed", err, "id", pos[0])
		fail("memory_update", "UPDATE_ERROR", err.Error())
		return err
	}
	slog.Debug("cli memory update succeeded", "id", pos[0])
	success("memory_update", updated)
	return nil
}

func runPolicyList(_ []string) error {
	slog.Debug("cli policy_list start")
	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli policy_list failed", err)
		fail("policy_list", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	ctx := context.Background()
	policies, err := svc.ListPolicies(ctx)
	if err != nil {
		logCLIError("cli policy_list failed", err)
		fail("policy_list", "LIST_ERROR", err.Error())
		return err
	}
	slog.Debug("cli policy_list succeeded", "result_count", len(policies))
	success("policy_list", policies)
	return nil
}

func runPolicyAdd(args []string) error {
	flags := parseFlags(args)
	slog.Debug("cli policy_add start", "pattern", flags["pattern"])
	priority := 0
	if raw := strings.TrimSpace(flags["priority"]); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			logCLIError("cli policy_add failed", err, "priority", raw)
			fail("policy_add", "VALIDATION_ERROR", "priority must be an integer")
			return fmt.Errorf("priority must be an integer")
		}
		priority = parsed
	}

	req := &v1.PolicyAddRequest{
		PatternType: flags["pattern-type"],
		Pattern:     flags["pattern"],
		Mode:        flags["mode"],
		Priority:    priority,
		MatchMode:   flags["match-mode"],
	}
	if err := v1.ValidatePolicyAdd(req); err != nil {
		logCLIError("cli policy_add failed", err, "pattern", req.Pattern)
		fail("policy_add", "VALIDATION_ERROR", err.Error())
		return err
	}

	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli policy_add failed", err, "pattern", req.Pattern)
		fail("policy_add", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	ctx := context.Background()
	result, err := svc.AddPolicy(ctx, &core.IngestionPolicy{
		PatternType: req.PatternType,
		Pattern:     req.Pattern,
		Mode:        req.Mode,
		Priority:    req.Priority,
		MatchMode:   req.MatchMode,
	})
	if err != nil {
		logCLIError("cli policy_add failed", err, "pattern", req.Pattern)
		fail("policy_add", "ADD_ERROR", err.Error())
		return err
	}
	slog.Debug("cli policy_add succeeded", "pattern", req.Pattern, "policy_id", result.ID)
	success("policy_add", result)
	return nil
}

func runPolicyRemove(args []string) error {
	pos := positionalArgs(args)
	if len(pos) == 0 {
		logCLIError("cli policy_remove failed", fmt.Errorf("policy ID required"))
		fail("policy_remove", "VALIDATION_ERROR", "policy ID required")
		return fmt.Errorf("policy ID required")
	}
	slog.Debug("cli policy_remove start", "id", pos[0])

	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli policy_remove failed", err, "id", pos[0])
		fail("policy_remove", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	ctx := context.Background()
	if err := svc.RemovePolicy(ctx, pos[0]); err != nil {
		logCLIError("cli policy_remove failed", err, "id", pos[0])
		fail("policy_remove", "REMOVE_ERROR", err.Error())
		return err
	}
	slog.Debug("cli policy_remove succeeded", "id", pos[0])
	success("policy_remove", map[string]string{"id": pos[0], "status": "removed"})
	return nil
}

func runJob(args []string) error {
	flags := parseFlags(args)
	pos := positionalArgs(args)

	if len(pos) > 0 && (flags["reprocess"] == "true" || flags["reprocess-all"] == "true") {
		logCLIError("cli jobs_run failed", fmt.Errorf("cannot combine positional job kind with --reprocess/--reprocess-all"))
		fail("jobs_run", "VALIDATION_ERROR", "cannot combine positional job kind with --reprocess/--reprocess-all")
		return fmt.Errorf("cannot combine positional job kind with --reprocess/--reprocess-all")
	}

	kind := ""
	if len(pos) > 0 {
		kind = pos[0]
	} else {
		reprocess := flags["reprocess"] == "true"
		reprocessAll := flags["reprocess-all"] == "true"
		switch {
		case reprocess && reprocessAll:
			logCLIError("cli jobs_run failed", fmt.Errorf("cannot pass both --reprocess and --reprocess-all"))
			fail("jobs_run", "VALIDATION_ERROR", "cannot pass both --reprocess and --reprocess-all")
			return fmt.Errorf("cannot pass both --reprocess and --reprocess-all")
		case reprocess:
			kind = "reprocess"
		case reprocessAll:
			kind = "reprocess_all"
		}
	}

	if kind == "" {
		logCLIError("cli jobs_run failed", fmt.Errorf("job kind required"))
		fail("jobs_run", "VALIDATION_ERROR", "job kind required")
		return fmt.Errorf("job kind required")
	}
	slog.Debug("cli jobs_run start", "kind", kind)

	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli jobs_run failed", err, "kind", kind)
		fail("jobs_run", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	ctx := context.Background()
	job, err := svc.RunJob(ctx, kind)
	if err != nil {
		logCLIError("cli jobs_run failed", err, "kind", kind)
		fail("jobs_run", "JOB_ERROR", err.Error())
		return err
	}
	slog.Debug("cli jobs_run succeeded", "kind", kind, "job_id", job.ID)
	success("jobs_run", job)
	return nil
}

func runExplainRecall(args []string) error {
	flags := parseFlags(args)
	slog.Debug("cli explain_recall start", "query", flags["query"], "item_id", flags["item-id"])

	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli explain_recall failed", err, "query", flags["query"], "item_id", flags["item-id"])
		fail("explain_recall", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	ctx := context.Background()
	result, err := svc.ExplainRecall(ctx, flags["query"], flags["item-id"])
	if err != nil {
		logCLIError("cli explain_recall failed", err, "query", flags["query"], "item_id", flags["item-id"])
		fail("explain_recall", "EXPLAIN_ERROR", err.Error())
		return err
	}
	slog.Debug("cli explain_recall succeeded", "query", flags["query"], "item_id", flags["item-id"])
	success("explain_recall", result)
	return nil
}

func runRepair(args []string) error {
	flags := parseFlags(args)
	check := flags["check"] == "true"
	fix := flags["fix"]
	slog.Debug("cli repair start", "check", check, "fix", fix)

	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli repair failed", err, "check", check, "fix", fix)
		fail("repair", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	ctx := context.Background()
	report, err := svc.Repair(ctx, check, fix)
	if err != nil {
		logCLIError("cli repair failed", err, "check", check, "fix", fix)
		fail("repair", "REPAIR_ERROR", err.Error())
		return err
	}
	slog.Debug("cli repair succeeded", "check", check, "fix", fix, "checked", report.Checked, "issues", report.Issues, "fixed", report.Fixed)
	success("repair", report)
	return nil
}

func runStatus(_ []string) error {
	configuredDBPath := runtime.LoadConfigWithEnv().Storage.DBPath
	slog.Debug("cli status start", "db_path", configuredDBPath)
	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli status failed", err, "db_path", configuredDBPath)
		fail("status", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	ctx := context.Background()
	status, err := svc.Status(ctx)
	if err != nil {
		logCLIError("cli status failed", err, "db_path", configuredDBPath)
		fail("status", "STATUS_ERROR", err.Error())
		return err
	}
	slog.Debug("cli status succeeded", "db_path", status.DBPath, "event_count", status.EventCount, "memory_count", status.MemoryCount, "summary_count", status.SummaryCount, "episode_count", status.EpisodeCount, "entity_count", status.EntityCount)
	success("status", status)
	return nil
}

// CommandEnvelope is the full amm.v1 command envelope accepted by the run and
// validate automation commands.
type CommandEnvelope struct {
	Version   string          `json:"version"`
	Command   string          `json:"command"`
	RequestID string          `json:"request_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

func runEnvelope(args []string) error {
	flags := parseFlags(args)
	inPath := flags["in"]
	if inPath == "" {
		inPath = "-"
	}

	var input io.Reader = os.Stdin
	if inPath != "-" {
		f, err := os.Open(inPath)
		if err != nil {
			logCLIError("cli run envelope failed", err)
			fail("run", "FILE_ERROR", err.Error())
			return err
		}
		defer f.Close()
		input = f
	}

	data, err := io.ReadAll(input)
	if err != nil {
		logCLIError("cli run envelope failed", err)
		fail("run", "READ_ERROR", err.Error())
		return err
	}

	var envelope CommandEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		logCLIError("cli run envelope failed", err)
		fail("run", "PARSE_ERROR", err.Error())
		return err
	}
	slog.Debug("cli run envelope start", "command", envelope.Command)

	if envelope.Version != "amm.v1" {
		logCLIError("cli run envelope failed", fmt.Errorf("invalid version: %s", envelope.Version), "command", envelope.Command)
		fail("run", "VERSION_ERROR", fmt.Sprintf("expected version 'amm.v1', got '%s'", envelope.Version))
		return fmt.Errorf("invalid version: %s", envelope.Version)
	}

	svc, cleanup, err := getService()
	if err != nil {
		logCLIError("cli run envelope failed", err, "command", envelope.Command)
		fail("run", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	ctx := context.Background()
	if err := dispatchEnvelope(ctx, svc, envelope); err != nil {
		// dispatchEnvelope already called fail() with user-facing JSON output.
		// Only log at debug level to avoid triple-reporting (fail + logCLIError + caller).
		slog.Debug("cli run envelope dispatch error", "command", envelope.Command, "error", err)
		return err
	}
	slog.Debug("cli run envelope succeeded", "command", envelope.Command)
	return nil
}

func validateEnvelope(args []string) error {
	flags := parseFlags(args)
	inPath := flags["in"]
	if inPath == "" {
		inPath = "-"
	}

	var input io.Reader = os.Stdin
	if inPath != "-" {
		f, err := os.Open(inPath)
		if err != nil {
			logCLIError("cli validate envelope failed", err)
			fail("validate", "FILE_ERROR", err.Error())
			return err
		}
		defer f.Close()
		input = f
	}

	data, err := io.ReadAll(input)
	if err != nil {
		logCLIError("cli validate envelope failed", err)
		fail("validate", "READ_ERROR", err.Error())
		return err
	}

	var envelope CommandEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		logCLIError("cli validate envelope failed", err)
		fail("validate", "PARSE_ERROR", err.Error())
		return err
	}
	slog.Debug("cli validate envelope start", "command", envelope.Command)

	if envelope.Version != "amm.v1" {
		logCLIError("cli validate envelope failed", fmt.Errorf("invalid version: %s", envelope.Version), "command", envelope.Command)
		fail("validate", "VERSION_ERROR", fmt.Sprintf("expected version 'amm.v1', got '%s'", envelope.Version))
		return fmt.Errorf("invalid version: %s", envelope.Version)
	}

	if _, ok := v1.CommandRegistry[envelope.Command]; !ok {
		logCLIError("cli validate envelope failed", fmt.Errorf("unknown command: %s", envelope.Command), "command", envelope.Command)
		fail("validate", "UNKNOWN_COMMAND", fmt.Sprintf("unknown command: %s", envelope.Command))
		return fmt.Errorf("unknown command: %s", envelope.Command)
	}
	slog.Debug("cli validate envelope succeeded", "command", envelope.Command, "valid", true)

	success("validate", map[string]interface{}{
		"valid":   true,
		"command": envelope.Command,
		"version": envelope.Version,
	})
	return nil
}

func dispatchEnvelope(ctx context.Context, svc core.Service, envelope CommandEnvelope) error {
	switch envelope.Command {
	case "init":
		status, err := svc.Status(ctx)
		if err != nil {
			fail("run", "INIT_ERROR", err.Error())
			return err
		}
		success("run", map[string]string{"status": "initialized", "db_path": status.DBPath})

	case "ingest_event":
		var evt core.Event
		if err := json.Unmarshal(envelope.Payload, &evt); err != nil {
			fail("run", "PARSE_ERROR", err.Error())
			return err
		}
		result, err := svc.IngestEvent(ctx, &evt)
		if err != nil {
			fail("run", "INGEST_ERROR", err.Error())
			return err
		}
		success("run", result)

	case "ingest_transcript":
		var payload struct {
			Events []*core.Event `json:"events"`
		}
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			fail("run", "PARSE_ERROR", err.Error())
			return err
		}
		slog.Debug("cli run envelope ingest_transcript", "event_count", len(payload.Events))
		count, err := svc.IngestTranscript(ctx, payload.Events)
		if err != nil {
			fail("run", "INGEST_ERROR", err.Error())
			return err
		}
		success("run", map[string]int{"ingested": count})

	case "remember":
		var mem core.Memory
		if err := json.Unmarshal(envelope.Payload, &mem); err != nil {
			fail("run", "PARSE_ERROR", err.Error())
			return err
		}
		result, err := svc.Remember(ctx, &mem)
		if err != nil {
			fail("run", "REMEMBER_ERROR", err.Error())
			return err
		}
		success("run", result)

	case "recall":
		var payload struct {
			Query string             `json:"query"`
			Opts  core.RecallOptions `json:"opts"`
		}
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			fail("run", "PARSE_ERROR", err.Error())
			return err
		}
		result, err := svc.Recall(ctx, payload.Query, payload.Opts)
		if err != nil {
			fail("run", "RECALL_ERROR", err.Error())
			return err
		}
		success("run", result)

	case "describe":
		var payload struct {
			IDs []string `json:"ids"`
		}
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			fail("run", "PARSE_ERROR", err.Error())
			return err
		}
		results, err := svc.Describe(ctx, payload.IDs)
		if err != nil {
			fail("run", "DESCRIBE_ERROR", err.Error())
			return err
		}
		success("run", results)

	case "expand":
		var payload struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			fail("run", "PARSE_ERROR", err.Error())
			return err
		}
		result, err := svc.Expand(ctx, payload.ID, payload.Kind)
		if err != nil {
			fail("run", "EXPAND_ERROR", err.Error())
			return err
		}
		success("run", result)

	case "history":
		var payload struct {
			Query string              `json:"query"`
			Opts  core.HistoryOptions `json:"opts"`
		}
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			fail("run", "PARSE_ERROR", err.Error())
			return err
		}
		events, err := svc.History(ctx, payload.Query, payload.Opts)
		if err != nil {
			fail("run", "HISTORY_ERROR", err.Error())
			return err
		}
		success("run", map[string]interface{}{"events": events, "count": len(events)})

	case "get_memory":
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			fail("run", "PARSE_ERROR", err.Error())
			return err
		}
		mem, err := svc.GetMemory(ctx, payload.ID)
		if err != nil {
			fail("run", "GET_ERROR", err.Error())
			return err
		}
		success("run", mem)

	case "update_memory":
		var mem core.Memory
		if err := json.Unmarshal(envelope.Payload, &mem); err != nil {
			fail("run", "PARSE_ERROR", err.Error())
			return err
		}
		updated, err := svc.UpdateMemory(ctx, &mem)
		if err != nil {
			fail("run", "UPDATE_ERROR", err.Error())
			return err
		}
		success("run", updated)

	case "policy_list":
		policies, err := svc.ListPolicies(ctx)
		if err != nil {
			fail("run", "LIST_ERROR", err.Error())
			return err
		}
		success("run", policies)

	case "policy_add":
		var policy core.IngestionPolicy
		if err := json.Unmarshal(envelope.Payload, &policy); err != nil {
			fail("run", "PARSE_ERROR", err.Error())
			return err
		}
		result, err := svc.AddPolicy(ctx, &policy)
		if err != nil {
			fail("run", "ADD_ERROR", err.Error())
			return err
		}
		success("run", result)

	case "policy_remove":
		var payload struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			fail("run", "PARSE_ERROR", err.Error())
			return err
		}
		if err := svc.RemovePolicy(ctx, payload.ID); err != nil {
			fail("run", "REMOVE_ERROR", err.Error())
			return err
		}
		success("run", map[string]string{"id": payload.ID, "status": "removed"})

	case "run_job":
		var payload struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			fail("run", "PARSE_ERROR", err.Error())
			return err
		}
		job, err := svc.RunJob(ctx, payload.Kind)
		if err != nil {
			fail("run", "JOB_ERROR", err.Error())
			return err
		}
		success("run", job)

	case "explain_recall":
		var payload struct {
			Query  string `json:"query"`
			ItemID string `json:"item_id"`
		}
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			fail("run", "PARSE_ERROR", err.Error())
			return err
		}
		result, err := svc.ExplainRecall(ctx, payload.Query, payload.ItemID)
		if err != nil {
			fail("run", "EXPLAIN_ERROR", err.Error())
			return err
		}
		success("run", result)

	case "repair":
		var payload struct {
			Check bool   `json:"check"`
			Fix   string `json:"fix"`
		}
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			fail("run", "PARSE_ERROR", err.Error())
			return err
		}
		report, err := svc.Repair(ctx, payload.Check, payload.Fix)
		if err != nil {
			fail("run", "REPAIR_ERROR", err.Error())
			return err
		}
		success("run", report)

	case "status":
		status, err := svc.Status(ctx)
		if err != nil {
			fail("run", "STATUS_ERROR", err.Error())
			return err
		}
		success("run", status)

	default:
		fail("run", "UNKNOWN_COMMAND", fmt.Sprintf("unknown command: %s", envelope.Command))
		return fmt.Errorf("unknown command: %s", envelope.Command)
	}

	return nil
}
