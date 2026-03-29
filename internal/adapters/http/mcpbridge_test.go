package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

func callBridgeTool(t *testing.T, srv *mcpserver.MCPServer, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	tool := srv.GetTool(name)
	if tool == nil {
		t.Fatalf("tool %q not registered", name)
	}

	result, err := tool.Handler(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{Name: name, Arguments: args},
	})
	if err != nil {
		t.Fatalf("tool %q call err: %v", name, err)
	}
	if result == nil {
		t.Fatalf("tool %q returned nil result", name)
	}
	return result
}

func toolResultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatalf("expected text content")
	}
	text, ok := mcp.AsTextContent(result.Content[0])
	if !ok {
		t.Fatalf("expected text content, got %T", result.Content[0])
	}
	return text.Text
}

func decodeToolResult[T any](t *testing.T, result *mcp.CallToolResult) T {
	t.Helper()
	var out T
	if err := json.Unmarshal([]byte(toolResultText(t, result)), &out); err != nil {
		t.Fatalf("decode tool result: %v", err)
	}
	return out
}

func mustRememberViaTool(t *testing.T, bridge *mcpserver.MCPServer, body string) core.Memory {
	t.Helper()
	rememberResult := callBridgeTool(t, bridge, "amm_remember", map[string]any{
		"type":              string(core.MemoryTypeFact),
		"scope":             string(core.ScopeGlobal),
		"body":              body,
		"tight_description": body,
	})
	if rememberResult.IsError {
		t.Fatalf("remember tool returned error: %s", toolResultText(t, rememberResult))
	}
	return decodeToolResult[core.Memory](t, rememberResult)
}

func TestMCPBridge_RegistersAllTools(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")
	if got, want := len(bridge.ListTools()), 31; got != want {
		t.Fatalf("tool count=%d want=%d", got, want)
	}
}

func TestMCPBridge_StatusTool(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")

	result := callBridgeTool(t, bridge, "amm_status", map[string]any{})
	if result.IsError {
		t.Fatalf("status tool returned error: %s", toolResultText(t, result))
	}

	var status core.StatusResult
	if err := json.Unmarshal([]byte(toolResultText(t, result)), &status); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if status.DBPath == "" {
		t.Fatal("expected status db_path")
	}
}

func TestMCPBridge_RememberAndRecall(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")

	rememberResult := callBridgeTool(t, bridge, "amm_remember", map[string]any{
		"type":              string(core.MemoryTypeFact),
		"scope":             string(core.ScopeGlobal),
		"body":              "mcp bridge memory remember and recall",
		"tight_description": "mcp bridge memory remember and recall",
	})
	if rememberResult.IsError {
		t.Fatalf("remember tool returned error: %s", toolResultText(t, rememberResult))
	}

	recallResult := callBridgeTool(t, bridge, "amm_recall", map[string]any{
		"query": "mcp bridge memory remember and recall",
		"opts":  map[string]any{"mode": string(core.RecallModeFacts), "limit": 10},
	})
	if recallResult.IsError {
		t.Fatalf("recall tool returned error: %s", toolResultText(t, recallResult))
	}

	var recall core.RecallResult
	if err := json.Unmarshal([]byte(toolResultText(t, recallResult)), &recall); err != nil {
		t.Fatalf("unmarshal recall: %v", err)
	}
	if len(recall.Items) == 0 {
		t.Fatal("expected at least one recall item")
	}
}

func TestMCPBridge_ErrorHandling(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")

	result := callBridgeTool(t, bridge, "amm_get_memory", map[string]any{"id": "does-not-exist"})
	if !result.IsError {
		t.Fatal("expected error result")
	}
	if got := toolResultText(t, result); got == "" {
		t.Fatal("expected error text")
	}
}

func TestMCPBridge_IngestEvent(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")

	result := callBridgeTool(t, bridge, "amm_ingest_event", map[string]any{
		"kind":          "message_user",
		"source_system": "test",
		"content":       "bridge ingest event",
	})
	if result.IsError {
		t.Fatalf("ingest_event returned error: %s", toolResultText(t, result))
	}
	evt := decodeToolResult[core.Event](t, result)
	if evt.ID == "" || evt.Kind != "message_user" {
		t.Fatalf("unexpected event: %+v", evt)
	}
}

func TestMCPBridge_IngestTranscript(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")

	result := callBridgeTool(t, bridge, "amm_ingest_transcript", map[string]any{
		"events": []any{
			map[string]any{"kind": "message_user", "source_system": "test", "content": "one"},
			map[string]any{"kind": "message_assistant", "source_system": "test", "content": "two"},
		},
	})
	if result.IsError {
		t.Fatalf("ingest_transcript returned error: %s", toolResultText(t, result))
	}
	if got := decodeToolResult[int](t, result); got != 2 {
		t.Fatalf("ingested count=%d want=2", got)
	}
}

func TestMCPBridge_GetMemory(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")
	mem := mustRememberViaTool(t, bridge, "bridge get memory")

	result := callBridgeTool(t, bridge, "amm_get_memory", map[string]any{"id": mem.ID})
	if result.IsError {
		t.Fatalf("get_memory returned error: %s", toolResultText(t, result))
	}
	got := decodeToolResult[core.Memory](t, result)
	if got.ID != mem.ID {
		t.Fatalf("memory id=%s want=%s", got.ID, mem.ID)
	}
}

func TestMCPBridge_UpdateMemory(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")
	mem := mustRememberViaTool(t, bridge, "bridge update memory")

	result := callBridgeTool(t, bridge, "amm_update_memory", map[string]any{
		"id":                mem.ID,
		"body":              "bridge update memory changed",
		"status":            string(core.MemoryStatusArchived),
		"metadata":          map[string]any{"k": "v", "skip": 1},
		"tight_description": "bridge update memory changed",
	})
	if result.IsError {
		t.Fatalf("update_memory returned error: %s", toolResultText(t, result))
	}
	updated := decodeToolResult[core.Memory](t, result)
	if updated.Body != "bridge update memory changed" || updated.Status != core.MemoryStatusArchived {
		t.Fatalf("unexpected updated memory: %+v", updated)
	}
	if updated.Metadata["k"] != "v" {
		t.Fatalf("expected metadata to include string values: %+v", updated.Metadata)
	}
}

func TestMCPBridge_ShareMemory(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")
	mem := mustRememberViaTool(t, bridge, "bridge share memory")

	result := callBridgeTool(t, bridge, "amm_share", map[string]any{"id": mem.ID, "privacy": string(core.PrivacyShared)})
	if result.IsError {
		t.Fatalf("share returned error: %s", toolResultText(t, result))
	}
	updated := decodeToolResult[core.Memory](t, result)
	if updated.PrivacyLevel != core.PrivacyShared {
		t.Fatalf("privacy=%s want=%s", updated.PrivacyLevel, core.PrivacyShared)
	}
}

func TestMCPBridge_ForgetMemory(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")
	mem := mustRememberViaTool(t, bridge, "bridge forget memory")

	result := callBridgeTool(t, bridge, "amm_forget", map[string]any{"id": mem.ID})
	if result.IsError {
		t.Fatalf("forget returned error: %s", toolResultText(t, result))
	}
	forgotten := decodeToolResult[core.Memory](t, result)
	if forgotten.Status != core.MemoryStatusRetracted {
		t.Fatalf("status=%s want=%s", forgotten.Status, core.MemoryStatusRetracted)
	}
}

func TestMCPBridge_Describe(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")
	mem := mustRememberViaTool(t, bridge, "bridge describe memory")

	result := callBridgeTool(t, bridge, "amm_describe", map[string]any{"ids": []any{mem.ID, 12}})
	if result.IsError {
		t.Fatalf("describe returned error: %s", toolResultText(t, result))
	}
	described := decodeToolResult[[]core.DescribeResult](t, result)
	if len(described) == 0 {
		t.Fatal("expected describe results")
	}
}

func TestMCPBridge_Expand(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")
	mem := mustRememberViaTool(t, bridge, "bridge expand memory")

	result := callBridgeTool(t, bridge, "amm_expand", map[string]any{"id": mem.ID, "kind": "memory"})
	if result.IsError {
		t.Fatalf("expand returned error: %s", toolResultText(t, result))
	}
	expanded := decodeToolResult[core.ExpandResult](t, result)
	if expanded.Memory == nil || expanded.Memory.ID != mem.ID {
		t.Fatalf("unexpected expand result: %+v", expanded)
	}
}

func TestMCPBridge_History(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")

	callBridgeTool(t, bridge, "amm_ingest_event", map[string]any{
		"kind":          "message_user",
		"source_system": "test",
		"content":       "bridge history message",
	})

	result := callBridgeTool(t, bridge, "amm_history", map[string]any{"query": "bridge history", "opts": map[string]any{"limit": 10}})
	if result.IsError {
		t.Fatalf("history returned error: %s", toolResultText(t, result))
	}
	events := decodeToolResult[[]core.Event](t, result)
	if len(events) == 0 {
		t.Fatal("expected history events")
	}
}

func TestMCPBridge_ExplainRecall(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")
	mem := mustRememberViaTool(t, bridge, "bridge explain recall")

	result := callBridgeTool(t, bridge, "amm_explain_recall", map[string]any{
		"query":   "bridge explain recall",
		"item_id": mem.ID,
	})
	if result.IsError {
		t.Fatalf("explain_recall returned error: %s", toolResultText(t, result))
	}
	explained := decodeToolResult[map[string]any](t, result)
	if len(explained) == 0 {
		t.Fatal("expected explain recall payload")
	}
}

func TestMCPBridge_PolicyLifecycle(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")

	addResult := callBridgeTool(t, bridge, "amm_policy_add", map[string]any{
		"pattern_type": "source",
		"pattern":      "bridge-*",
		"mode":         "full",
		"priority":     float64(7),
		"match_mode":   "glob",
	})
	if addResult.IsError {
		t.Fatalf("policy_add returned error: %s", toolResultText(t, addResult))
	}
	policy := decodeToolResult[core.IngestionPolicy](t, addResult)

	listResult := callBridgeTool(t, bridge, "amm_policy_list", map[string]any{})
	if listResult.IsError {
		t.Fatalf("policy_list returned error: %s", toolResultText(t, listResult))
	}
	policies := decodeToolResult[[]core.IngestionPolicy](t, listResult)
	if len(policies) == 0 {
		t.Fatal("expected policies")
	}

	removeResult := callBridgeTool(t, bridge, "amm_policy_remove", map[string]any{"id": policy.ID})
	if removeResult.IsError {
		t.Fatalf("policy_remove returned error: %s", toolResultText(t, removeResult))
	}
	removed := decodeToolResult[map[string]any](t, removeResult)
	if removed["status"] != "removed" {
		t.Fatalf("unexpected remove payload: %+v", removed)
	}
}

func TestMCPBridge_ProjectLifecycle(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")

	registerResult := callBridgeTool(t, bridge, "amm_register_project", map[string]any{
		"name":        "bridge project",
		"path":        "/tmp/bridge",
		"description": "bridge project description",
	})
	if registerResult.IsError {
		t.Fatalf("register_project returned error: %s", toolResultText(t, registerResult))
	}
	project := decodeToolResult[core.Project](t, registerResult)

	getResult := callBridgeTool(t, bridge, "amm_get_project", map[string]any{"id": project.ID})
	if getResult.IsError {
		t.Fatalf("get_project returned error: %s", toolResultText(t, getResult))
	}

	listResult := callBridgeTool(t, bridge, "amm_list_projects", map[string]any{})
	if listResult.IsError {
		t.Fatalf("list_projects returned error: %s", toolResultText(t, listResult))
	}
	projects := decodeToolResult[[]core.Project](t, listResult)
	if len(projects) == 0 {
		t.Fatal("expected projects")
	}

	removeResult := callBridgeTool(t, bridge, "amm_remove_project", map[string]any{"id": project.ID})
	if removeResult.IsError {
		t.Fatalf("remove_project returned error: %s", toolResultText(t, removeResult))
	}
}

func TestMCPBridge_RelationshipLifecycle(t *testing.T) {
	srv, repo, ctx := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")

	now := time.Now().UTC()
	entityA := &core.Entity{ID: "ent_mcp_a", Type: "person", CanonicalName: "A", Description: "A", CreatedAt: now, UpdatedAt: now}
	entityB := &core.Entity{ID: "ent_mcp_b", Type: "person", CanonicalName: "B", Description: "B", CreatedAt: now, UpdatedAt: now}
	if err := repo.InsertEntity(ctx, entityA); err != nil {
		t.Fatalf("insert entity A: %v", err)
	}
	if err := repo.InsertEntity(ctx, entityB); err != nil {
		t.Fatalf("insert entity B: %v", err)
	}

	addResult := callBridgeTool(t, bridge, "amm_add_relationship", map[string]any{
		"from_entity_id":    entityA.ID,
		"to_entity_id":      entityB.ID,
		"relationship_type": "related_to",
	})
	if addResult.IsError {
		t.Fatalf("add_relationship returned error: %s", toolResultText(t, addResult))
	}
	rel := decodeToolResult[core.Relationship](t, addResult)

	getResult := callBridgeTool(t, bridge, "amm_get_relationship", map[string]any{"id": rel.ID})
	if getResult.IsError {
		t.Fatalf("get_relationship returned error: %s", toolResultText(t, getResult))
	}

	listResult := callBridgeTool(t, bridge, "amm_list_relationships", map[string]any{
		"entity_id":         entityA.ID,
		"relationship_type": "related_to",
		"limit":             10,
	})
	if listResult.IsError {
		t.Fatalf("list_relationships returned error: %s", toolResultText(t, listResult))
	}
	rels := decodeToolResult[[]core.Relationship](t, listResult)
	if len(rels) == 0 {
		t.Fatal("expected relationships")
	}

	removeResult := callBridgeTool(t, bridge, "amm_remove_relationship", map[string]any{"id": rel.ID})
	if removeResult.IsError {
		t.Fatalf("remove_relationship returned error: %s", toolResultText(t, removeResult))
	}
}

func TestMCPBridge_GetSummary_NotFound(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")

	result := callBridgeTool(t, bridge, "amm_get_summary", map[string]any{"id": "missing-summary"})
	if !result.IsError {
		t.Fatal("expected error result")
	}
}

func TestMCPBridge_GetEpisode_NotFound(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")

	result := callBridgeTool(t, bridge, "amm_get_episode", map[string]any{"id": "missing-episode"})
	if !result.IsError {
		t.Fatal("expected error result")
	}
}

func TestMCPBridge_GetEntity_NotFound(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")

	result := callBridgeTool(t, bridge, "amm_get_entity", map[string]any{"id": "missing-entity"})
	if !result.IsError {
		t.Fatal("expected error result")
	}
}

func TestMCPBridge_RunJob(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")

	result := callBridgeTool(t, bridge, "amm_jobs_run", map[string]any{"kind": "cleanup_recall_history"})
	if result.IsError {
		t.Fatalf("jobs_run returned error: %s", toolResultText(t, result))
	}
	job := decodeToolResult[core.Job](t, result)
	if job.Kind == "" {
		t.Fatalf("unexpected job: %+v", job)
	}
}

func TestMCPBridge_Repair(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")

	result := callBridgeTool(t, bridge, "amm_repair", map[string]any{"check": true})
	if result.IsError {
		t.Fatalf("repair returned error: %s", toolResultText(t, result))
	}
	report := decodeToolResult[core.RepairReport](t, result)
	if report.Checked < 0 || report.Issues < 0 {
		t.Fatalf("unexpected repair report: %+v", report)
	}
}

func TestMCPBridge_ResetDerived(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")

	result := callBridgeTool(t, bridge, "amm_reset_derived", map[string]any{"confirm": true})
	if result.IsError {
		t.Fatalf("reset_derived returned error: %s", toolResultText(t, result))
	}
	reset := decodeToolResult[core.ResetDerivedResult](t, result)
	if reset.EventsReset < 0 {
		t.Fatalf("unexpected reset result: %+v", reset)
	}
}

func TestMCPBridge_Init(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	bridge := newMCPBridge(srv.svc, "1.0.0")

	result := callBridgeTool(t, bridge, "amm_init", map[string]any{})
	if result.IsError {
		t.Fatalf("init returned error: %s", toolResultText(t, result))
	}
	payload := decodeToolResult[map[string]any](t, result)
	if payload["status"] == nil {
		t.Fatalf("missing status in init payload: %+v", payload)
	}
}

func TestMCPOverHTTP_Integration(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	ts := httptest.NewServer(srv.server.Handler)
	t.Cleanup(ts.Close)

	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	resp, err := ts.Client().Post(ts.URL+"/v1/mcp", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("post initialize: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d want=200", resp.StatusCode)
	}

	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["jsonrpc"] != "2.0" {
		t.Fatalf("jsonrpc=%v want=2.0", payload["jsonrpc"])
	}
	if payload["result"] == nil {
		t.Fatalf("missing initialize result: %#v", payload)
	}
}
