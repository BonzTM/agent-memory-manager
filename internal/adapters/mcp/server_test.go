//go:build fts5

package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/bonztm/agent-memory-manager/internal/adapters/sqlite"
	"github.com/bonztm/agent-memory-manager/internal/core"
	"github.com/bonztm/agent-memory-manager/internal/service"
)

func testMCPService(t *testing.T) core.Service {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "mcp-test.db")
	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return service.New(&sqlite.SQLiteRepository{DB: db}, dbPath, nil, nil)
}

func toolReq(t *testing.T, name string, args interface{}) jsonrpcRequest {
	t.Helper()
	argBytes, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	paramsBytes, err := json.Marshal(map[string]interface{}{
		"name":      name,
		"arguments": json.RawMessage(argBytes),
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return jsonrpcRequest{JSONRPC: "2.0", ID: 1, Method: "tools/call", Params: paramsBytes}
}

func decodeToolResultText(t *testing.T, resp jsonrpcResponse) string {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("unexpected rpc error: %+v", resp.Error)
	}
	resultMap, ok := resp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type: %T", resp.Result)
	}
	content, ok := resultMap["content"].([]map[string]string)
	if ok && len(content) > 0 {
		return content[0]["text"]
	}
	contentIface, ok := resultMap["content"].([]interface{})
	if !ok || len(contentIface) == 0 {
		t.Fatalf("missing content in result: %#v", resultMap)
	}
	first, ok := contentIface[0].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected content entry type: %T", contentIface[0])
	}
	text, _ := first["text"].(string)
	return text
}

func TestToolsIncludesPolicyTools(t *testing.T) {
	ts := tools()
	seen := map[string]bool{}
	for _, tool := range ts {
		seen[tool.Name] = true
	}
	for _, name := range []string{"amm_policy_list", "amm_policy_add", "amm_policy_remove"} {
		if !seen[name] {
			t.Fatalf("expected tools() to include %s", name)
		}
	}
}

func TestHandleToolCallPolicyLifecycle(t *testing.T) {
	svc := testMCPService(t)

	addResp := handleToolCall(svc, toolReq(t, "amm_policy_add", map[string]interface{}{
		"pattern_type": "source",
		"pattern":      "svc-*",
		"mode":         "full",
	}))
	addText := decodeToolResultText(t, addResp)
	var created map[string]interface{}
	if err := json.Unmarshal([]byte(addText), &created); err != nil {
		t.Fatalf("decode add result: %v", err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatalf("expected policy id in add result: %v", created)
	}

	listResp := handleToolCall(svc, toolReq(t, "amm_policy_list", map[string]interface{}{}))
	listText := decodeToolResultText(t, listResp)
	var policies []map[string]interface{}
	if err := json.Unmarshal([]byte(listText), &policies); err != nil {
		t.Fatalf("decode list result: %v", err)
	}
	if len(policies) != 1 {
		t.Fatalf("expected 1 policy in list result, got %d", len(policies))
	}

	removeResp := handleToolCall(svc, toolReq(t, "amm_policy_remove", map[string]interface{}{"id": id}))
	removeText := decodeToolResultText(t, removeResp)
	var removeResult map[string]interface{}
	if err := json.Unmarshal([]byte(removeText), &removeResult); err != nil {
		t.Fatalf("decode remove result: %v", err)
	}
	if removeResult["status"] != "removed" {
		t.Fatalf("expected removed status, got %v", removeResult["status"])
	}

	missingResp := handleToolCall(svc, toolReq(t, "amm_policy_remove", map[string]interface{}{"id": "pol_missing"}))
	if missingResp.Error != nil {
		t.Fatalf("expected tool-level error response, got rpc error: %+v", missingResp.Error)
	}
	missingMap, ok := missingResp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected result type: %T", missingResp.Result)
	}
	isError, _ := missingMap["isError"].(bool)
	if !isError {
		t.Fatalf("expected isError=true for missing policy remove, got %#v", missingMap)
	}
}

func TestHandleToolCallPolicyAddValidation(t *testing.T) {
	svc := testMCPService(t)

	resp := handleToolCall(svc, toolReq(t, "amm_policy_add", map[string]interface{}{
		"pattern_type": "tenant",
		"pattern":      "*",
		"mode":         "full",
	}))

	if resp.Error == nil {
		t.Fatalf("expected rpc error for invalid policy add")
	}
	if resp.Error.Code != -32602 {
		t.Fatalf("expected invalid params code -32602, got %d", resp.Error.Code)
	}
}

func TestHandleToolCallShareMemory(t *testing.T) {
	svc := testMCPService(t)

	rememberResp := handleToolCall(svc, toolReq(t, "amm_remember", map[string]interface{}{
		"type":              string(core.MemoryTypeFact),
		"agent_id":          "agent-a",
		"privacy_level":     string(core.PrivacyPrivate),
		"body":              "share test memory",
		"tight_description": "share test memory",
	}))
	var created core.Memory
	if err := json.Unmarshal([]byte(decodeToolResultText(t, rememberResp)), &created); err != nil {
		t.Fatalf("decode remember result: %v", err)
	}

	shareResp := handleToolCall(svc, toolReq(t, "amm_share", map[string]interface{}{
		"id":      created.ID,
		"privacy": "shared",
	}))
	var shared core.Memory
	if err := json.Unmarshal([]byte(decodeToolResultText(t, shareResp)), &shared); err != nil {
		t.Fatalf("decode share result: %v", err)
	}
	if shared.PrivacyLevel != core.PrivacyShared {
		t.Fatalf("expected shared privacy level, got %q", shared.PrivacyLevel)
	}

	badPrivacy := handleToolCall(svc, toolReq(t, "amm_share", map[string]interface{}{
		"id":      created.ID,
		"privacy": "team_only",
	}))
	if badPrivacy.Error == nil {
		t.Fatalf("expected invalid params for bad privacy")
	}
	if badPrivacy.Error.Code != -32602 {
		t.Fatalf("expected invalid params code -32602, got %d", badPrivacy.Error.Code)
	}
}
