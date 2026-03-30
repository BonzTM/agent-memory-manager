package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

type mcpJSONRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      any              `json:"id,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *mcpJSONRPCError `json:"error,omitempty"`
}

type mcpJSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpToolCallResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

type mcpHTTPSession struct {
	t        *testing.T
	client   *nethttp.Client
	endpoint string
	id       int
	session  string
}

func newMCPHTTPSession(t *testing.T, endpoint string, client *nethttp.Client) *mcpHTTPSession {
	t.Helper()
	s := &mcpHTTPSession{t: t, endpoint: endpoint, client: client, id: 1}
	s.initialize()
	return s
}

func (s *mcpHTTPSession) initialize() {
	s.t.Helper()
	resp := s.call(map[string]any{
		"jsonrpc": "2.0",
		"id":      s.nextID(),
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "amm-http-e2e",
				"version": "1.0.0",
			},
		},
	})
	if resp.Error != nil {
		s.t.Fatalf("initialize error: code=%d message=%s", resp.Error.Code, resp.Error.Message)
	}
	if len(resp.Result) == 0 {
		s.t.Fatal("initialize missing result")
	}

	_ = s.callNotification("notifications/initialized", map[string]any{})
}

func callMCPTool[T any](s *mcpHTTPSession, name string, args map[string]any) T {
	s.t.Helper()
	toolResp := callMCPToolEnvelope(s, name, args)
	if toolResp.IsError {
		s.t.Fatalf("tool %s returned error: %s", name, firstToolText(toolResp.Content))
	}

	var out T
	if err := json.Unmarshal([]byte(firstToolText(toolResp.Content)), &out); err != nil {
		s.t.Fatalf("tool %s decode content text: %v", name, err)
	}
	return out
}

func callMCPToolEnvelope(s *mcpHTTPSession, name string, args map[string]any) mcpToolCallResponse {
	s.t.Helper()
	resp := s.call(map[string]any{
		"jsonrpc": "2.0",
		"id":      s.nextID(),
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": args,
		},
	})
	if resp.Error != nil {
		s.t.Fatalf("tool %s jsonrpc error: code=%d message=%s", name, resp.Error.Code, resp.Error.Message)
	}

	var toolResp mcpToolCallResponse
	if err := json.Unmarshal(resp.Result, &toolResp); err != nil {
		s.t.Fatalf("tool %s decode result: %v", name, err)
	}
	return toolResp
}

func (s *mcpHTTPSession) callNotification(method string, params map[string]any) *mcpJSONRPCResponse {
	s.t.Helper()
	return s.call(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
	})
}

func (s *mcpHTTPSession) call(payload map[string]any) *mcpJSONRPCResponse {
	s.t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		s.t.Fatalf("marshal jsonrpc payload: %v", err)
	}

	req, err := nethttp.NewRequest(nethttp.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		s.t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if s.session != "" {
		req.Header.Set("Mcp-Session-Id", s.session)
	}

	httpResp, err := s.client.Do(req)
	if err != nil {
		s.t.Fatalf("post /v1/mcp: %v", err)
	}
	defer httpResp.Body.Close()

	if session := httpResp.Header.Get("Mcp-Session-Id"); session != "" {
		s.session = session
	}

	if httpResp.StatusCode != nethttp.StatusOK && httpResp.StatusCode != nethttp.StatusAccepted && httpResp.StatusCode != nethttp.StatusNoContent {
		s.t.Fatalf("unexpected status=%d", httpResp.StatusCode)
	}

	if httpResp.StatusCode == nethttp.StatusAccepted || httpResp.StatusCode == nethttp.StatusNoContent {
		return &mcpJSONRPCResponse{}
	}

	var resp mcpJSONRPCResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		s.t.Fatalf("decode jsonrpc response: %v", err)
	}
	if resp.JSONRPC != "2.0" {
		s.t.Fatalf("jsonrpc=%q want=2.0", resp.JSONRPC)
	}
	return &resp
}

func (s *mcpHTTPSession) nextID() int {
	id := s.id
	s.id++
	return id
}

func firstToolText(content []struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}) string {
	for _, part := range content {
		if part.Type == "text" && part.Text != "" {
			return part.Text
		}
	}
	return ""
}

func TestMCPBridgeE2E_Init(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	ts := httptest.NewServer(srv.server.Handler)
	t.Cleanup(ts.Close)

	session := newMCPHTTPSession(t, ts.URL+"/v1/mcp", ts.Client())

	payload := callMCPTool[map[string]any](session, "amm_init", map[string]any{})
	if payload["status"] == nil {
		t.Fatalf("missing status in init payload: %+v", payload)
	}
}

func TestMCPBridgeE2E_RememberAndRecall(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	ts := httptest.NewServer(srv.server.Handler)
	t.Cleanup(ts.Close)

	session := newMCPHTTPSession(t, ts.URL+"/v1/mcp", ts.Client())
	fact := fmt.Sprintf("mcp e2e remember recall %d", time.Now().UnixNano())

	remembered := callMCPTool[core.Memory](session, "amm_remember", map[string]any{
		"type":              string(core.MemoryTypeFact),
		"scope":             string(core.ScopeGlobal),
		"body":              fact,
		"tight_description": fact,
	})
	if remembered.ID == "" {
		t.Fatal("expected remembered memory id")
	}

	recalled := callMCPTool[core.RecallResult](session, "amm_recall", map[string]any{
		"query": fact,
		"opts": map[string]any{
			"mode":  string(core.RecallModeFacts),
			"limit": 10,
		},
	})

	found := false
	for _, item := range recalled.Items {
		if strings.Contains(item.TightDescription, fact) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected recall to contain remembered fact %q", fact)
	}
}

func TestMCPBridgeE2E_IngestEvent(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	ts := httptest.NewServer(srv.server.Handler)
	t.Cleanup(ts.Close)

	session := newMCPHTTPSession(t, ts.URL+"/v1/mcp", ts.Client())

	event := callMCPTool[core.Event](session, "amm_ingest_event", map[string]any{
		"kind":          "message_user",
		"source_system": "test",
		"content":       "mcp e2e ingest event",
	})
	if event.ID == "" || event.Kind != "message_user" {
		t.Fatalf("unexpected event: %+v", event)
	}

	status := callMCPTool[core.StatusResult](session, "amm_status", map[string]any{})
	if status.EventCount < 1 {
		t.Fatalf("expected event_count >= 1, got %d", status.EventCount)
	}
}

func TestMCPBridgeE2E_Status(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	ts := httptest.NewServer(srv.server.Handler)
	t.Cleanup(ts.Close)

	session := newMCPHTTPSession(t, ts.URL+"/v1/mcp", ts.Client())

	status := callMCPTool[core.StatusResult](session, "amm_status", map[string]any{})
	if status.DBPath == "" {
		t.Fatal("expected status db_path")
	}
	if status.EventCount < 0 || status.MemoryCount < 0 || status.SummaryCount < 0 || status.EpisodeCount < 0 || status.EntityCount < 0 {
		t.Fatalf("expected non-negative status counts: %+v", status)
	}
}

func TestMCPBridgeE2E_ExpandDelegationDepthBlocked(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	ts := httptest.NewServer(srv.server.Handler)
	t.Cleanup(ts.Close)

	session := newMCPHTTPSession(t, ts.URL+"/v1/mcp", ts.Client())
	fact := fmt.Sprintf("mcp bridge expand depth guard %d", time.Now().UnixNano())

	remembered := callMCPTool[core.Memory](session, "amm_remember", map[string]any{
		"type":              string(core.MemoryTypeFact),
		"body":              fact,
		"tight_description": fact,
	})
	if remembered.ID == "" {
		t.Fatal("expected remembered memory id")
	}

	resp := callMCPToolEnvelope(session, "amm_expand", map[string]any{
		"id":               remembered.ID,
		"kind":             "memory",
		"delegation_depth": 99,
	})
	if !resp.IsError {
		t.Fatalf("expected isError=true for blocked expansion, got %+v", resp)
	}
	errText := firstToolText(resp.Content)
	if !strings.Contains(errText, "EXPANSION_RECURSION_BLOCKED") && !strings.Contains(strings.ToLower(errText), "recursion blocked") {
		t.Fatalf("expected blocked expansion marker in text, got: %q", errText)
	}
}

func TestMCPBridgeE2E_FormatContextWindow(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)
	ts := httptest.NewServer(srv.server.Handler)
	t.Cleanup(ts.Close)

	session := newMCPHTTPSession(t, ts.URL+"/v1/mcp", ts.Client())
	sessionID := fmt.Sprintf("mcp-bridge-context-%d", time.Now().UnixNano())

	for i := 0; i < 4; i++ {
		_ = callMCPTool[core.Event](session, "amm_ingest_event", map[string]any{
			"kind":          "message_user",
			"source_system": "test",
			"session_id":    sessionID,
			"content":       fmt.Sprintf("context window event %d", i),
		})
	}

	result := callMCPTool[core.ContextWindowResult](session, "amm_format_context_window", map[string]any{
		"session_id": sessionID,
	})

	if result.Content == "" {
		t.Fatal("expected non-empty content from format_context_window")
	}
	if result.SummaryCount < 0 || result.FreshCount < 0 || result.EstTokens < 0 {
		t.Fatalf("expected non-negative context window counters: %+v", result)
	}
}

func TestMCPBridgeE2E_Grep(t *testing.T) {
	srv, repo, ctx := testHTTPEnv(t)
	ts := httptest.NewServer(srv.server.Handler)
	t.Cleanup(ts.Close)

	session := newMCPHTTPSession(t, ts.URL+"/v1/mcp", ts.Client())
	sessionID := fmt.Sprintf("mcp-bridge-grep-a-%d", time.Now().UnixNano())
	otherSessionID := fmt.Sprintf("mcp-bridge-grep-b-%d", time.Now().UnixNano())
	pattern := "alpha_unique_test_pattern"

	seedGroup := func(summaryID, eventID, groupSessionID string) *core.Event {
		now := time.Now().UTC().Truncate(time.Second)
		sum := &core.Summary{
			ID:               summaryID,
			Kind:             "leaf",
			Depth:            0,
			Scope:            core.ScopeSession,
			SessionID:        groupSessionID,
			Body:             summaryID + " body",
			TightDescription: summaryID + " tight",
			PrivacyLevel:     core.PrivacyPrivate,
			CreatedAt:        now,
			UpdatedAt:        now,
		}
		sum.ProjectID = "prj_grep_mcp"
		if err := repo.InsertSummary(ctx, sum); err != nil {
			t.Fatalf("insert summary %s: %v", summaryID, err)
		}

		evtNow := time.Now().UTC().Truncate(time.Second)
		evt := &core.Event{
			ID:           eventID,
			Kind:         "message_user",
			SourceSystem: "test",
			SessionID:    groupSessionID,
			ProjectID:    "prj_grep_mcp",
			PrivacyLevel: core.PrivacyPrivate,
			Content:      fmt.Sprintf("grep event %s has %s", eventID, pattern),
			OccurredAt:   evtNow,
			IngestedAt:   evtNow,
		}
		if err := repo.InsertEvent(ctx, evt); err != nil {
			t.Fatalf("insert event %s: %v", eventID, err)
		}
		if err := repo.InsertSummaryEdge(ctx, &core.SummaryEdge{ParentSummaryID: sum.ID, ChildKind: "event", ChildID: evt.ID, EdgeOrder: 0}); err != nil {
			t.Fatalf("insert summary edge %s: %v", eventID, err)
		}
		return evt
	}

	a1 := seedGroup("sum_grep_mcp_a1", "evt_grep_mcp_a1", sessionID)
	a2 := seedGroup("sum_grep_mcp_a2", "evt_grep_mcp_a2", sessionID)
	b1 := seedGroup("sum_grep_mcp_b1", "evt_grep_mcp_b1", otherSessionID)

	result := callMCPTool[core.GrepResult](session, "amm_grep", map[string]any{
		"pattern":     pattern,
		"session_id":  sessionID,
		"group_limit": 1,
	})

	if result.Pattern != pattern {
		t.Fatalf("expected grep pattern %q, got %q", pattern, result.Pattern)
	}
	if result.TotalHits != 2 {
		t.Fatalf("expected total_hits=2 for session filter, got %d", result.TotalHits)
	}
	if len(result.Groups) != 1 {
		t.Fatalf("expected group_limit=1 to cap groups, got %+v", result)
	}
	for _, group := range result.Groups {
		for _, match := range group.Matches {
			if match.EventID != a1.ID && match.EventID != a2.ID {
				t.Fatalf("expected only session %s events, got match %+v", sessionID, match)
			}
			if match.EventID == b1.ID {
				t.Fatalf("unexpected other-session event in results: %+v", match)
			}
		}
	}
}
