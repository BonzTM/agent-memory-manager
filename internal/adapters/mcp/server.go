package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/joshd-04/agent-memory-manager/internal/core"
	"github.com/joshd-04/agent-memory-manager/internal/runtime"
)

// JSON-RPC types for MCP protocol.
type jsonrpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonrpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Tool describes an MCP tool.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// tools returns the list of amm MCP tools matching CLI commands.
func tools() []Tool {
	return []Tool{
		{Name: "amm_init", Description: "Initialize the amm database", InputSchema: emptySchema()},
		{Name: "amm_ingest_event", Description: "Append a raw event to history", InputSchema: eventSchema()},
		{Name: "amm_remember", Description: "Commit a durable memory", InputSchema: rememberSchema()},
		{Name: "amm_recall", Description: "Retrieve memories using various modes", InputSchema: recallSchema()},
		{Name: "amm_describe", Description: "Get thin descriptions of items", InputSchema: describeSchema()},
		{Name: "amm_expand", Description: "Expand an item to full detail", InputSchema: expandSchema()},
		{Name: "amm_history", Description: "Query raw interaction history", InputSchema: historySchema()},
		{Name: "amm_get_memory", Description: "Get a single memory by ID", InputSchema: idSchema()},
		{Name: "amm_jobs_run", Description: "Run a maintenance job", InputSchema: jobSchema()},
		{Name: "amm_explain_recall", Description: "Explain why an item surfaced", InputSchema: explainSchema()},
		{Name: "amm_repair", Description: "Run integrity checks and repairs", InputSchema: repairSchema()},
		{Name: "amm_status", Description: "Get system status", InputSchema: emptySchema()},
		{Name: "amm_ingest_transcript", Description: "Bulk ingest a sequence of events", InputSchema: transcriptSchema()},
		{Name: "amm_update_memory", Description: "Update an existing memory", InputSchema: updateMemorySchema()},
	}
}

// Serve runs the MCP server on stdin/stdout using JSON-RPC.
func Serve() error {
	cfg := runtime.DefaultConfig()
	cfg = runtime.ConfigFromEnv(cfg)

	svc, cleanup, err := runtime.NewService(cfg)
	if err != nil {
		return fmt.Errorf("init service: %w", err)
	}
	defer cleanup()

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		var req jsonrpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			writeResponse(jsonrpcResponse{
				JSONRPC: "2.0",
				Error:   &rpcError{Code: -32700, Message: "parse error"},
			})
			continue
		}

		resp := handleRequest(svc, req)
		writeResponse(resp)
	}
	return scanner.Err()
}

func handleRequest(svc core.Service, req jsonrpcRequest) jsonrpcResponse {
	switch req.Method {
	case "initialize":
		return jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]string{
					"name":    "amm-mcp",
					"version": "0.1.0",
				},
				"capabilities": map[string]interface{}{
					"tools": map[string]bool{"listChanged": false},
				},
			},
		}

	case "tools/list":
		return jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]interface{}{"tools": tools()},
		}

	case "tools/call":
		return handleToolCall(svc, req)

	default:
		return jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)},
		}
	}
}

type toolCallParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func handleToolCall(svc core.Service, req jsonrpcRequest) jsonrpcResponse {
	var params toolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32602, Message: "invalid params"},
		}
	}

	ctx := context.Background()
	var result interface{}
	var callErr error

	switch params.Name {
	case "amm_init":
		callErr = svc.Init(ctx, "")

	case "amm_ingest_event":
		var evt core.Event
		if err := json.Unmarshal(params.Arguments, &evt); err != nil {
			return errorResponse(req.ID, -32602, fmt.Sprintf("invalid arguments for %s: %v", params.Name, err))
		}
		result, callErr = svc.IngestEvent(ctx, &evt)

	case "amm_ingest_transcript":
		var args struct {
			Events []*core.Event `json:"events"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return errorResponse(req.ID, -32602, fmt.Sprintf("invalid arguments for %s: %v", params.Name, err))
		}
		result, callErr = svc.IngestTranscript(ctx, args.Events)

	case "amm_remember":
		var mem core.Memory
		if err := json.Unmarshal(params.Arguments, &mem); err != nil {
			return errorResponse(req.ID, -32602, fmt.Sprintf("invalid arguments for %s: %v", params.Name, err))
		}
		result, callErr = svc.Remember(ctx, &mem)

	case "amm_recall":
		var args struct {
			Query string             `json:"query"`
			Opts  core.RecallOptions `json:"opts"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return errorResponse(req.ID, -32602, fmt.Sprintf("invalid arguments for %s: %v", params.Name, err))
		}
		result, callErr = svc.Recall(ctx, args.Query, args.Opts)

	case "amm_describe":
		var args struct {
			IDs []string `json:"ids"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return errorResponse(req.ID, -32602, fmt.Sprintf("invalid arguments for %s: %v", params.Name, err))
		}
		result, callErr = svc.Describe(ctx, args.IDs)

	case "amm_expand":
		var args struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return errorResponse(req.ID, -32602, fmt.Sprintf("invalid arguments for %s: %v", params.Name, err))
		}
		result, callErr = svc.Expand(ctx, args.ID, args.Kind)

	case "amm_history":
		var args struct {
			Query string              `json:"query"`
			Opts  core.HistoryOptions `json:"opts"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return errorResponse(req.ID, -32602, fmt.Sprintf("invalid arguments for %s: %v", params.Name, err))
		}
		result, callErr = svc.History(ctx, args.Query, args.Opts)

	case "amm_get_memory":
		var args struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return errorResponse(req.ID, -32602, fmt.Sprintf("invalid arguments for %s: %v", params.Name, err))
		}
		result, callErr = svc.GetMemory(ctx, args.ID)

	case "amm_update_memory":
		var mem core.Memory
		if err := json.Unmarshal(params.Arguments, &mem); err != nil {
			return errorResponse(req.ID, -32602, fmt.Sprintf("invalid arguments for %s: %v", params.Name, err))
		}
		result, callErr = svc.UpdateMemory(ctx, &mem)

	case "amm_jobs_run":
		var args struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return errorResponse(req.ID, -32602, fmt.Sprintf("invalid arguments for %s: %v", params.Name, err))
		}
		result, callErr = svc.RunJob(ctx, args.Kind)

	case "amm_explain_recall":
		var args struct {
			Query  string `json:"query"`
			ItemID string `json:"item_id"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return errorResponse(req.ID, -32602, fmt.Sprintf("invalid arguments for %s: %v", params.Name, err))
		}
		result, callErr = svc.ExplainRecall(ctx, args.Query, args.ItemID)

	case "amm_repair":
		var args struct {
			Check bool   `json:"check"`
			Fix   string `json:"fix"`
		}
		if err := json.Unmarshal(params.Arguments, &args); err != nil {
			return errorResponse(req.ID, -32602, fmt.Sprintf("invalid arguments for %s: %v", params.Name, err))
		}
		result, callErr = svc.Repair(ctx, args.Check, args.Fix)

	case "amm_status":
		result, callErr = svc.Status(ctx)

	default:
		return errorResponse(req.ID, -32602, fmt.Sprintf("unknown tool: %s", params.Name))
	}

	if callErr != nil {
		return jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"content": []map[string]string{{"type": "text", "text": fmt.Sprintf("error: %v", callErr)}},
				"isError": true,
			},
		}
	}

	resultJSON, _ := json.Marshal(result)
	return jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": string(resultJSON)}},
		},
	}
}

func errorResponse(id interface{}, code int, message string) jsonrpcResponse {
	return jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &rpcError{Code: code, Message: message},
	}
}

func writeResponse(resp jsonrpcResponse) {
	data, _ := json.Marshal(resp)
	fmt.Fprintln(os.Stdout, string(data))
}

// Schema helpers for MCP tool definitions.

func emptySchema() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}

func eventSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"kind":          map[string]string{"type": "string", "description": "Event kind (e.g. message_user, message_assistant)"},
			"source_system": map[string]string{"type": "string", "description": "Source system identifier"},
			"content":       map[string]string{"type": "string", "description": "Event content"},
			"session_id":    map[string]string{"type": "string", "description": "Session identifier"},
			"project_id":    map[string]string{"type": "string", "description": "Project identifier"},
			"occurred_at":   map[string]string{"type": "string", "description": "When the event occurred (RFC3339)"},
		},
		"required": []string{"kind", "source_system", "content"},
	}
}

func rememberSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"type":              map[string]string{"type": "string", "description": "Memory type"},
			"scope":             map[string]string{"type": "string", "description": "Scope: global, project, session"},
			"body":              map[string]string{"type": "string", "description": "Memory body"},
			"tight_description": map[string]string{"type": "string", "description": "One-line summary"},
			"subject":           map[string]string{"type": "string", "description": "Subject of the memory"},
			"project_id":        map[string]string{"type": "string", "description": "Project identifier"},
		},
		"required": []string{"type", "body", "tight_description"},
	}
}

func recallSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]string{"type": "string", "description": "Search query"},
			"opts": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"mode":       map[string]string{"type": "string"},
					"project_id": map[string]string{"type": "string"},
					"session_id": map[string]string{"type": "string"},
					"limit":      map[string]string{"type": "integer"},
				},
			},
		},
		"required": []string{"query"},
	}
}

func describeSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"ids": map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
		},
		"required": []string{"ids"},
	}
}

func expandSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id":   map[string]string{"type": "string", "description": "Item ID to expand"},
			"kind": map[string]string{"type": "string", "description": "Item kind: memory, summary, episode"},
		},
		"required": []string{"id"},
	}
}

func historySchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]string{"type": "string"},
			"opts": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"session_id": map[string]string{"type": "string"},
					"project_id": map[string]string{"type": "string"},
					"limit":      map[string]string{"type": "integer"},
				},
			},
		},
	}
}

func idSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]string{"type": "string", "description": "Item ID"},
		},
		"required": []string{"id"},
	}
}

func jobSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"kind": map[string]string{"type": "string", "description": "Job kind to run"},
		},
		"required": []string{"kind"},
	}
}

func explainSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query":   map[string]string{"type": "string"},
			"item_id": map[string]string{"type": "string"},
		},
		"required": []string{"query", "item_id"},
	}
}

func repairSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"check": map[string]string{"type": "boolean"},
			"fix":   map[string]string{"type": "string", "description": "What to fix: indexes, links, recall_history"},
		},
	}
}

func transcriptSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"events": map[string]interface{}{
				"type":        "array",
				"description": "List of events to ingest",
				"items":       eventSchema(),
			},
		},
		"required": []string{"events"},
	}
}

func updateMemorySchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id":                map[string]string{"type": "string", "description": "Memory ID to update"},
			"body":              map[string]string{"type": "string", "description": "Updated memory body"},
			"tight_description": map[string]string{"type": "string", "description": "Updated one-line summary"},
			"status":            map[string]string{"type": "string", "description": "Memory status: active, superseded, archived, retracted"},
		},
		"required": []string{"id"},
	}
}

// Unused but required for interface compliance — suppress linter.
var _ = time.Now
