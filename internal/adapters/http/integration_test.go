package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	nethttp "net/http"
	"strings"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/adapters/sqlite"
	"github.com/bonztm/agent-memory-manager/internal/core"
)

func startTestServer(t *testing.T) (string, *sqlite.SQLiteRepository) {
	t.Helper()
	srv, repo, _ := testHTTPEnv(t)
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		if err := srv.Serve(ln); err != nil {
			t.Errorf("serve: %v", err)
		}
	}()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	addr := ln.Addr().(*net.TCPAddr)
	return fmt.Sprintf("http://localhost:%d", addr.Port), repo
}

func doRequest(t *testing.T, method, url string, body interface{}) *nethttp.Response {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := nethttp.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := nethttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeData(t *testing.T, resp *nethttp.Response, out interface{}) {
	t.Helper()
	defer resp.Body.Close()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		t.Fatal(err)
	}
}

func mustStatus(t *testing.T, resp *nethttp.Response, want int) {
	t.Helper()
	defer resp.Body.Close()
	if resp.StatusCode != want {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d want=%d body=%s", resp.StatusCode, want, string(b))
	}
}

func TestIntegration_FullLifecycle(t *testing.T) {
	baseURL, repo := startTestServer(t)
	ctx := context.Background()
	now := time.Now().UTC()

	resp := doRequest(t, nethttp.MethodGet, baseURL+"/healthz", nil)
	func() {
		defer resp.Body.Close()
		if resp.StatusCode != nethttp.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("healthz status=%d body=%s", resp.StatusCode, string(b))
		}
		b, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(b), "ok") {
			t.Fatalf("healthz body=%s", string(b))
		}
	}()

	resp = doRequest(t, nethttp.MethodGet, baseURL+"/v1/status", nil)
	var statusResult core.StatusResult
	decodeData(t, resp, &statusResult)
	if statusResult.DBPath == "" {
		t.Fatal("expected status data")
	}

	eventResp := doRequest(t, nethttp.MethodPost, baseURL+"/v1/events", map[string]any{
		"kind":          "message_user",
		"source_system": "integration",
		"content":       "integration event body",
		"occurred_at":   now,
	})
	var event core.Event
	decodeData(t, eventResp, &event)
	if event.ID == "" {
		t.Fatal("expected event id")
	}

	rememberResp := doRequest(t, nethttp.MethodPost, baseURL+"/v1/memories", map[string]any{
		"type":              string(core.MemoryTypeFact),
		"scope":             string(core.ScopeGlobal),
		"body":              "integration lifecycle memory",
		"tight_description": "integration lifecycle memory",
		"subject":           "integration",
	})
	var memory core.Memory
	decodeData(t, rememberResp, &memory)
	if memory.ID == "" || memory.Type != core.MemoryTypeFact {
		t.Fatalf("unexpected memory: %+v", memory)
	}

	getResp := doRequest(t, nethttp.MethodGet, baseURL+"/v1/memories/"+memory.ID, nil)
	var gotMemory core.Memory
	decodeData(t, getResp, &gotMemory)
	if gotMemory.ID != memory.ID || gotMemory.Body != memory.Body || gotMemory.Type != memory.Type {
		t.Fatalf("memory mismatch: got=%+v want=%+v", gotMemory, memory)
	}

	updated := gotMemory
	updated.Body = "integration lifecycle memory updated"
	updated.TightDescription = "integration lifecycle memory updated"
	updateResp := doRequest(t, nethttp.MethodPatch, baseURL+"/v1/memories/"+memory.ID, updated)
	decodeData(t, updateResp, &gotMemory)
	if gotMemory.Body != updated.Body {
		t.Fatalf("expected updated body, got %q", gotMemory.Body)
	}

	getResp = doRequest(t, nethttp.MethodGet, baseURL+"/v1/memories/"+memory.ID, nil)
	decodeData(t, getResp, &gotMemory)
	if gotMemory.Body != updated.Body {
		t.Fatalf("expected persisted update, got %q", gotMemory.Body)
	}

	recallResp := doRequest(t, nethttp.MethodPost, baseURL+"/v1/recall", map[string]any{
		"query": "integration lifecycle memory updated",
		"opts": map[string]any{
			"mode":  string(core.RecallModeFacts),
			"limit": 10,
		},
	})
	var recallResult core.RecallResult
	decodeData(t, recallResp, &recallResult)
	found := false
	for _, item := range recallResult.Items {
		if item.ID == memory.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected recall to include memory %s", memory.ID)
	}

	shareResp := doRequest(t, nethttp.MethodPatch, baseURL+"/v1/memories/"+memory.ID+"/share", map[string]any{
		"privacy": string(core.PrivacyShared),
	})
	decodeData(t, shareResp, &gotMemory)
	if gotMemory.PrivacyLevel != core.PrivacyShared {
		t.Fatalf("expected shared privacy, got %q", gotMemory.PrivacyLevel)
	}

	forgetResp := doRequest(t, nethttp.MethodDelete, baseURL+"/v1/memories/"+memory.ID, nil)
	decodeData(t, forgetResp, &gotMemory)
	if gotMemory.Status != core.MemoryStatusRetracted {
		t.Fatalf("expected retracted memory, got %q", gotMemory.Status)
	}

	getResp = doRequest(t, nethttp.MethodGet, baseURL+"/v1/memories/"+memory.ID, nil)
	decodeData(t, getResp, &gotMemory)
	if gotMemory.Status != core.MemoryStatusRetracted {
		t.Fatalf("expected retracted status, got %q", gotMemory.Status)
	}

	policyResp := doRequest(t, nethttp.MethodPost, baseURL+"/v1/policies", map[string]any{
		"pattern_type": "source",
		"pattern":      "integration-*",
		"mode":         "full",
	})
	var policy core.IngestionPolicy
	decodeData(t, policyResp, &policy)
	if policy.ID == "" {
		t.Fatal("expected policy id")
	}

	listPoliciesResp := doRequest(t, nethttp.MethodGet, baseURL+"/v1/policies", nil)
	var policies []core.IngestionPolicy
	decodeData(t, listPoliciesResp, &policies)
	policyFound := false
	for _, p := range policies {
		if p.ID == policy.ID {
			policyFound = true
			break
		}
	}
	if !policyFound {
		t.Fatalf("expected policy %s in list", policy.ID)
	}

	removePolicyResp := doRequest(t, nethttp.MethodDelete, baseURL+"/v1/policies/"+policy.ID, nil)
	mustStatus(t, removePolicyResp, nethttp.StatusNoContent)

	projectResp := doRequest(t, nethttp.MethodPost, baseURL+"/v1/projects", map[string]any{
		"name":        "integration project",
		"description": "integration test project",
	})
	var project core.Project
	decodeData(t, projectResp, &project)
	if project.ID == "" {
		t.Fatal("expected project id")
	}

	getProjectResp := doRequest(t, nethttp.MethodGet, baseURL+"/v1/projects/"+project.ID, nil)
	var gotProject core.Project
	decodeData(t, getProjectResp, &gotProject)
	if gotProject.ID != project.ID || gotProject.Name != project.Name {
		t.Fatalf("project mismatch: got=%+v want=%+v", gotProject, project)
	}

	listProjectsResp := doRequest(t, nethttp.MethodGet, baseURL+"/v1/projects", nil)
	var projects []core.Project
	decodeData(t, listProjectsResp, &projects)
	projectFound := false
	for _, p := range projects {
		if p.ID == project.ID {
			projectFound = true
			break
		}
	}
	if !projectFound {
		t.Fatalf("expected project %s in list", project.ID)
	}

	removeProjectResp := doRequest(t, nethttp.MethodDelete, baseURL+"/v1/projects/"+project.ID, nil)
	mustStatus(t, removeProjectResp, nethttp.StatusNoContent)

	entityNow := time.Now().UTC()
	entityA := &core.Entity{ID: "ent_http_a", Type: "person", CanonicalName: "Entity A", Description: "integration entity A", CreatedAt: entityNow, UpdatedAt: entityNow}
	entityB := &core.Entity{ID: "ent_http_b", Type: "person", CanonicalName: "Entity B", Description: "integration entity B", CreatedAt: entityNow, UpdatedAt: entityNow}
	if err := repo.InsertEntity(ctx, entityA); err != nil {
		t.Fatalf("insert entity A: %v", err)
	}
	if err := repo.InsertEntity(ctx, entityB); err != nil {
		t.Fatalf("insert entity B: %v", err)
	}
	relResp := doRequest(t, nethttp.MethodPost, baseURL+"/v1/relationships", map[string]any{
		"from_entity_id":    entityA.ID,
		"to_entity_id":      entityB.ID,
		"relationship_type": "related_to",
	})
	var rel core.Relationship
	decodeData(t, relResp, &rel)
	if rel.ID == "" {
		t.Fatal("expected relationship id")
	}

	relGetResp := doRequest(t, nethttp.MethodGet, baseURL+"/v1/relationships/"+rel.ID, nil)
	var gotRel core.Relationship
	decodeData(t, relGetResp, &gotRel)
	if gotRel.ID != rel.ID || gotRel.FromEntityID != entityA.ID || gotRel.ToEntityID != entityB.ID {
		t.Fatalf("relationship mismatch: got=%+v want=%+v", gotRel, rel)
	}

	relListResp := doRequest(t, nethttp.MethodGet, baseURL+"/v1/relationships?entity_id="+entityA.ID+"&relationship_type=related_to&limit=10", nil)
	var rels []core.Relationship
	decodeData(t, relListResp, &rels)
	relFound := false
	for _, candidate := range rels {
		if candidate.ID == rel.ID {
			relFound = true
			break
		}
	}
	if !relFound {
		t.Fatalf("expected relationship %s in list", rel.ID)
	}

	relRemoveResp := doRequest(t, nethttp.MethodDelete, baseURL+"/v1/relationships/"+rel.ID, nil)
	mustStatus(t, relRemoveResp, nethttp.StatusNoContent)

	notFoundResp := doRequest(t, nethttp.MethodGet, baseURL+"/v1/memories/nonexistent", nil)
	mustStatus(t, notFoundResp, nethttp.StatusNotFound)

	badJSONReq, err := nethttp.NewRequest(nethttp.MethodPost, baseURL+"/v1/memories", strings.NewReader("{"))
	if err != nil {
		t.Fatal(err)
	}
	badJSONReq.Header.Set("Content-Type", "application/json")
	badJSONResp, err := nethttp.DefaultClient.Do(badJSONReq)
	if err != nil {
		t.Fatal(err)
	}
	mustStatus(t, badJSONResp, nethttp.StatusBadRequest)
}
