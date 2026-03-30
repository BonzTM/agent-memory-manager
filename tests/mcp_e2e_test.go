//go:build e2e

package tests

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMCPE2E_Init(t *testing.T) {
	server := startMCPServer(t)

	resultRaw, err := sendToolCall(server, "amm_init", map[string]interface{}{})
	if err != nil {
		t.Fatalf("amm_init failed: %v", err)
	}

	var result struct {
		Message string `json:"message"`
		Status  struct {
			Initialized bool `json:"initialized"`
		} `json:"status"`
	}
	if err := json.Unmarshal(resultRaw, &result); err != nil {
		t.Fatalf("decode amm_init result: %v", err)
	}
	if !result.Status.Initialized {
		t.Fatalf("expected initialized=true, got: %s", string(resultRaw))
	}
	if result.Message == "" {
		t.Fatalf("expected non-empty init message, got: %s", string(resultRaw))
	}
}

func TestMCPE2E_RememberAndRecall(t *testing.T) {
	server := startMCPServer(t)

	factText := "mcp-e2e fact " + time.Now().UTC().Format(time.RFC3339Nano)
	_, err := sendToolCall(server, "amm_remember", map[string]interface{}{
		"type":              "fact",
		"body":              factText,
		"tight_description": factText,
	})
	if err != nil {
		t.Fatalf("amm_remember failed: %v", err)
	}

	recallRaw, err := sendToolCall(server, "amm_recall", map[string]interface{}{
		"query": factText,
		"opts": map[string]interface{}{
			"mode":  "hybrid",
			"limit": 10,
		},
	})
	if err != nil {
		t.Fatalf("amm_recall failed: %v", err)
	}

	recallText := strings.ToLower(string(recallRaw))
	if !strings.Contains(recallText, strings.ToLower(factText)) {
		t.Fatalf("expected recall to contain remembered fact %q, got: %s", factText, string(recallRaw))
	}
}

func TestMCPE2E_IngestEvent(t *testing.T) {
	server := startMCPServer(t)

	eventRaw, err := sendToolCall(server, "amm_ingest_event", map[string]interface{}{
		"kind":          "message_user",
		"source_system": "test",
		"content":       "mcp e2e ingest event",
	})
	if err != nil {
		t.Fatalf("amm_ingest_event failed: %v", err)
	}

	var eventResult struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(eventRaw, &eventResult); err != nil {
		t.Fatalf("decode ingest_event result: %v", err)
	}
	if eventResult.ID == "" {
		t.Fatalf("expected event id in ingest_event result, got: %s", string(eventRaw))
	}
}

func TestMCPE2E_Status(t *testing.T) {
	server := startMCPServer(t)

	statusRaw, err := sendToolCall(server, "amm_status", map[string]interface{}{})
	if err != nil {
		t.Fatalf("amm_status failed: %v", err)
	}

	var statusMap map[string]interface{}
	if err := json.Unmarshal(statusRaw, &statusMap); err != nil {
		t.Fatalf("decode status result: %v", err)
	}
	if _, ok := statusMap["event_count"]; !ok {
		t.Fatalf("expected event_count in status result, got: %s", string(statusRaw))
	}
	if _, ok := statusMap["memory_count"]; !ok {
		t.Fatalf("expected memory_count in status result, got: %s", string(statusRaw))
	}
}

func TestMCPE2E_ExpandDelegationDepthBlocked(t *testing.T) {
	server := startMCPServer(t)

	factText := "mcp-e2e expand depth guard " + time.Now().UTC().Format(time.RFC3339Nano)
	rememberRaw, err := sendToolCall(server, "amm_remember", map[string]interface{}{
		"type":              "fact",
		"body":              factText,
		"tight_description": factText,
	})
	if err != nil {
		t.Fatalf("amm_remember failed: %v", err)
	}

	var remembered struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rememberRaw, &remembered); err != nil {
		t.Fatalf("decode remember result: %v", err)
	}
	if remembered.ID == "" {
		t.Fatalf("expected remembered id, got: %s", string(rememberRaw))
	}

	_, err = sendToolCall(server, "amm_expand", map[string]interface{}{
		"id":               remembered.ID,
		"kind":             "memory",
		"delegation_depth": 99,
	})
	if err == nil {
		t.Fatal("expected amm_expand to be blocked by delegation depth")
	}

	errText := err.Error()
	if !strings.Contains(errText, "EXPANSION_RECURSION_BLOCKED") && !strings.Contains(strings.ToLower(errText), "recursion blocked") {
		t.Fatalf("expected blocked expansion marker in error, got: %q", errText)
	}
}

func TestMCPE2E_FormatContextWindow(t *testing.T) {
	server := startMCPServer(t)

	sessionID := "mcp-e2e-context-" + time.Now().UTC().Format("20060102T150405.000000000")
	for i := 0; i < 4; i++ {
		_, err := sendToolCall(server, "amm_ingest_event", map[string]interface{}{
			"kind":          "message_user",
			"source_system": "test",
			"session_id":    sessionID,
			"content":       "context window event " + time.Now().UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			t.Fatalf("amm_ingest_event failed: %v", err)
		}
	}

	resultRaw, err := sendToolCall(server, "amm_format_context_window", map[string]interface{}{
		"session_id": sessionID,
	})
	if err != nil {
		t.Fatalf("amm_format_context_window failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resultRaw, &result); err != nil {
		t.Fatalf("decode format_context_window result: %v", err)
	}
	for _, key := range []string{"content", "summary_count", "fresh_count", "est_tokens"} {
		if _, ok := result[key]; !ok {
			t.Fatalf("expected %s in format_context_window result, got: %s", key, string(resultRaw))
		}
	}
}

func TestMCPE2E_Grep(t *testing.T) {
	server := startMCPServer(t)

	sessionID := "mcp-e2e-grep-" + time.Now().UTC().Format("20060102T150405.000000000")
	pattern := "alpha_unique_test_pattern"

	for i := 0; i < 3; i++ {
		_, err := sendToolCall(server, "amm_ingest_event", map[string]interface{}{
			"kind":          "message_user",
			"source_system": "test",
			"session_id":    sessionID,
			"content":       "event contains " + pattern + " " + time.Now().UTC().Format(time.RFC3339Nano),
		})
		if err != nil {
			t.Fatalf("amm_ingest_event failed: %v", err)
		}
	}

	resultRaw, err := sendToolCall(server, "amm_grep", map[string]interface{}{
		"pattern":    pattern,
		"session_id": sessionID,
	})
	if err != nil {
		t.Fatalf("amm_grep failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resultRaw, &result); err != nil {
		t.Fatalf("decode grep result: %v", err)
	}
	for _, key := range []string{"pattern", "total_hits", "groups"} {
		if _, ok := result[key]; !ok {
			t.Fatalf("expected %s in grep result, got: %s", key, string(resultRaw))
		}
	}
}
