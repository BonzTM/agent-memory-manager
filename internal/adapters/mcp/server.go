package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/buildinfo"
	"github.com/bonztm/agent-memory-manager/internal/core"
	"github.com/bonztm/agent-memory-manager/internal/runtime"
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

// Tool describes a single MCP-exposed amm tool, including its name,
// human-readable description, and JSON schema input contract.
type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

// Serve runs the amm MCP server over stdin/stdout using JSON-RPC and dispatches
// incoming tool calls through the shared service layer.
func Serve() error {
	cfg := runtime.LoadConfigWithEnv()
	slog.Debug("mcp server start", "db_path", cfg.Storage.DBPath, "version", Version)

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
	slog.Debug("mcp handle request", "method", req.Method)
	switch req.Method {
	case "initialize":
		return jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]string{
					"name":    "amm-mcp",
					"version": buildinfo.Version,
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
		slog.Error("mcp tool call failed", "tool", "", "error", err)
		return jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &rpcError{Code: -32602, Message: "invalid params"},
		}
	}

	ctx := context.Background()
	slog.Debug("mcp tool call start", "tool", params.Name)
	result, callErr := dispatchTool(ctx, svc, params.Name, params.Arguments)
	if callErr != nil {
		var argErr invalidToolArgsError
		if errors.As(callErr, &argErr) {
			slog.Error("mcp tool call failed", "tool", params.Name, "error", argErr)
			return errorResponse(req.ID, -32602, fmt.Sprintf("invalid arguments for %s: %v", params.Name, argErr))
		}
		if errors.Is(callErr, errUnknownTool) {
			slog.Error("mcp tool call failed", "tool", params.Name, "error", fmt.Sprintf("unknown tool: %s", params.Name))
			return errorResponse(req.ID, -32602, fmt.Sprintf("unknown tool: %s", params.Name))
		}
	}

	if callErr != nil {
		slog.Error("mcp tool call failed", "tool", params.Name, "error", callErr)
		errText := fmt.Sprintf("error: %v", callErr)
		if errors.Is(callErr, core.ErrExpansionRecursionBlocked) {
			errText = fmt.Sprintf("EXPANSION_RECURSION_BLOCKED: %v", callErr)
		}
		return jsonrpcResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"content": []map[string]string{{"type": "text", "text": errText}},
				"isError": true,
			},
		}
	}

	resultJSON, _ := json.Marshal(result)
	slog.Debug("mcp tool call succeeded", "tool", params.Name)
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
			"agent_id":          map[string]string{"type": "string", "description": "Agent identifier"},
		},
		"required": []string{"type", "body", "tight_description"},
	}
}

func recallSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query":    map[string]string{"type": "string", "description": "Search query"},
			"agent_id": map[string]string{"type": "string", "description": "Agent identifier"},
			"explain":  map[string]string{"type": "boolean", "description": "Include score signal breakdowns in each recall item"},
			"opts": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"mode":       map[string]string{"type": "string"},
					"project_id": map[string]string{"type": "string"},
					"session_id": map[string]string{"type": "string"},
					"agent_id":   map[string]string{"type": "string"},
					"limit":      map[string]string{"type": "integer"},
					"explain":    map[string]string{"type": "boolean"},
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
			"id":         map[string]string{"type": "string", "description": "Item ID to expand"},
			"kind":       map[string]string{"type": "string", "description": "Item kind: memory, summary, episode (defaults to 'memory' if omitted)"},
			"session_id": map[string]string{"type": "string", "description": "Session identifier for relevance feedback"},
			"delegation_depth": map[string]interface{}{
				"type":        "integer",
				"minimum":     0,
				"description": "Current delegation depth for recursion control",
			},
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

func grepSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern":           map[string]string{"type": "string"},
			"session_id":        map[string]string{"type": "string"},
			"project_id":        map[string]string{"type": "string"},
			"max_group_depth":   map[string]string{"type": "integer"},
			"group_limit":       map[string]string{"type": "integer"},
			"matches_per_group": map[string]string{"type": "integer"},
		},
		"required": []string{"pattern"},
	}
}

func formatContextWindowSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id":          map[string]string{"type": "string"},
			"project_id":          map[string]string{"type": "string"},
			"fresh_tail_count":    map[string]string{"type": "integer"},
			"max_summary_depth":   map[string]string{"type": "integer"},
			"include_parent_refs": map[string]string{"type": "boolean"},
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

func shareSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id":      map[string]string{"type": "string", "description": "Memory ID to share"},
			"privacy": map[string]string{"type": "string", "description": "Privacy level: private, shared, public_safe"},
		},
		"required": []string{"id", "privacy"},
	}
}

func policyListSchema() map[string]interface{} {
	return emptySchema()
}

func policyAddSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"pattern_type": map[string]string{"type": "string", "description": "Pattern type: kind, session, source, surface, agent, project, runtime"},
			"pattern":      map[string]string{"type": "string", "description": "Policy pattern"},
			"mode":         map[string]string{"type": "string", "description": "Ingestion mode: full, read_only, ignore"},
			"priority":     map[string]string{"type": "integer", "description": "Priority ordering (higher wins)"},
			"match_mode":   map[string]string{"type": "string", "description": "Pattern match mode: exact, glob, regex"},
		},
		"required": []string{"pattern_type", "pattern", "mode"},
	}
}

func policyRemoveSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]string{"type": "string", "description": "Policy ID to remove"},
		},
		"required": []string{"id"},
	}
}

func projectSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":        map[string]string{"type": "string", "description": "Project name"},
			"path":        map[string]string{"type": "string", "description": "Project path"},
			"description": map[string]string{"type": "string", "description": "Project description"},
		},
		"required": []string{"name"},
	}
}

func listRelationshipsSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"entity_id":         map[string]string{"type": "string", "description": "Filter by entity ID"},
			"relationship_type": map[string]string{"type": "string", "description": "Filter by relationship type"},
			"limit":             map[string]string{"type": "integer", "description": "Max results to return"},
		},
	}
}

func addRelationshipSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"from_entity_id":    map[string]string{"type": "string", "description": "Source entity ID"},
			"to_entity_id":      map[string]string{"type": "string", "description": "Destination entity ID"},
			"relationship_type": map[string]string{"type": "string", "description": "Relationship type"},
		},
		"required": []string{"from_entity_id", "to_entity_id", "relationship_type"},
	}
}

func resetDerivedSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"confirm": map[string]string{"type": "boolean"},
		},
		"required": []string{"confirm"},
	}
}

// Unused but required for interface compliance — suppress linter.
var _ = time.Now
