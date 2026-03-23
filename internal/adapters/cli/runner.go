package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/joshd-04/agent-memory-manager/internal/core"
	v1 "github.com/joshd-04/agent-memory-manager/internal/contracts/v1"
	"github.com/joshd-04/agent-memory-manager/internal/runtime"
)

// Envelope is the standard JSON output wrapper.
type Envelope struct {
	OK        bool        `json:"ok"`
	Command   string      `json:"command"`
	Timestamp string      `json:"timestamp"`
	Result    interface{} `json:"result,omitempty"`
	Error     *EnvError   `json:"error,omitempty"`
}

// EnvError carries error info in the envelope.
type EnvError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
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

// Run is the main CLI entrypoint.
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
		if len(cmdArgs) > 0 && cmdArgs[0] == "show" {
			return runGetMemory(cmdArgs[1:])
		}
		return runGetMemory(cmdArgs)
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
	case "help", "--help", "-h":
		printUsage()
		return nil
	default:
		return fmt.Errorf("unknown command: %s", cmd)
	}
}

func getService() (core.Service, func(), error) {
	cfg := runtime.DefaultConfig()
	cfg = runtime.ConfigFromEnv(cfg)

	// Check for --db flag in a simple way
	dbPath := os.Getenv("AMM_DB_PATH")
	if dbPath != "" {
		cfg.Storage.DBPath = dbPath
	}

	return runtime.NewService(cfg)
}

func parseFlags(args []string) map[string]string {
	flags := make(map[string]string)
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			key := strings.TrimPrefix(args[i], "--")
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				flags[key] = args[i+1]
				i++
			} else {
				flags[key] = "true"
			}
		}
	}
	return flags
}

func positionalArgs(args []string) []string {
	var pos []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") {
			i++ // skip value
			continue
		}
		pos = append(pos, args[i])
	}
	return pos
}

func printUsage() {
	fmt.Println(`amm - Agent Memory Manager

Usage: amm <command> [options]

Commands:
  init                    Initialize the database
  ingest event            Append a raw event
  ingest transcript       Bulk ingest events
  remember                Commit a durable memory
  recall                  Retrieve memories
  describe                Describe items
  expand                  Expand an item
  history                 Query raw history
  memory [show] <id>      Show a memory
  jobs run <kind>         Run a maintenance job
  explain-recall          Explain why something surfaced
  repair                  Run integrity checks/repairs
  status                  Show system status

Environment:
  AMM_DB_PATH             Database path (default: ~/.amm/amm.db)`)
}

func runInit(args []string) error {
	flags := parseFlags(args)

	// Parse --db flag first and propagate via environment so getService
	// opens the correct database, avoiding a redundant double-open.
	if dbPath := flags["db"]; dbPath != "" {
		os.Setenv("AMM_DB_PATH", dbPath)
	}

	svc, cleanup, err := getService()
	if err != nil {
		fail("init", "INIT_ERROR", err.Error())
		return err
	}
	defer cleanup()

	// The factory already opens and migrates; just confirm status.
	ctx := context.Background()
	status, err := svc.Status(ctx)
	if err != nil {
		fail("init", "INIT_ERROR", err.Error())
		return err
	}
	success("init", map[string]string{"status": "initialized", "db_path": status.DBPath})
	return nil
}

func runIngestEvent(args []string) error {
	flags := parseFlags(args)
	svc, cleanup, err := getService()
	if err != nil {
		fail("ingest_event", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	var input io.Reader = os.Stdin
	if inFile := flags["in"]; inFile != "" && inFile != "-" {
		f, err := os.Open(inFile)
		if err != nil {
			fail("ingest_event", "FILE_ERROR", err.Error())
			return err
		}
		defer f.Close()
		input = f
	}

	var req v1.IngestEventRequest
	if err := json.NewDecoder(input).Decode(&req); err != nil {
		fail("ingest_event", "PARSE_ERROR", err.Error())
		return err
	}

	if err := v1.ValidateIngestEvent(&req); err != nil {
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
			fail("ingest_event", "VALIDATION_ERROR", fmt.Sprintf("invalid occurred_at %q: %v", req.OccurredAt, err))
			return fmt.Errorf("invalid occurred_at: %w", err)
		}
		event.OccurredAt = t
	}

	ctx := context.Background()
	result, err := svc.IngestEvent(ctx, event)
	if err != nil {
		fail("ingest_event", "INGEST_ERROR", err.Error())
		return err
	}
	success("ingest_event", result)
	return nil
}

func runIngestTranscript(args []string) error {
	flags := parseFlags(args)
	svc, cleanup, err := getService()
	if err != nil {
		fail("ingest_transcript", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	var input io.Reader = os.Stdin
	if inFile := flags["in"]; inFile != "" && inFile != "-" {
		f, err := os.Open(inFile)
		if err != nil {
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
				fail("ingest_transcript", "PARSE_ERROR", err.Error())
				return err
			}
			events = append(events, &evt)
		}
	}

	ctx := context.Background()
	count, err := svc.IngestTranscript(ctx, events)
	if err != nil {
		fail("ingest_transcript", "INGEST_ERROR", err.Error())
		return err
	}
	success("ingest_transcript", map[string]int{"ingested": count})
	return nil
}

func runRemember(args []string) error {
	flags := parseFlags(args)
	svc, cleanup, err := getService()
	if err != nil {
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
		fail("remember", "VALIDATION_ERROR", err.Error())
		return err
	}

	ctx := context.Background()
	result, err := svc.Remember(ctx, memory)
	if err != nil {
		fail("remember", "REMEMBER_ERROR", err.Error())
		return err
	}
	success("remember", result)
	return nil
}

func runRecall(args []string) error {
	flags := parseFlags(args)
	pos := positionalArgs(args)

	svc, cleanup, err := getService()
	if err != nil {
		fail("recall", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	query := strings.Join(pos, " ")
	mode := core.RecallMode(flags["mode"])
	if mode == "" {
		mode = core.RecallModeHybrid
	}

	opts := core.RecallOptions{
		Mode:      mode,
		ProjectID: flags["project"],
		SessionID: flags["session"],
	}

	ctx := context.Background()
	result, err := svc.Recall(ctx, query, opts)
	if err != nil {
		fail("recall", "RECALL_ERROR", err.Error())
		return err
	}
	success("recall", result)
	return nil
}

func runDescribe(args []string) error {
	pos := positionalArgs(args)
	svc, cleanup, err := getService()
	if err != nil {
		fail("describe", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	ctx := context.Background()
	results, err := svc.Describe(ctx, pos)
	if err != nil {
		fail("describe", "DESCRIBE_ERROR", err.Error())
		return err
	}
	success("describe", results)
	return nil
}

func runExpand(args []string) error {
	pos := positionalArgs(args)
	flags := parseFlags(args)

	if len(pos) == 0 {
		fail("expand", "VALIDATION_ERROR", "item ID required")
		return fmt.Errorf("item ID required")
	}

	svc, cleanup, err := getService()
	if err != nil {
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

	ctx := context.Background()
	result, err := svc.Expand(ctx, pos[0], kind)
	if err != nil {
		fail("expand", "EXPAND_ERROR", err.Error())
		return err
	}
	success("expand", result)
	return nil
}

func runHistory(args []string) error {
	flags := parseFlags(args)
	pos := positionalArgs(args)

	svc, cleanup, err := getService()
	if err != nil {
		fail("history", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	query := strings.Join(pos, " ")
	opts := core.HistoryOptions{
		SessionID: flags["session"],
		ProjectID: flags["project"],
	}

	ctx := context.Background()
	events, err := svc.History(ctx, query, opts)
	if err != nil {
		fail("history", "HISTORY_ERROR", err.Error())
		return err
	}
	success("history", map[string]interface{}{"events": events, "count": len(events)})
	return nil
}

func runGetMemory(args []string) error {
	pos := positionalArgs(args)
	if len(pos) == 0 {
		fail("memory", "VALIDATION_ERROR", "memory ID required")
		return fmt.Errorf("memory ID required")
	}

	svc, cleanup, err := getService()
	if err != nil {
		fail("memory", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	ctx := context.Background()
	mem, err := svc.GetMemory(ctx, pos[0])
	if err != nil {
		fail("memory", "GET_ERROR", err.Error())
		return err
	}
	success("memory", mem)
	return nil
}

func runJob(args []string) error {
	pos := positionalArgs(args)
	if len(pos) == 0 {
		fail("jobs_run", "VALIDATION_ERROR", "job kind required")
		return fmt.Errorf("job kind required")
	}

	svc, cleanup, err := getService()
	if err != nil {
		fail("jobs_run", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	ctx := context.Background()
	job, err := svc.RunJob(ctx, pos[0])
	if err != nil {
		fail("jobs_run", "JOB_ERROR", err.Error())
		return err
	}
	success("jobs_run", job)
	return nil
}

func runExplainRecall(args []string) error {
	flags := parseFlags(args)

	svc, cleanup, err := getService()
	if err != nil {
		fail("explain_recall", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	ctx := context.Background()
	result, err := svc.ExplainRecall(ctx, flags["query"], flags["item-id"])
	if err != nil {
		fail("explain_recall", "EXPLAIN_ERROR", err.Error())
		return err
	}
	success("explain_recall", result)
	return nil
}

func runRepair(args []string) error {
	flags := parseFlags(args)

	svc, cleanup, err := getService()
	if err != nil {
		fail("repair", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	check := flags["check"] == "true"
	fix := flags["fix"]

	ctx := context.Background()
	report, err := svc.Repair(ctx, check, fix)
	if err != nil {
		fail("repair", "REPAIR_ERROR", err.Error())
		return err
	}
	success("repair", report)
	return nil
}

func runStatus(_ []string) error {
	svc, cleanup, err := getService()
	if err != nil {
		fail("status", "SERVICE_ERROR", err.Error())
		return err
	}
	defer cleanup()

	ctx := context.Background()
	status, err := svc.Status(ctx)
	if err != nil {
		fail("status", "STATUS_ERROR", err.Error())
		return err
	}
	success("status", status)
	return nil
}
