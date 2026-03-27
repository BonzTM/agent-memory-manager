package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	nethttp "net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/bonztm/agent-memory-manager/internal/adapters/sqlite"
	"github.com/bonztm/agent-memory-manager/internal/core"
	"github.com/bonztm/agent-memory-manager/internal/service"
)

func testHTTPEnv(t *testing.T) (*Server, *sqlite.SQLiteRepository, context.Context) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	ctx := context.Background()
	db, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := &sqlite.SQLiteRepository{DB: db}
	svc := service.New(repo, dbPath, nil, nil)
	return NewServer(svc, Config{Addr: ":0"}), repo, ctx
}

func jsonRequest(t *testing.T, method, target string, payload interface{}) *nethttp.Request {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(method, target, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func mustDecodeData[T any](t *testing.T, body []byte) T {
	t.Helper()
	var wrapped struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		t.Fatalf("decode data: %v body=%s", err, string(body))
	}
	return wrapped.Data
}

func mustRemember(t *testing.T, srv *Server) core.Memory {
	t.Helper()
	w := httptest.NewRecorder()
	req := jsonRequest(t, nethttp.MethodPost, "/v1/memories", map[string]interface{}{
		"type":              string(core.MemoryTypeFact),
		"scope":             string(core.ScopeGlobal),
		"body":              "remembered body",
		"tight_description": "remembered",
	})
	srv.handleRemember(w, req)
	if w.Code != nethttp.StatusCreated {
		t.Fatalf("remember status=%d body=%s", w.Code, w.Body.String())
	}
	return mustDecodeData[core.Memory](t, w.Body.Bytes())
}

func TestHandlersHappyPaths(t *testing.T) {
	srv, repo, ctx := testHTTPEnv(t)

	now := time.Now().UTC()
	entity := &core.Entity{
		ID:            "ent_test",
		Type:          "person",
		CanonicalName: "Alice",
		Description:   "tester",
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := repo.InsertEntity(ctx, entity); err != nil {
		t.Fatalf("insert entity: %v", err)
	}
	summary := &core.Summary{
		ID:               "sum_test",
		Kind:             "leaf",
		Scope:            core.ScopeGlobal,
		Body:             "summary body",
		TightDescription: "summary",
		PrivacyLevel:     core.PrivacyPrivate,
		SourceSpan:       core.SourceSpan{},
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.InsertSummary(ctx, summary); err != nil {
		t.Fatalf("insert summary: %v", err)
	}
	episode := &core.Episode{
		ID:               "ep_test",
		Title:            "episode",
		Summary:          "episode summary",
		TightDescription: "episode",
		Scope:            core.ScopeGlobal,
		Importance:       0.5,
		PrivacyLevel:     core.PrivacyPrivate,
		SourceSpan:       core.SourceSpan{},
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := repo.InsertEpisode(ctx, episode); err != nil {
		t.Fatalf("insert episode: %v", err)
	}

	t.Run("healthz", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(nethttp.MethodGet, "/healthz", nil)
		srv.handleHealthz(w, req)
		if w.Code != nethttp.StatusOK {
			t.Fatalf("status=%d", w.Code)
		}
	})

	t.Run("init", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := jsonRequest(t, nethttp.MethodPost, "/v1/init", map[string]interface{}{})
		srv.handleInit(w, req)
		if w.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("ingest_event", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := jsonRequest(t, nethttp.MethodPost, "/v1/events", map[string]interface{}{"kind": "message_user", "source_system": "test", "content": "hello"})
		srv.handleIngestEvent(w, req)
		if w.Code != nethttp.StatusCreated {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("ingest_transcript", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := jsonRequest(t, nethttp.MethodPost, "/v1/transcripts", map[string]interface{}{"events": []map[string]interface{}{{"kind": "message_user", "source_system": "test", "content": "one"}}})
		srv.handleIngestTranscript(w, req)
		if w.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	mem := mustRemember(t, srv)

	t.Run("get_memory", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(nethttp.MethodGet, "/v1/memories/"+mem.ID, nil)
		req.SetPathValue("id", mem.ID)
		srv.handleGetMemory(w, req)
		if w.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("update_memory", func(t *testing.T) {
		updated := mem
		updated.Body = "updated body"
		w := httptest.NewRecorder()
		req := jsonRequest(t, nethttp.MethodPatch, "/v1/memories/"+mem.ID, updated)
		req.SetPathValue("id", mem.ID)
		srv.handleUpdateMemory(w, req)
		if w.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("share_memory", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := jsonRequest(t, nethttp.MethodPatch, "/v1/memories/"+mem.ID+"/share", map[string]string{"privacy": "shared"})
		req.SetPathValue("id", mem.ID)
		srv.handleShareMemory(w, req)
		if w.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("recall", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := jsonRequest(t, nethttp.MethodPost, "/v1/recall", map[string]interface{}{"query": "remembered", "opts": map[string]interface{}{"mode": "hybrid"}})
		srv.handleRecall(w, req)
		if w.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("describe", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := jsonRequest(t, nethttp.MethodPost, "/v1/describe", map[string]interface{}{"ids": []string{mem.ID}})
		srv.handleDescribe(w, req)
		if w.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("expand", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(nethttp.MethodGet, "/v1/expand/"+mem.ID+"?kind=memory", nil)
		req.SetPathValue("id", mem.ID)
		srv.handleExpand(w, req)
		if w.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("history", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := jsonRequest(t, nethttp.MethodPost, "/v1/history", map[string]interface{}{"query": "hello", "opts": map[string]interface{}{"limit": 10}})
		srv.handleHistory(w, req)
		if w.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	var policyID string
	t.Run("policy_add_list_remove", func(t *testing.T) {
		addW := httptest.NewRecorder()
		addReq := jsonRequest(t, nethttp.MethodPost, "/v1/policies", map[string]interface{}{"pattern_type": "source", "pattern": "http-*", "mode": "full"})
		srv.handleAddPolicy(addW, addReq)
		if addW.Code != nethttp.StatusCreated {
			t.Fatalf("status=%d body=%s", addW.Code, addW.Body.String())
		}
		policy := mustDecodeData[core.IngestionPolicy](t, addW.Body.Bytes())
		policyID = policy.ID

		listW := httptest.NewRecorder()
		listReq := httptest.NewRequest(nethttp.MethodGet, "/v1/policies", nil)
		srv.handleListPolicies(listW, listReq)
		if listW.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", listW.Code, listW.Body.String())
		}

		removeW := httptest.NewRecorder()
		removeReq := httptest.NewRequest(nethttp.MethodDelete, "/v1/policies/"+policyID, nil)
		removeReq.SetPathValue("id", policyID)
		srv.handleRemovePolicy(removeW, removeReq)
		if removeW.Code != nethttp.StatusNoContent {
			t.Fatalf("status=%d body=%s", removeW.Code, removeW.Body.String())
		}
	})

	var projectID string
	t.Run("project_register_get_list_remove", func(t *testing.T) {
		registerW := httptest.NewRecorder()
		registerReq := jsonRequest(t, nethttp.MethodPost, "/v1/projects", map[string]interface{}{"name": "test project"})
		srv.handleRegisterProject(registerW, registerReq)
		if registerW.Code != nethttp.StatusCreated {
			t.Fatalf("status=%d body=%s", registerW.Code, registerW.Body.String())
		}
		project := mustDecodeData[core.Project](t, registerW.Body.Bytes())
		projectID = project.ID

		getW := httptest.NewRecorder()
		getReq := httptest.NewRequest(nethttp.MethodGet, "/v1/projects/"+projectID, nil)
		getReq.SetPathValue("id", projectID)
		srv.handleGetProject(getW, getReq)
		if getW.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", getW.Code, getW.Body.String())
		}

		listW := httptest.NewRecorder()
		listReq := httptest.NewRequest(nethttp.MethodGet, "/v1/projects", nil)
		srv.handleListProjects(listW, listReq)
		if listW.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", listW.Code, listW.Body.String())
		}

		removeW := httptest.NewRecorder()
		removeReq := httptest.NewRequest(nethttp.MethodDelete, "/v1/projects/"+projectID, nil)
		removeReq.SetPathValue("id", projectID)
		srv.handleRemoveProject(removeW, removeReq)
		if removeW.Code != nethttp.StatusNoContent {
			t.Fatalf("status=%d body=%s", removeW.Code, removeW.Body.String())
		}
	})

	var relationshipID string
	t.Run("relationship_add_get_list_remove", func(t *testing.T) {
		addW := httptest.NewRecorder()
		addReq := jsonRequest(t, nethttp.MethodPost, "/v1/relationships", map[string]interface{}{"from_entity_id": entity.ID, "to_entity_id": entity.ID, "relationship_type": "related_to"})
		srv.handleAddRelationship(addW, addReq)
		if addW.Code != nethttp.StatusCreated {
			t.Fatalf("status=%d body=%s", addW.Code, addW.Body.String())
		}
		rel := mustDecodeData[core.Relationship](t, addW.Body.Bytes())
		relationshipID = rel.ID

		getW := httptest.NewRecorder()
		getReq := httptest.NewRequest(nethttp.MethodGet, "/v1/relationships/"+relationshipID, nil)
		getReq.SetPathValue("id", relationshipID)
		srv.handleGetRelationship(getW, getReq)
		if getW.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", getW.Code, getW.Body.String())
		}

		listW := httptest.NewRecorder()
		listReq := httptest.NewRequest(nethttp.MethodGet, "/v1/relationships?entity_id="+entity.ID+"&relationship_type=related_to&limit=5", nil)
		srv.handleListRelationships(listW, listReq)
		if listW.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", listW.Code, listW.Body.String())
		}

		removeW := httptest.NewRecorder()
		removeReq := httptest.NewRequest(nethttp.MethodDelete, "/v1/relationships/"+relationshipID, nil)
		removeReq.SetPathValue("id", relationshipID)
		srv.handleRemoveRelationship(removeW, removeReq)
		if removeW.Code != nethttp.StatusNoContent {
			t.Fatalf("status=%d body=%s", removeW.Code, removeW.Body.String())
		}
	})

	t.Run("summary_episode_entity_get", func(t *testing.T) {
		sw := httptest.NewRecorder()
		sreq := httptest.NewRequest(nethttp.MethodGet, "/v1/summaries/"+summary.ID, nil)
		sreq.SetPathValue("id", summary.ID)
		srv.handleGetSummary(sw, sreq)
		if sw.Code != nethttp.StatusOK {
			t.Fatalf("summary status=%d body=%s", sw.Code, sw.Body.String())
		}

		ew := httptest.NewRecorder()
		ereq := httptest.NewRequest(nethttp.MethodGet, "/v1/episodes/"+episode.ID, nil)
		ereq.SetPathValue("id", episode.ID)
		srv.handleGetEpisode(ew, ereq)
		if ew.Code != nethttp.StatusOK {
			t.Fatalf("episode status=%d body=%s", ew.Code, ew.Body.String())
		}

		entW := httptest.NewRecorder()
		entReq := httptest.NewRequest(nethttp.MethodGet, "/v1/entities/"+entity.ID, nil)
		entReq.SetPathValue("id", entity.ID)
		srv.handleGetEntity(entW, entReq)
		if entW.Code != nethttp.StatusOK {
			t.Fatalf("entity status=%d body=%s", entW.Code, entW.Body.String())
		}
	})

	t.Run("run_job", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := jsonRequest(t, nethttp.MethodPost, "/v1/jobs/cleanup_recall_history", map[string]interface{}{})
		req.SetPathValue("kind", "cleanup_recall_history")
		srv.handleRunJob(w, req)
		if w.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("repair", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := jsonRequest(t, nethttp.MethodPost, "/v1/repair", map[string]interface{}{"check": true})
		srv.handleRepair(w, req)
		if w.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("explain_recall", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := jsonRequest(t, nethttp.MethodPost, "/v1/explain-recall", map[string]interface{}{"query": "remembered", "item_id": mem.ID})
		srv.handleExplainRecall(w, req)
		if w.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("status", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(nethttp.MethodGet, "/v1/status", nil)
		srv.handleStatus(w, req)
		if w.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("reset_derived", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := jsonRequest(t, nethttp.MethodPost, "/v1/reset-derived", map[string]interface{}{"confirm": true})
		srv.handleResetDerived(w, req)
		if w.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("forget_memory", func(t *testing.T) {
		mem2 := mustRemember(t, srv)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(nethttp.MethodDelete, "/v1/memories/"+mem2.ID, nil)
		req.SetPathValue("id", mem2.ID)
		srv.handleForgetMemory(w, req)
		if w.Code != nethttp.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestHandlersErrorPaths(t *testing.T) {
	srv, _, _ := testHTTPEnv(t)

	badJSONReq := func(method, path string) *nethttp.Request {
		req := httptest.NewRequest(method, path, bytes.NewBufferString("{"))
		req.Header.Set("Content-Type", "application/json")
		return req
	}

	assertStatus := func(t *testing.T, got, want int, body string) {
		t.Helper()
		if got != want {
			t.Fatalf("status=%d want=%d body=%s", got, want, body)
		}
	}

	t.Run("bad json body handlers", func(t *testing.T) {
		cases := []struct {
			name    string
			handle  func(nethttp.ResponseWriter, *nethttp.Request)
			method  string
			path    string
			setPath func(*nethttp.Request)
		}{
			{"ingest_event", srv.handleIngestEvent, nethttp.MethodPost, "/v1/events", nil},
			{"ingest_transcript", srv.handleIngestTranscript, nethttp.MethodPost, "/v1/transcripts", nil},
			{"remember", srv.handleRemember, nethttp.MethodPost, "/v1/memories", nil},
			{"update_memory", srv.handleUpdateMemory, nethttp.MethodPatch, "/v1/memories/x", func(r *nethttp.Request) { r.SetPathValue("id", "x") }},
			{"share_memory", srv.handleShareMemory, nethttp.MethodPatch, "/v1/memories/x/share", func(r *nethttp.Request) { r.SetPathValue("id", "x") }},
			{"recall", srv.handleRecall, nethttp.MethodPost, "/v1/recall", nil},
			{"describe", srv.handleDescribe, nethttp.MethodPost, "/v1/describe", nil},
			{"history", srv.handleHistory, nethttp.MethodPost, "/v1/history", nil},
			{"add_policy", srv.handleAddPolicy, nethttp.MethodPost, "/v1/policies", nil},
			{"register_project", srv.handleRegisterProject, nethttp.MethodPost, "/v1/projects", nil},
			{"add_relationship", srv.handleAddRelationship, nethttp.MethodPost, "/v1/relationships", nil},
			{"repair", srv.handleRepair, nethttp.MethodPost, "/v1/repair", nil},
			{"explain_recall", srv.handleExplainRecall, nethttp.MethodPost, "/v1/explain-recall", nil},
			{"reset_derived", srv.handleResetDerived, nethttp.MethodPost, "/v1/reset-derived", nil},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				req := badJSONReq(tc.method, tc.path)
				if tc.setPath != nil {
					tc.setPath(req)
				}
				tc.handle(w, req)
				assertStatus(t, w.Code, nethttp.StatusBadRequest, w.Body.String())
			})
		}
	})

	t.Run("not found handlers", func(t *testing.T) {
		cases := []struct {
			name   string
			handle func(nethttp.ResponseWriter, *nethttp.Request)
			path   string
			setID  string
		}{
			{"get_memory", srv.handleGetMemory, "/v1/memories/missing", "missing"},
			{"forget_memory", srv.handleForgetMemory, "/v1/memories/missing", "missing"},
			{"remove_policy", srv.handleRemovePolicy, "/v1/policies/missing", "missing"},
			{"get_project", srv.handleGetProject, "/v1/projects/missing", "missing"},
			{"remove_project", srv.handleRemoveProject, "/v1/projects/missing", "missing"},
			{"get_relationship", srv.handleGetRelationship, "/v1/relationships/missing", "missing"},
			{"remove_relationship", srv.handleRemoveRelationship, "/v1/relationships/missing", "missing"},
			{"get_summary", srv.handleGetSummary, "/v1/summaries/missing", "missing"},
			{"get_episode", srv.handleGetEpisode, "/v1/episodes/missing", "missing"},
			{"get_entity", srv.handleGetEntity, "/v1/entities/missing", "missing"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				w := httptest.NewRecorder()
				req := httptest.NewRequest(nethttp.MethodGet, tc.path, nil)
				req.SetPathValue("id", tc.setID)
				if tc.name == "forget_memory" || tc.name == "remove_policy" || tc.name == "remove_project" || tc.name == "remove_relationship" {
					req.Method = nethttp.MethodDelete
				}
				tc.handle(w, req)
				assertStatus(t, w.Code, nethttp.StatusNotFound, w.Body.String())
			})
		}
	})

	t.Run("list_relationships_bad_limit", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(nethttp.MethodGet, "/v1/relationships?limit=abc", nil)
		srv.handleListRelationships(w, req)
		assertStatus(t, w.Code, nethttp.StatusBadRequest, w.Body.String())
	})

	t.Run("expand_invalid_kind", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(nethttp.MethodGet, "/v1/expand/id?kind=invalid", nil)
		req.SetPathValue("id", "id")
		srv.handleExpand(w, req)
		assertStatus(t, w.Code, nethttp.StatusBadRequest, w.Body.String())
	})

	t.Run("run_job_invalid_kind", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := jsonRequest(t, nethttp.MethodPost, "/v1/jobs/unknown", map[string]interface{}{})
		req.SetPathValue("kind", "unknown")
		srv.handleRunJob(w, req)
		assertStatus(t, w.Code, nethttp.StatusBadRequest, w.Body.String())
	})

	t.Run("repair_invalid_fix", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := jsonRequest(t, nethttp.MethodPost, "/v1/repair", map[string]interface{}{"fix": "unknown"})
		srv.handleRepair(w, req)
		assertStatus(t, w.Code, nethttp.StatusBadRequest, w.Body.String())
	})

	t.Run("explain_recall_not_found", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := jsonRequest(t, nethttp.MethodPost, "/v1/explain-recall", map[string]interface{}{"query": "x", "item_id": "missing"})
		srv.handleExplainRecall(w, req)
		assertStatus(t, w.Code, nethttp.StatusNotFound, w.Body.String())
	})

	t.Run("reset_derived_confirm_false", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := jsonRequest(t, nethttp.MethodPost, "/v1/reset-derived", map[string]interface{}{"confirm": false})
		srv.handleResetDerived(w, req)
		assertStatus(t, w.Code, nethttp.StatusBadRequest, w.Body.String())
	})

	t.Run("share_memory_invalid_privacy", func(t *testing.T) {
		mem := mustRemember(t, srv)
		w := httptest.NewRecorder()
		req := jsonRequest(t, nethttp.MethodPatch, "/v1/memories/"+mem.ID+"/share", map[string]string{"privacy": "bad"})
		req.SetPathValue("id", mem.ID)
		srv.handleShareMemory(w, req)
		assertStatus(t, w.Code, nethttp.StatusBadRequest, w.Body.String())
	})
}
