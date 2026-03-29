package httpapi

import (
	"encoding/json"
	"log/slog"
	nethttp "net/http"
	"strconv"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

func decodeJSON(r *nethttp.Request, target interface{}) error {
	return json.NewDecoder(r.Body).Decode(target)
}

func writeServiceError(w nethttp.ResponseWriter, err error) {
	status, code := serviceErrorToHTTP(err)
	writeError(w, status, code, err.Error())
}

func (s *Server) handleInit(w nethttp.ResponseWriter, r *nethttp.Request) {
	status, err := s.svc.Status(r.Context())
	if err != nil {
		slog.Error("handler error", "handler", "handleInit", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, map[string]interface{}{"message": "already initialized", "status": status})
}

func (s *Server) handleIngestEvent(w nethttp.ResponseWriter, r *nethttp.Request) {
	var evt core.Event
	if err := decodeJSON(r, &evt); err != nil {
		slog.Warn("invalid request body", "handler", "handleIngestEvent", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, err := s.svc.IngestEvent(r.Context(), &evt)
	if err != nil {
		slog.Error("handler error", "handler", "handleIngestEvent", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusCreated, result)
}

func (s *Server) handleIngestTranscript(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req struct {
		Events []*core.Event `json:"events"`
	}
	if err := decodeJSON(r, &req); err != nil {
		slog.Warn("invalid request body", "handler", "handleIngestTranscript", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	ingested, err := s.svc.IngestTranscript(r.Context(), req.Events)
	if err != nil {
		slog.Error("handler error", "handler", "handleIngestTranscript", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, map[string]int{"ingested": ingested})
}

func (s *Server) handleRemember(w nethttp.ResponseWriter, r *nethttp.Request) {
	var mem core.Memory
	if err := decodeJSON(r, &mem); err != nil {
		slog.Warn("invalid request body", "handler", "handleRemember", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, err := s.svc.Remember(r.Context(), &mem)
	if err != nil {
		slog.Error("handler error", "handler", "handleRemember", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusCreated, result)
}

func (s *Server) handleGetMemory(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := r.PathValue("id")
	result, err := s.svc.GetMemory(r.Context(), id)
	if err != nil {
		slog.Error("handler error", "handler", "handleGetMemory", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleUpdateMemory(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := r.PathValue("id")

	var patch struct {
		Body             *string            `json:"body"`
		TightDescription *string            `json:"tight_description"`
		Subject          *string            `json:"subject"`
		Type             *string            `json:"type"`
		Scope            *string            `json:"scope"`
		Status           *string            `json:"status"`
		Metadata         map[string]string  `json:"metadata"`
	}
	if err := decodeJSON(r, &patch); err != nil {
		slog.Warn("invalid request body", "handler", "handleUpdateMemory", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}

	existing, err := s.svc.GetMemory(r.Context(), id)
	if err != nil {
		slog.Error("handler error", "handler", "handleUpdateMemory", "error", err)
		writeServiceError(w, err)
		return
	}

	if patch.Body != nil {
		existing.Body = *patch.Body
	}
	if patch.TightDescription != nil {
		existing.TightDescription = *patch.TightDescription
	}
	if patch.Subject != nil {
		existing.Subject = *patch.Subject
	}
	if patch.Type != nil {
		existing.Type = core.MemoryType(*patch.Type)
	}
	if patch.Scope != nil {
		existing.Scope = core.Scope(*patch.Scope)
	}
	if patch.Status != nil {
		existing.Status = core.MemoryStatus(*patch.Status)
	}
	if patch.Metadata != nil {
		for k, v := range patch.Metadata {
			if existing.Metadata == nil {
				existing.Metadata = make(map[string]string)
			}
			existing.Metadata[k] = v
		}
	}

	result, err := s.svc.UpdateMemory(r.Context(), existing)
	if err != nil {
		slog.Error("handler error", "handler", "handleUpdateMemory", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleShareMemory(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := r.PathValue("id")
	var req struct {
		Privacy string `json:"privacy"`
	}
	if err := decodeJSON(r, &req); err != nil {
		slog.Warn("invalid request body", "handler", "handleShareMemory", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, err := s.svc.ShareMemory(r.Context(), id, core.PrivacyLevel(req.Privacy))
	if err != nil {
		slog.Error("handler error", "handler", "handleShareMemory", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleForgetMemory(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := r.PathValue("id")
	result, err := s.svc.ForgetMemory(r.Context(), id)
	if err != nil {
		slog.Error("handler error", "handler", "handleForgetMemory", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleRecall(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req struct {
		Query   string             `json:"query"`
		AgentID string             `json:"agent_id"`
		Explain bool               `json:"explain"`
		Opts    core.RecallOptions `json:"opts"`
	}
	if err := decodeJSON(r, &req); err != nil {
		slog.Warn("invalid request body", "handler", "handleRecall", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if req.Opts.AgentID == "" && req.AgentID != "" {
		req.Opts.AgentID = req.AgentID
	}
	if req.Explain {
		req.Opts.Explain = true
	}
	result, err := s.svc.Recall(r.Context(), req.Query, req.Opts)
	if err != nil {
		slog.Error("handler error", "handler", "handleRecall", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleDescribe(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req struct {
		IDs []string `json:"ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		slog.Warn("invalid request body", "handler", "handleDescribe", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, err := s.svc.Describe(r.Context(), req.IDs)
	if err != nil {
		slog.Error("handler error", "handler", "handleDescribe", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleExpand(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := r.PathValue("id")
	kind := r.URL.Query().Get("kind")
	sessionID := r.URL.Query().Get("session_id")
	result, err := s.svc.Expand(r.Context(), id, kind, core.ExpandOptions{SessionID: sessionID})
	if err != nil {
		slog.Error("handler error", "handler", "handleExpand", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleHistory(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req struct {
		Query string              `json:"query"`
		Opts  core.HistoryOptions `json:"opts"`
	}
	if err := decodeJSON(r, &req); err != nil {
		slog.Warn("invalid request body", "handler", "handleHistory", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, err := s.svc.History(r.Context(), req.Query, req.Opts)
	if err != nil {
		slog.Error("handler error", "handler", "handleHistory", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleListPolicies(w nethttp.ResponseWriter, r *nethttp.Request) {
	result, err := s.svc.ListPolicies(r.Context())
	if err != nil {
		slog.Error("handler error", "handler", "handleListPolicies", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleAddPolicy(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req core.IngestionPolicy
	if err := decodeJSON(r, &req); err != nil {
		slog.Warn("invalid request body", "handler", "handleAddPolicy", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, err := s.svc.AddPolicy(r.Context(), &req)
	if err != nil {
		slog.Error("handler error", "handler", "handleAddPolicy", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusCreated, result)
}

func (s *Server) handleRemovePolicy(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := r.PathValue("id")
	if err := s.svc.RemovePolicy(r.Context(), id); err != nil {
		slog.Error("handler error", "handler", "handleRemovePolicy", "error", err)
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

func (s *Server) handleRegisterProject(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req core.Project
	if err := decodeJSON(r, &req); err != nil {
		slog.Warn("invalid request body", "handler", "handleRegisterProject", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, err := s.svc.RegisterProject(r.Context(), &req)
	if err != nil {
		slog.Error("handler error", "handler", "handleRegisterProject", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusCreated, result)
}

func (s *Server) handleGetProject(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := r.PathValue("id")
	result, err := s.svc.GetProject(r.Context(), id)
	if err != nil {
		slog.Error("handler error", "handler", "handleGetProject", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleListProjects(w nethttp.ResponseWriter, r *nethttp.Request) {
	result, err := s.svc.ListProjects(r.Context())
	if err != nil {
		slog.Error("handler error", "handler", "handleListProjects", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleRemoveProject(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := r.PathValue("id")
	if err := s.svc.RemoveProject(r.Context(), id); err != nil {
		slog.Error("handler error", "handler", "handleRemoveProject", "error", err)
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

func (s *Server) handleAddRelationship(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req core.Relationship
	if err := decodeJSON(r, &req); err != nil {
		slog.Warn("invalid request body", "handler", "handleAddRelationship", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, err := s.svc.AddRelationship(r.Context(), &req)
	if err != nil {
		slog.Error("handler error", "handler", "handleAddRelationship", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusCreated, result)
}

func (s *Server) handleGetRelationship(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := r.PathValue("id")
	result, err := s.svc.GetRelationship(r.Context(), id)
	if err != nil {
		slog.Error("handler error", "handler", "handleGetRelationship", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleListRelationships(w nethttp.ResponseWriter, r *nethttp.Request) {
	limit := 0
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			writeError(w, nethttp.StatusBadRequest, "bad_request", "limit must be an integer")
			return
		}
		limit = parsed
	}
	opts := core.ListRelationshipsOptions{
		EntityID:         r.URL.Query().Get("entity_id"),
		RelationshipType: r.URL.Query().Get("relationship_type"),
		Limit:            limit,
	}
	result, err := s.svc.ListRelationships(r.Context(), opts)
	if err != nil {
		slog.Error("handler error", "handler", "handleListRelationships", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleRemoveRelationship(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := r.PathValue("id")
	if err := s.svc.RemoveRelationship(r.Context(), id); err != nil {
		slog.Error("handler error", "handler", "handleRemoveRelationship", "error", err)
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(nethttp.StatusNoContent)
}

func (s *Server) handleGetSummary(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := r.PathValue("id")
	result, err := s.svc.GetSummary(r.Context(), id)
	if err != nil {
		slog.Error("handler error", "handler", "handleGetSummary", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleGetEpisode(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := r.PathValue("id")
	result, err := s.svc.GetEpisode(r.Context(), id)
	if err != nil {
		slog.Error("handler error", "handler", "handleGetEpisode", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleGetEntity(w nethttp.ResponseWriter, r *nethttp.Request) {
	id := r.PathValue("id")
	result, err := s.svc.GetEntity(r.Context(), id)
	if err != nil {
		slog.Error("handler error", "handler", "handleGetEntity", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleRunJob(w nethttp.ResponseWriter, r *nethttp.Request) {
	kind := r.PathValue("kind")
	result, err := s.svc.RunJob(r.Context(), kind)
	if err != nil {
		slog.Error("handler error", "handler", "handleRunJob", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleRepair(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req struct {
		Check bool   `json:"check"`
		Fix   string `json:"fix"`
	}
	if err := decodeJSON(r, &req); err != nil {
		slog.Warn("invalid request body", "handler", "handleRepair", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, err := s.svc.Repair(r.Context(), req.Check, req.Fix)
	if err != nil {
		slog.Error("handler error", "handler", "handleRepair", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleExplainRecall(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req struct {
		Query  string `json:"query"`
		ItemID string `json:"item_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		slog.Warn("invalid request body", "handler", "handleExplainRecall", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	result, err := s.svc.ExplainRecall(r.Context(), req.Query, req.ItemID)
	if err != nil {
		slog.Error("handler error", "handler", "handleExplainRecall", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleStatus(w nethttp.ResponseWriter, r *nethttp.Request) {
	result, err := s.svc.Status(r.Context())
	if err != nil {
		slog.Error("handler error", "handler", "handleStatus", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleResetDerived(w nethttp.ResponseWriter, r *nethttp.Request) {
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if err := decodeJSON(r, &req); err != nil {
		slog.Warn("invalid request body", "handler", "handleResetDerived", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if !req.Confirm {
		writeError(w, nethttp.StatusBadRequest, "bad_request", "confirm must be true")
		return
	}
	result, err := s.svc.ResetDerived(r.Context())
	if err != nil {
		slog.Error("handler error", "handler", "handleResetDerived", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleHealthz(w nethttp.ResponseWriter, r *nethttp.Request) {
	writeData(w, nethttp.StatusOK, map[string]string{"status": "ok"})
}
