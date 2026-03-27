//go:build fts5

package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func decodeToolResultJSON(t *testing.T, resp jsonrpcResponse, out interface{}) {
	t.Helper()
	text := decodeToolResultText(t, resp)
	if err := json.Unmarshal([]byte(text), out); err != nil {
		t.Fatalf("decode tool result json: %v (text=%q)", err, text)
	}
}

func TestToolsContainsExpectedNames(t *testing.T) {
	t.Parallel()

	allTools := tools()
	if len(allTools) == 0 {
		t.Fatal("expected non-empty tool list")
	}

	expected := []string{
		"amm_init",
		"amm_ingest_event",
		"amm_remember",
		"amm_recall",
		"amm_describe",
		"amm_expand",
		"amm_history",
		"amm_get_memory",
		"amm_update_memory",
		"amm_share",
		"amm_jobs_run",
		"amm_repair",
		"amm_explain_recall",
		"amm_status",
		"amm_ingest_transcript",
		"amm_policy_list",
		"amm_policy_add",
		"amm_policy_remove",
		"amm_reset_derived",
	}

	seen := map[string]bool{}
	for _, tool := range allTools {
		seen[tool.Name] = true
		if tool.InputSchema == nil {
			t.Fatalf("tool %s has nil schema", tool.Name)
		}
	}

	for _, name := range expected {
		if !seen[name] {
			t.Fatalf("expected tools() to include %s", name)
		}
	}
}

func TestSchemaHelpersNonNil(t *testing.T) {
	t.Parallel()

	schemas := []map[string]interface{}{
		emptySchema(),
		eventSchema(),
		rememberSchema(),
		recallSchema(),
		describeSchema(),
		expandSchema(),
		historySchema(),
		idSchema(),
		jobSchema(),
		explainSchema(),
		repairSchema(),
		transcriptSchema(),
		updateMemorySchema(),
		shareSchema(),
		policyListSchema(),
		policyAddSchema(),
		policyRemoveSchema(),
		resetDerivedSchema(),
	}

	for i, schema := range schemas {
		if schema == nil {
			t.Fatalf("schema %d is nil", i)
		}
		if schema["type"] == nil {
			t.Fatalf("schema %d missing type", i)
		}
	}
}

func TestHandleRequestInitializeToolsListAndUnknown(t *testing.T) {
	t.Parallel()

	svc := testMCPService(t)

	initResp := handleRequest(svc, jsonrpcRequest{JSONRPC: "2.0", ID: 1, Method: "initialize"})
	if initResp.Error != nil {
		t.Fatalf("initialize error: %+v", initResp.Error)
	}
	initMap, ok := initResp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("initialize result type: %T", initResp.Result)
	}
	if got, _ := initMap["protocolVersion"].(string); got == "" {
		t.Fatalf("expected protocolVersion, got: %#v", initMap)
	}

	listResp := handleRequest(svc, jsonrpcRequest{JSONRPC: "2.0", ID: 2, Method: "tools/list"})
	if listResp.Error != nil {
		t.Fatalf("tools/list error: %+v", listResp.Error)
	}
	listMap, ok := listResp.Result.(map[string]interface{})
	if !ok {
		t.Fatalf("tools/list result type: %T", listResp.Result)
	}
	toolList, ok := listMap["tools"].([]Tool)
	if !ok {
		t.Fatalf("tools/list tools type: %T", listMap["tools"])
	}
	if len(toolList) == 0 {
		t.Fatal("expected non-empty tools/list response")
	}

	callReq := toolReq(t, "amm_status", map[string]interface{}{})
	callResp := handleRequest(svc, callReq)
	if callResp.Error != nil {
		t.Fatalf("tools/call via handleRequest error: %+v", callResp.Error)
	}

	unknownResp := handleRequest(svc, jsonrpcRequest{JSONRPC: "2.0", ID: 3, Method: "unknown/method"})
	if unknownResp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if unknownResp.Error.Code != -32601 {
		t.Fatalf("unexpected unknown method code: %d", unknownResp.Error.Code)
	}
}

func TestHandleToolCallInvalidArgumentsPerTool(t *testing.T) {
	t.Parallel()

	svc := testMCPService(t)

	toolsWithArgs := []string{
		"amm_ingest_event",
		"amm_ingest_transcript",
		"amm_remember",
		"amm_recall",
		"amm_describe",
		"amm_expand",
		"amm_history",
		"amm_get_memory",
		"amm_update_memory",
		"amm_share",
		"amm_policy_add",
		"amm_policy_remove",
		"amm_reset_derived",
		"amm_jobs_run",
		"amm_explain_recall",
		"amm_repair",
	}

	for _, toolName := range toolsWithArgs {
		toolName := toolName
		t.Run(toolName, func(t *testing.T) {
			resp := handleToolCall(svc, toolReq(t, toolName, "bad-args"))
			if resp.Error == nil {
				t.Fatalf("expected error for invalid args on %s", toolName)
			}
			if resp.Error.Code != -32602 {
				t.Fatalf("unexpected error code for %s: %d", toolName, resp.Error.Code)
			}
			if !strings.Contains(resp.Error.Message, "invalid arguments for "+toolName) {
				t.Fatalf("unexpected error message for %s: %q", toolName, resp.Error.Message)
			}
		})
	}
}

func TestHandleToolCallInvalidParamsAndUnknownTool(t *testing.T) {
	t.Parallel()

	svc := testMCPService(t)

	invalidParamsResp := handleToolCall(svc, jsonrpcRequest{
		JSONRPC: "2.0",
		ID:      "invalid",
		Method:  "tools/call",
		Params:  json.RawMessage(`{"name":`),
	})
	if invalidParamsResp.Error == nil {
		t.Fatal("expected invalid params error")
	}
	if invalidParamsResp.Error.Code != -32602 {
		t.Fatalf("unexpected invalid params code: %d", invalidParamsResp.Error.Code)
	}

	unknownToolResp := handleToolCall(svc, toolReq(t, "amm_unknown", map[string]interface{}{}))
	if unknownToolResp.Error == nil {
		t.Fatal("expected unknown tool error")
	}
	if unknownToolResp.Error.Code != -32602 {
		t.Fatalf("unexpected unknown tool code: %d", unknownToolResp.Error.Code)
	}
}

func TestHandleToolCallRecallDefaultsToHybrid(t *testing.T) {
	svc := testMCPService(t)

	ingestResp := handleToolCall(svc, toolReq(t, "amm_ingest_event", map[string]interface{}{
		"kind":          "message_user",
		"source_system": "mcp-test",
		"content":       "mcp default hybrid recall event",
	}))
	if ingestResp.Error != nil {
		t.Fatalf("ingest event rpc error: %+v", ingestResp.Error)
	}

	recallResp := handleToolCall(svc, toolReq(t, "amm_recall", map[string]interface{}{
		"query": "hybrid recall event",
		"opts":  map[string]interface{}{},
	}))
	var recallResult core.RecallResult
	decodeToolResultJSON(t, recallResp, &recallResult)
	if recallResult.Meta.Mode != core.RecallModeHybrid {
		t.Fatalf("expected default recall mode hybrid, got %#v", recallResult.Meta.Mode)
	}
	if len(recallResult.Items) == 0 {
		t.Fatalf("expected recall to return items: %#v", recallResult)
	}
	foundHistory := false
	for _, item := range recallResult.Items {
		if item.Kind == "history-node" {
			foundHistory = true
			break
		}
	}
	if !foundHistory {
		t.Fatalf("expected history-node in default recall result: %#v", recallResult.Items)
	}
}

func TestHandleToolCallCoversAllTools(t *testing.T) {
	svc := testMCPService(t)

	initResp := handleToolCall(svc, toolReq(t, "amm_init", map[string]interface{}{}))
	if initResp.Error != nil {
		t.Fatalf("amm_init rpc error: %+v", initResp.Error)
	}
	var initResult struct {
		Message string            `json:"message"`
		Status  core.StatusResult `json:"status"`
	}
	decodeToolResultJSON(t, initResp, &initResult)
	if initResult.Message != "already initialized" {
		t.Fatalf("expected amm_init message to report already initialized, got %q", initResult.Message)
	}
	if !initResult.Status.Initialized {
		t.Fatalf("expected amm_init status to report initialized, got %#v", initResult.Status)
	}

	ingestEventResp := handleToolCall(svc, toolReq(t, "amm_ingest_event", map[string]interface{}{
		"kind":          "message_user",
		"source_system": "mcp-test",
		"content":       "event for mcp tool call coverage",
	}))
	var ingestedEvent core.Event
	decodeToolResultJSON(t, ingestEventResp, &ingestedEvent)
	if ingestedEvent.ID == "" {
		t.Fatal("expected ingested event to have id")
	}

	rememberResp := handleToolCall(svc, toolReq(t, "amm_remember", map[string]interface{}{
		"type":              string(core.MemoryTypeFact),
		"scope":             string(core.ScopeGlobal),
		"body":              "coverage memory body",
		"tight_description": "coverage memory",
		"subject":           "mcp",
	}))
	var createdMemory core.Memory
	decodeToolResultJSON(t, rememberResp, &createdMemory)
	if createdMemory.ID == "" {
		t.Fatal("expected remembered memory to have id")
	}

	recallResp := handleToolCall(svc, toolReq(t, "amm_recall", map[string]interface{}{
		"query": "coverage",
		"opts": map[string]interface{}{
			"mode":  "ambient",
			"limit": 10,
		},
	}))
	var recallResult core.RecallResult
	decodeToolResultJSON(t, recallResp, &recallResult)
	if len(recallResult.Items) == 0 {
		t.Fatalf("expected recall to return items: %#v", recallResult)
	}

	describeResp := handleToolCall(svc, toolReq(t, "amm_describe", map[string]interface{}{
		"ids": []string{createdMemory.ID},
	}))
	var described []core.DescribeResult
	decodeToolResultJSON(t, describeResp, &described)
	if len(described) != 1 || described[0].ID != createdMemory.ID {
		t.Fatalf("unexpected describe result: %#v", described)
	}

	expandResp := handleToolCall(svc, toolReq(t, "amm_expand", map[string]interface{}{
		"id":   createdMemory.ID,
		"kind": "memory",
	}))
	var expanded core.ExpandResult
	decodeToolResultJSON(t, expandResp, &expanded)
	if expanded.Memory == nil || expanded.Memory.ID != createdMemory.ID {
		t.Fatalf("unexpected expand result: %#v", expanded)
	}

	historyResp := handleToolCall(svc, toolReq(t, "amm_history", map[string]interface{}{
		"query": "coverage",
		"opts":  map[string]interface{}{"limit": 10},
	}))
	var history []core.Event
	decodeToolResultJSON(t, historyResp, &history)
	if len(history) == 0 {
		t.Fatal("expected non-empty history")
	}

	getMemoryResp := handleToolCall(svc, toolReq(t, "amm_get_memory", map[string]interface{}{"id": createdMemory.ID}))
	var fetchedMemory core.Memory
	decodeToolResultJSON(t, getMemoryResp, &fetchedMemory)
	if fetchedMemory.ID != createdMemory.ID {
		t.Fatalf("unexpected fetched memory id: %s", fetchedMemory.ID)
	}

	updatedMemory := fetchedMemory
	updatedMemory.Body = "coverage memory body updated"
	updatedMemory.TightDescription = "coverage memory updated"
	updatedMemory.Status = core.MemoryStatusActive

	updateResp := handleToolCall(svc, toolReq(t, "amm_update_memory", updatedMemory))
	var updateResult core.Memory
	decodeToolResultJSON(t, updateResp, &updateResult)
	if updateResult.ID != createdMemory.ID || updateResult.Body != updatedMemory.Body {
		t.Fatalf("unexpected update result: %#v", updateResult)
	}

	shareResp := handleToolCall(svc, toolReq(t, "amm_share", map[string]interface{}{
		"id":      createdMemory.ID,
		"privacy": "shared",
	}))
	var sharedResult core.Memory
	decodeToolResultJSON(t, shareResp, &sharedResult)
	if sharedResult.ID != createdMemory.ID || sharedResult.PrivacyLevel != core.PrivacyShared {
		t.Fatalf("unexpected share result: %#v", sharedResult)
	}

	jobResp := handleToolCall(svc, toolReq(t, "amm_jobs_run", map[string]interface{}{"kind": "rebuild_indexes"}))
	var job core.Job
	decodeToolResultJSON(t, jobResp, &job)
	if job.Kind != "rebuild_indexes" {
		t.Fatalf("unexpected job kind: %#v", job)
	}

	repairResp := handleToolCall(svc, toolReq(t, "amm_repair", map[string]interface{}{"check": true}))
	var report core.RepairReport
	decodeToolResultJSON(t, repairResp, &report)
	if report.Checked < 0 || report.Issues < 0 || report.Fixed < 0 {
		t.Fatalf("invalid repair report: %#v", report)
	}

	explainResp := handleToolCall(svc, toolReq(t, "amm_explain_recall", map[string]interface{}{
		"query":   "coverage memory",
		"item_id": createdMemory.ID,
	}))
	var explain map[string]interface{}
	decodeToolResultJSON(t, explainResp, &explain)
	if _, ok := explain["final_score"]; !ok {
		t.Fatalf("expected final_score in explain result: %#v", explain)
	}

	statusResp := handleToolCall(svc, toolReq(t, "amm_status", map[string]interface{}{}))
	var status core.StatusResult
	decodeToolResultJSON(t, statusResp, &status)
	if !status.Initialized {
		t.Fatalf("expected initialized status true: %#v", status)
	}
	if status.EventCount == 0 || status.MemoryCount == 0 {
		t.Fatalf("expected non-zero event/memory counts: %#v", status)
	}

	ingestTranscriptResp := handleToolCall(svc, toolReq(t, "amm_ingest_transcript", map[string]interface{}{
		"events": []map[string]interface{}{
			{"kind": "message_user", "source_system": "mcp-test", "content": "transcript event one"},
			{"kind": "message_assistant", "source_system": "mcp-test", "content": "transcript event two"},
		},
	}))
	var transcriptCount int
	decodeToolResultJSON(t, ingestTranscriptResp, &transcriptCount)
	if transcriptCount != 2 {
		t.Fatalf("expected ingest transcript count 2, got %d", transcriptCount)
	}

	policyAddResp := handleToolCall(svc, toolReq(t, "amm_policy_add", map[string]interface{}{
		"pattern_type": "source",
		"pattern":      "mcp-*",
		"mode":         "full",
	}))
	var addedPolicy core.IngestionPolicy
	decodeToolResultJSON(t, policyAddResp, &addedPolicy)
	if addedPolicy.ID == "" {
		t.Fatalf("expected policy id from add: %#v", addedPolicy)
	}

	policyListResp := handleToolCall(svc, toolReq(t, "amm_policy_list", map[string]interface{}{}))
	var policies []core.IngestionPolicy
	decodeToolResultJSON(t, policyListResp, &policies)
	if len(policies) == 0 {
		t.Fatal("expected at least one policy in list")
	}

	foundPolicy := false
	for _, policy := range policies {
		if policy.ID == addedPolicy.ID {
			foundPolicy = true
			break
		}
	}
	if !foundPolicy {
		t.Fatalf("added policy not found in list: added=%s list=%v", addedPolicy.ID, policies)
	}

	policyRemoveResp := handleToolCall(svc, toolReq(t, "amm_policy_remove", map[string]interface{}{"id": addedPolicy.ID}))
	var removeResult map[string]string
	decodeToolResultJSON(t, policyRemoveResp, &removeResult)
	if !reflect.DeepEqual(removeResult, map[string]string{"id": addedPolicy.ID, "status": "removed"}) {
		t.Fatalf("unexpected policy remove result: %#v", removeResult)
	}

	resetDerivedResp := handleToolCall(svc, toolReq(t, "amm_reset_derived", map[string]interface{}{"confirm": true}))
	var resetResult core.ResetDerivedResult
	decodeToolResultJSON(t, resetDerivedResp, &resetResult)
}

func TestServeHandlesParseAndRequests(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AMM_DB_PATH", filepath.Join(tmpDir, "serve-test.db"))

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		_ = inR.Close()
		_ = inW.Close()
		t.Fatalf("create stdout pipe: %v", err)
	}

	oldStdin := os.Stdin
	oldStdout := os.Stdout
	os.Stdin = inR
	os.Stdout = outW
	t.Cleanup(func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
		_ = inR.Close()
		_ = inW.Close()
		_ = outR.Close()
		_ = outW.Close()
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- Serve()
		_ = outW.Close()
	}()

	if _, err := inW.Write([]byte("{not-json}\n")); err != nil {
		t.Fatalf("write invalid request: %v", err)
	}
	for _, req := range []jsonrpcRequest{
		{JSONRPC: "2.0", ID: 1, Method: "initialize"},
		{JSONRPC: "2.0", ID: 2, Method: "tools/list"},
		{JSONRPC: "2.0", ID: 3, Method: "unknown/method"},
		toolReq(t, "amm_status", map[string]interface{}{}),
	} {
		line, err := json.Marshal(req)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		if _, err := inW.Write(append(line, '\n')); err != nil {
			t.Fatalf("write request: %v", err)
		}
	}
	_ = inW.Close()

	if err := <-errCh; err != nil {
		t.Fatalf("Serve() returned error: %v", err)
	}

	outputBytes, err := io.ReadAll(outR)
	if err != nil {
		t.Fatalf("read server output: %v", err)
	}

	var responses []jsonrpcResponse
	scanner := bufio.NewScanner(bytes.NewReader(outputBytes))
	for scanner.Scan() {
		var resp jsonrpcResponse
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			t.Fatalf("decode response line: %v, line=%q", err, scanner.Text())
		}
		responses = append(responses, resp)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan output: %v", err)
	}

	if len(responses) != 5 {
		t.Fatalf("expected 5 responses, got %d", len(responses))
	}
	if responses[0].Error == nil || responses[0].Error.Code != -32700 {
		t.Fatalf("expected parse error response first, got %#v", responses[0])
	}
	if responses[1].Error != nil {
		t.Fatalf("initialize response error: %+v", responses[1].Error)
	}
	if responses[2].Error != nil {
		t.Fatalf("tools/list response error: %+v", responses[2].Error)
	}
	if responses[3].Error == nil || responses[3].Error.Code != -32601 {
		t.Fatalf("expected unknown method error response, got %#v", responses[3])
	}
	if responses[4].Error != nil {
		t.Fatalf("tools/call response error: %+v", responses[4].Error)
	}
}
