package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	v1 "github.com/bonztm/agent-memory-manager/internal/contracts/v1"
	"github.com/bonztm/agent-memory-manager/internal/core"
)

func captureRunWithStdin(t *testing.T, args []string, stdin string) (string, string, error) {
	t.Helper()

	origStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	if _, err := w.Write([]byte(stdin)); err != nil {
		_ = r.Close()
		_ = w.Close()
		t.Fatalf("write stdin: %v", err)
	}
	if err := w.Close(); err != nil {
		_ = r.Close()
		t.Fatalf("close stdin writer: %v", err)
	}
	os.Stdin = r
	defer func() {
		os.Stdin = origStdin
		_ = r.Close()
	}()

	return captureRun(t, args)
}

func mustRawJSON(t *testing.T, payload any) json.RawMessage {
	t.Helper()
	if payload == nil {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return data
}

func writeEnvelopeFile(t *testing.T, env CommandEnvelope) string {
	t.Helper()
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	path := filepath.Join(t.TempDir(), "envelope.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write envelope: %v", err)
	}
	return path
}

func setBadDBPath(t *testing.T) {
	t.Helper()
	if err := os.Setenv("AMM_DB_PATH", t.TempDir()); err != nil {
		t.Fatalf("set bad db path: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("AMM_DB_PATH")
	})
}

func assertEnvelopeErrorCode(t *testing.T, raw string, wantCommand string, wantCode string) map[string]interface{} {
	t.Helper()
	env := assertEnvelope(t, raw, false, wantCommand)
	errMap, ok := env["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing error payload: %v", env)
	}
	if got := errMap["code"]; got != wantCode {
		t.Fatalf("error code = %v, want %s; env=%v", got, wantCode, env)
	}
	return env
}

func runEnvelopeFile(t *testing.T, env CommandEnvelope) map[string]interface{} {
	t.Helper()
	path := writeEnvelopeFile(t, env)
	stdout, stderr, err := captureRun(t, []string{"run", "--in", path})
	if err != nil {
		t.Fatalf("run envelope %s error: %v stderr=%s", env.Command, err, stderr)
	}
	if stderr != "" {
		t.Fatalf("run envelope %s unexpected stderr: %s", env.Command, stderr)
	}
	return assertEnvelope(t, stdout, true, "run")
}

func TestPrintVersion(t *testing.T) {
	oldVersion := Version
	Version = "test-build"
	defer func() { Version = oldVersion }()

	stdout, stderr, err := captureRun(t, []string{"version"})
	if err != nil {
		t.Fatalf("version error: %v stderr=%s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("expected empty stderr, got %q", stderr)
	}
	if got := strings.TrimSpace(stdout); got != "amm version test-build" {
		t.Fatalf("version output = %q, want %q", got, "amm version test-build")
	}
}

func TestValidateEnvelope(t *testing.T) {
	t.Run("valid envelope from stdin", func(t *testing.T) {
		stdout, stderr, err := captureRunWithStdin(t, []string{"validate"}, `{"version":"amm.v1","command":"status","request_id":"req-validate"}`)
		if err != nil {
			t.Fatalf("validate error: %v stderr=%s", err, stderr)
		}
		env := assertEnvelope(t, stdout, true, "validate")
		result, _ := env["result"].(map[string]interface{})
		if result["valid"] != true {
			t.Fatalf("expected valid=true, got %v", result["valid"])
		}
		if result["command"] != "status" {
			t.Fatalf("expected command status, got %v", result["command"])
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		_, stderr, err := captureRunWithStdin(t, []string{"validate"}, `{`)
		if err == nil {
			t.Fatal("expected validate parse error")
		}
		assertEnvelopeErrorCode(t, stderr, "validate", "PARSE_ERROR")
	})

	t.Run("missing version", func(t *testing.T) {
		_, stderr, err := captureRunWithStdin(t, []string{"validate"}, `{"command":"status"}`)
		if err == nil {
			t.Fatal("expected validate version error")
		}
		assertEnvelopeErrorCode(t, stderr, "validate", "VERSION_ERROR")
	})

	t.Run("missing command", func(t *testing.T) {
		_, stderr, err := captureRunWithStdin(t, []string{"validate"}, `{"version":"amm.v1"}`)
		if err == nil {
			t.Fatal("expected validate unknown command error")
		}
		assertEnvelopeErrorCode(t, stderr, "validate", "UNKNOWN_COMMAND")
	})

	t.Run("unknown command", func(t *testing.T) {
		_, stderr, err := captureRunWithStdin(t, []string{"validate"}, `{"version":"amm.v1","command":"nope_command"}`)
		if err == nil {
			t.Fatal("expected validate unknown command error")
		}
		assertEnvelopeErrorCode(t, stderr, "validate", "UNKNOWN_COMMAND")
	})

	t.Run("missing input file", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing.json")
		_, stderr, err := captureRun(t, []string{"validate", "--in", missing})
		if err == nil {
			t.Fatal("expected validate file error")
		}
		assertEnvelopeErrorCode(t, stderr, "validate", "FILE_ERROR")
	})
}

func TestRunEnvelopeErrors(t *testing.T) {
	setTempDBPath(t)

	t.Run("missing input file", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing.json")
		_, stderr, err := captureRun(t, []string{"run", "--in", missing})
		if err == nil {
			t.Fatal("expected run file error")
		}
		assertEnvelopeErrorCode(t, stderr, "run", "FILE_ERROR")
	})

	t.Run("invalid json", func(t *testing.T) {
		_, stderr, err := captureRunWithStdin(t, []string{"run"}, `{`)
		if err == nil {
			t.Fatal("expected run parse error")
		}
		assertEnvelopeErrorCode(t, stderr, "run", "PARSE_ERROR")
	})

	t.Run("invalid version", func(t *testing.T) {
		_, stderr, err := captureRunWithStdin(t, []string{"run"}, `{"version":"amm.v0","command":"status"}`)
		if err == nil {
			t.Fatal("expected run version error")
		}
		assertEnvelopeErrorCode(t, stderr, "run", "VERSION_ERROR")
	})

	t.Run("unknown command", func(t *testing.T) {
		_, stderr, err := captureRunWithStdin(t, []string{"run"}, `{"version":"amm.v1","command":"nope_command"}`)
		if err == nil {
			t.Fatal("expected run unknown command error")
		}
		assertEnvelopeErrorCode(t, stderr, "run", "UNKNOWN_COMMAND")
	})

	t.Run("share validation error", func(t *testing.T) {
		_, stderr, err := captureRunWithStdin(t, []string{"run"}, `{"version":"amm.v1","command":"share","payload":{"id":"mem_123","privacy":"invalid"}}`)
		if err == nil {
			t.Fatal("expected run share validation error")
		}
		assertEnvelopeErrorCode(t, stderr, "run", "VALIDATION_ERROR")
	})

	t.Run("reset-derived validation error", func(t *testing.T) {
		_, stderr, err := captureRunWithStdin(t, []string{"run"}, `{"version":"amm.v1","command":"reset_derived","payload":{"confirm":false}}`)
		if err == nil {
			t.Fatal("expected run reset-derived validation error")
		}
		assertEnvelopeErrorCode(t, stderr, "run", "VALIDATION_ERROR")
	})
}

func TestRunEnvelopeDispatchAllCommands(t *testing.T) {
	setTempDBPath(t)

	stdout, stderr, err := captureRunWithStdin(t, []string{"run"}, `{"version":"amm.v1","command":"init","request_id":"req-init"}`)
	if err != nil {
		t.Fatalf("run init envelope error: %v stderr=%s", err, stderr)
	}
	initEnv := assertEnvelope(t, stdout, true, "run")
	initResult, _ := initEnv["result"].(map[string]interface{})
	if initResult["status"] != "initialized" {
		t.Fatalf("expected initialized result, got %v", initResult)
	}

	runEnvelopeFile(t, CommandEnvelope{
		Version: "amm.v1",
		Command: v1.CmdIngestEvent,
		Payload: mustRawJSON(t, core.Event{
			Kind:         "message_user",
			SourceSystem: "test",
			ProjectID:    "proj-history",
			SessionID:    "sess-history",
			Content:      "envelope history event",
		}),
	})

	runEnvelopeFile(t, CommandEnvelope{
		Version: "amm.v1",
		Command: v1.CmdIngestTranscript,
		Payload: mustRawJSON(t, map[string]any{
			"events": []core.Event{{
				Kind:         "message_assistant",
				SourceSystem: "test",
				Content:      "envelope transcript event",
			}},
		}),
	})

	rememberEnv := runEnvelopeFile(t, CommandEnvelope{
		Version:   "amm.v1",
		Command:   v1.CmdRemember,
		RequestID: "req-remember",
		Payload: mustRawJSON(t, core.Memory{
			Type:             core.MemoryTypeFact,
			Scope:            core.ScopeProject,
			ProjectID:        "proj-env",
			AgentID:          "agent-env",
			Subject:          "subject-env",
			Body:             "envelope memory body",
			TightDescription: "envelope memory tight",
		}),
	})
	rememberResult, _ := rememberEnv["result"].(map[string]interface{})
	memoryID, _ := rememberResult["id"].(string)
	if memoryID == "" {
		t.Fatalf("missing memory id from remember result: %v", rememberResult)
	}

	recallEnv := runEnvelopeFile(t, CommandEnvelope{
		Version:   "amm.v1",
		Command:   v1.CmdRecall,
		RequestID: "req-recall",
		Payload: mustRawJSON(t, map[string]any{
			"query": "envelope memory",
			"opts": core.RecallOptions{
				Mode:      core.RecallModeFacts,
				ProjectID: "proj-env",
				AgentID:   "agent-env",
				Limit:     5,
				Explain:   true,
			},
		}),
	})
	recallResult, _ := recallEnv["result"].(map[string]interface{})
	items, _ := recallResult["items"].([]interface{})
	if len(items) == 0 {
		t.Fatalf("expected recall results, got %v", recallResult)
	}

	describeEnv := runEnvelopeFile(t, CommandEnvelope{
		Version: "amm.v1",
		Command: v1.CmdDescribe,
		Payload: mustRawJSON(t, map[string]any{"ids": []string{memoryID}}),
	})
	if result, ok := describeEnv["result"].([]interface{}); !ok || len(result) != 1 {
		t.Fatalf("expected one describe result, got %v", describeEnv["result"])
	}

	expandEnv := runEnvelopeFile(t, CommandEnvelope{
		Version: "amm.v1",
		Command: v1.CmdExpand,
		Payload: mustRawJSON(t, map[string]any{"id": memoryID, "session_id": "sess-expand"}),
	})
	expandResult, _ := expandEnv["result"].(map[string]interface{})
	if _, ok := expandResult["memory"].(map[string]interface{}); !ok {
		t.Fatalf("expected expanded memory payload, got %v", expandResult)
	}

	historyEnv := runEnvelopeFile(t, CommandEnvelope{
		Version: "amm.v1",
		Command: v1.CmdHistory,
		Payload: mustRawJSON(t, map[string]any{
			"query": "envelope",
			"opts":  core.HistoryOptions{ProjectID: "proj-history", SessionID: "sess-history", Limit: 10},
		}),
	})
	historyResult, _ := historyEnv["result"].(map[string]interface{})
	events, _ := historyResult["events"].([]interface{})
	if len(events) == 0 {
		t.Fatalf("expected history events, got %v", historyResult)
	}

	getMemoryEnv := runEnvelopeFile(t, CommandEnvelope{
		Version: "amm.v1",
		Command: v1.CmdGetMemory,
		Payload: mustRawJSON(t, map[string]any{"id": memoryID}),
	})
	getMemoryResult, _ := getMemoryEnv["result"].(map[string]interface{})
	if getMemoryResult["id"] != memoryID {
		t.Fatalf("expected memory id %q, got %v", memoryID, getMemoryResult["id"])
	}

	updateEnv := runEnvelopeFile(t, CommandEnvelope{
		Version: "amm.v1",
		Command: v1.CmdUpdateMemory,
		Payload: mustRawJSON(t, map[string]any{
			"id":     memoryID,
			"body":   "updated envelope memory body",
			"status": string(core.MemoryStatusArchived),
		}),
	})
	updateResult, _ := updateEnv["result"].(map[string]interface{})
	if updateResult["status"] != "archived" {
		t.Fatalf("expected archived status, got %v", updateResult["status"])
	}
	if updateResult["type"] != string(core.MemoryTypeFact) {
		t.Fatalf("expected type to remain %q, got %v", core.MemoryTypeFact, updateResult["type"])
	}
	if updateResult["tight_description"] != "envelope memory tight" {
		t.Fatalf("expected tight_description to remain unchanged, got %v", updateResult["tight_description"])
	}

	shareEnv := runEnvelopeFile(t, CommandEnvelope{
		Version: "amm.v1",
		Command: v1.CmdShare,
		Payload: mustRawJSON(t, v1.ShareRequest{ID: memoryID, Privacy: string(core.PrivacyShared)}),
	})
	shareResult, _ := shareEnv["result"].(map[string]interface{})
	if shareResult["privacy_level"] != "shared" {
		t.Fatalf("expected shared privacy level, got %v", shareResult["privacy_level"])
	}

	explainEnv := runEnvelopeFile(t, CommandEnvelope{
		Version: "amm.v1",
		Command: v1.CmdExplainRecall,
		Payload: mustRawJSON(t, map[string]any{"query": "updated envelope memory", "item_id": memoryID}),
	})
	explainResult, _ := explainEnv["result"].(map[string]interface{})
	if explainResult["item_id"] != memoryID {
		t.Fatalf("expected explain item_id %q, got %v", memoryID, explainResult["item_id"])
	}

	policyAddEnv := runEnvelopeFile(t, CommandEnvelope{
		Version: "amm.v1",
		Command: v1.CmdPolicyAdd,
		Payload: mustRawJSON(t, core.IngestionPolicy{
			PatternType: "source",
			Pattern:     "env-*",
			Mode:        "full",
			Priority:    7,
			MatchMode:   "glob",
		}),
	})
	policyAddResult, _ := policyAddEnv["result"].(map[string]interface{})
	policyID, _ := policyAddResult["id"].(string)
	if policyID == "" {
		t.Fatalf("missing policy id from result: %v", policyAddResult)
	}

	policyListEnv := runEnvelopeFile(t, CommandEnvelope{Version: "amm.v1", Command: v1.CmdPolicyList})
	if result, ok := policyListEnv["result"].([]interface{}); !ok || len(result) == 0 {
		t.Fatalf("expected listed policies, got %v", policyListEnv["result"])
	}

	policyRemoveEnv := runEnvelopeFile(t, CommandEnvelope{
		Version: "amm.v1",
		Command: v1.CmdPolicyRemove,
		Payload: mustRawJSON(t, map[string]any{"id": policyID}),
	})
	policyRemoveResult, _ := policyRemoveEnv["result"].(map[string]interface{})
	if policyRemoveResult["status"] != "removed" {
		t.Fatalf("expected removed status, got %v", policyRemoveResult["status"])
	}

	runJobEnv := runEnvelopeFile(t, CommandEnvelope{
		Version: "amm.v1",
		Command: v1.CmdRunJob,
		Payload: mustRawJSON(t, map[string]any{"kind": "reflect"}),
	})
	runJobResult, _ := runJobEnv["result"].(map[string]interface{})
	if runJobResult["kind"] != "reflect" {
		t.Fatalf("expected reflect job, got %v", runJobResult["kind"])
	}

	repairEnv := runEnvelopeFile(t, CommandEnvelope{
		Version: "amm.v1",
		Command: v1.CmdRepair,
		Payload: mustRawJSON(t, map[string]any{"check": true}),
	})
	repairResult, _ := repairEnv["result"].(map[string]interface{})
	if _, ok := repairResult["checked"]; !ok {
		t.Fatalf("expected repair result, got %v", repairResult)
	}

	statusEnv := runEnvelopeFile(t, CommandEnvelope{Version: "amm.v1", Command: v1.CmdStatus})
	statusResult, _ := statusEnv["result"].(map[string]interface{})
	if statusResult["initialized"] != true {
		t.Fatalf("expected initialized status result, got %v", statusResult)
	}

	forgetEnv := runEnvelopeFile(t, CommandEnvelope{
		Version: "amm.v1",
		Command: v1.CmdForget,
		Payload: mustRawJSON(t, v1.ForgetRequest{ID: memoryID}),
	})
	forgetResult, _ := forgetEnv["result"].(map[string]interface{})
	if forgetResult["status"] != "retracted" {
		t.Fatalf("expected retracted status, got %v", forgetResult["status"])
	}

	resetEnv := runEnvelopeFile(t, CommandEnvelope{
		Version: "amm.v1",
		Command: v1.CmdResetDerived,
		Payload: mustRawJSON(t, v1.ResetDerivedRequest{Confirm: true}),
	})
	resetResult, _ := resetEnv["result"].(map[string]interface{})
	if _, ok := resetResult["events_reset"]; !ok {
		t.Fatalf("expected reset-derived result, got %v", resetResult)
	}
}

func TestAdditionalLowCoverageCommandBranches(t *testing.T) {
	t.Run("policy add with priority and match mode", func(t *testing.T) {
		setTempDBPath(t)
		stdout, stderr, err := captureRun(t, []string{"policy", "add", "--pattern-type", "source", "--pattern", "prio-*", "--mode", "full", "--priority", "7", "--match-mode", "glob"})
		if err != nil {
			t.Fatalf("policy add error: %v stderr=%s", err, stderr)
		}
		result := decodeEnvelopeResult(t, stdout)
		if result["priority"] != float64(7) {
			t.Fatalf("expected priority 7, got %v", result["priority"])
		}
		if result["match_mode"] != "glob" {
			t.Fatalf("expected match_mode glob, got %v", result["match_mode"])
		}
	})

	t.Run("policy add invalid priority", func(t *testing.T) {
		setTempDBPath(t)
		_, stderr, err := captureRun(t, []string{"policy", "add", "--pattern-type", "source", "--pattern", "prio-*", "--mode", "full", "--priority", "bad"})
		if err == nil {
			t.Fatal("expected policy add invalid priority error")
		}
		assertEnvelopeErrorCode(t, stderr, "policy_add", "VALIDATION_ERROR")
	})

	t.Run("share missing id", func(t *testing.T) {
		setTempDBPath(t)
		_, stderr, err := captureRun(t, []string{"share", "--privacy", "shared"})
		if err == nil {
			t.Fatal("expected share missing id error")
		}
		assertEnvelopeErrorCode(t, stderr, "share", "VALIDATION_ERROR")
	})

	t.Run("share service error", func(t *testing.T) {
		setBadDBPath(t)
		_, stderr, err := captureRun(t, []string{"share", "mem_any", "--privacy", "shared"})
		if err == nil {
			t.Fatal("expected share service error")
		}
		assertEnvelopeErrorCode(t, stderr, "share", "SERVICE_ERROR")
	})

	t.Run("forget service error", func(t *testing.T) {
		setBadDBPath(t)
		_, stderr, err := captureRun(t, []string{"forget", "mem_any"})
		if err == nil {
			t.Fatal("expected forget service error")
		}
		assertEnvelopeErrorCode(t, stderr, "forget", "SERVICE_ERROR")
	})

	t.Run("policy list service error", func(t *testing.T) {
		setBadDBPath(t)
		_, stderr, err := captureRun(t, []string{"policy", "list"})
		if err == nil {
			t.Fatal("expected policy list service error")
		}
		assertEnvelopeErrorCode(t, stderr, "policy_list", "SERVICE_ERROR")
	})

	t.Run("reset-derived service error", func(t *testing.T) {
		setBadDBPath(t)
		_, stderr, err := captureRun(t, []string{"reset-derived", "--confirm"})
		if err == nil {
			t.Fatal("expected reset-derived service error")
		}
		assertEnvelopeErrorCode(t, stderr, "reset_derived", "SERVICE_ERROR")
	})
}
