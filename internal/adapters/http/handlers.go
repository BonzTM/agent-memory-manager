package httpapi

import (
	"encoding/json"
	"log/slog"
	nethttp "net/http"

	v1 "github.com/bonztm/agent-memory-manager/internal/contracts/v1"
	"github.com/bonztm/agent-memory-manager/internal/core"
)

func decodeJSON(w nethttp.ResponseWriter, r *nethttp.Request, target interface{}) error {
	r.Body = nethttp.MaxBytesReader(w, r.Body, 10<<20)
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
	if err := decodeJSON(w, r, &evt); err != nil {
		slog.Warn("invalid request body", "handler", "handleIngestEvent", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := v1.ValidateIngestEvent(&v1.IngestEventRequest{
		Kind:         evt.Kind,
		SourceSystem: evt.SourceSystem,
		Surface:      evt.Surface,
		SessionID:    evt.SessionID,
		ProjectID:    evt.ProjectID,
		AgentID:      evt.AgentID,
		ActorType:    evt.ActorType,
		ActorID:      evt.ActorID,
		PrivacyLevel: string(evt.PrivacyLevel),
		Content:      evt.Content,
		Metadata:     evt.Metadata,
	}); err != nil {
		slog.Warn("validation failed", "handler", "handleIngestEvent", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", err.Error())
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
	if err := decodeJSON(w, r, &req); err != nil {
		slog.Warn("invalid request body", "handler", "handleIngestTranscript", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	transcriptReq := &v1.IngestTranscriptRequest{Events: make([]v1.IngestEventRequest, 0, len(req.Events))}
	for _, evt := range req.Events {
		if evt == nil {
			transcriptReq.Events = append(transcriptReq.Events, v1.IngestEventRequest{})
			continue
		}
		transcriptReq.Events = append(transcriptReq.Events, v1.IngestEventRequest{
			Kind:         evt.Kind,
			SourceSystem: evt.SourceSystem,
			Surface:      evt.Surface,
			SessionID:    evt.SessionID,
			ProjectID:    evt.ProjectID,
			AgentID:      evt.AgentID,
			ActorType:    evt.ActorType,
			ActorID:      evt.ActorID,
			PrivacyLevel: string(evt.PrivacyLevel),
			Content:      evt.Content,
			Metadata:     evt.Metadata,
		})
	}
	if err := v1.ValidateIngestTranscript(transcriptReq); err != nil {
		slog.Warn("validation failed", "handler", "handleIngestTranscript", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", err.Error())
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
	if err := decodeJSON(w, r, &mem); err != nil {
		slog.Warn("invalid request body", "handler", "handleRemember", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := v1.ValidateRemember(&v1.RememberRequest{
		Type:             string(mem.Type),
		Scope:            string(mem.Scope),
		ProjectID:        mem.ProjectID,
		SessionID:        mem.SessionID,
		AgentID:          mem.AgentID,
		Subject:          mem.Subject,
		Body:             mem.Body,
		TightDescription: mem.TightDescription,
		PrivacyLevel:     string(mem.PrivacyLevel),
		Tags:             mem.Tags,
		Metadata:         mem.Metadata,
		SourceEventIDs:   mem.SourceEventIDs,
	}); err != nil {
		slog.Warn("validation failed", "handler", "handleRemember", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", err.Error())
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

	var updateReq v1.UpdateMemoryRequest
	if err := decodeJSON(w, r, &updateReq); err != nil {
		slog.Warn("invalid request body", "handler", "handleUpdateMemory", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	updateReq.ID = id
	if err := v1.ValidateUpdateMemory(&updateReq); err != nil {
		slog.Warn("validation failed", "handler", "handleUpdateMemory", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", err.Error())
		return
	}

	existing, err := s.svc.GetMemory(r.Context(), id)
	if err != nil {
		slog.Error("handler error", "handler", "handleUpdateMemory", "error", err)
		writeServiceError(w, err)
		return
	}

	v1.ApplyMemoryUpdate(existing, updateReq)

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
	if err := decodeJSON(w, r, &req); err != nil {
		slog.Warn("invalid request body", "handler", "handleShareMemory", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := v1.ValidateShare(v1.ShareRequest{ID: id, Privacy: req.Privacy}); err != nil {
		slog.Warn("validation failed", "handler", "handleShareMemory", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", err.Error())
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
	if err := v1.ValidateForget(&v1.ForgetRequest{ID: id}); err != nil {
		slog.Warn("validation failed", "handler", "handleForgetMemory", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", err.Error())
		return
	}
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
	if r.Method == nethttp.MethodGet {
		req.Query = r.URL.Query().Get("query")
		req.AgentID = r.URL.Query().Get("agent_id")
		req.Opts.Mode = core.RecallMode(r.URL.Query().Get("mode"))
		req.Opts.ProjectID = r.URL.Query().Get("project_id")
		req.Opts.SessionID = r.URL.Query().Get("session_id")
		req.Opts.AgentID = r.URL.Query().Get("agent_id")
		req.Opts.EntityIDs = r.URL.Query()["entity_id"]
		parsedLimit, err := parseIntParam(r, "limit", req.Opts.Limit)
		if err != nil {
			slog.Warn("validation failed", "handler", "handleRecall", "error", err)
			writeError(w, nethttp.StatusBadRequest, "validation_error", "limit must be an integer")
			return
		}
		req.Opts.Limit = parsedLimit
		parsedExplain, err := parseBoolParam(r, "explain", req.Opts.Explain)
		if err != nil {
			slog.Warn("validation failed", "handler", "handleRecall", "error", err)
			writeError(w, nethttp.StatusBadRequest, "validation_error", "explain must be a boolean")
			return
		}
		req.Opts.Explain = parsedExplain
	} else {
		if err := decodeJSON(w, r, &req); err != nil {
			slog.Warn("invalid request body", "handler", "handleRecall", "error", err)
			writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
			return
		}
	}
	if req.Opts.AgentID == "" && req.AgentID != "" {
		req.Opts.AgentID = req.AgentID
	}
	if req.Explain {
		req.Opts.Explain = true
	}
	if err := v1.ValidateRecall(&v1.RecallRequest{
		Query:     req.Query,
		Mode:      string(req.Opts.Mode),
		ProjectID: req.Opts.ProjectID,
		SessionID: req.Opts.SessionID,
		AgentID:   req.Opts.AgentID,
		EntityIDs: req.Opts.EntityIDs,
		Limit:     req.Opts.Limit,
		Explain:   req.Opts.Explain,
	}); err != nil {
		slog.Warn("validation failed", "handler", "handleRecall", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", err.Error())
		return
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
	if err := decodeJSON(w, r, &req); err != nil {
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
	if kind == "" {
		kind = "memory"
	}
	sessionID := r.URL.Query().Get("session_id")
	delegationDepth, err := parseIntParam(r, "delegation_depth", 0)
	if err != nil {
		slog.Warn("validation failed", "handler", "handleExpand", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", "delegation_depth must be an integer")
		return
	}
	if err := v1.ValidateExpand(&v1.ExpandRequest{
		ID:              id,
		Kind:            kind,
		SessionID:       sessionID,
		DelegationDepth: delegationDepth,
	}); err != nil {
		slog.Warn("validation failed", "handler", "handleExpand", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", err.Error())
		return
	}
	result, err := s.svc.Expand(r.Context(), id, kind, core.ExpandOptions{SessionID: sessionID, DelegationDepth: delegationDepth})
	if err != nil {
		slog.Error("handler error", "handler", "handleExpand", "error", err)
		writeServiceError(w, err)
		return
	}
	writeData(w, nethttp.StatusOK, result)
}

func (s *Server) handleFormatContextWindow(w nethttp.ResponseWriter, r *nethttp.Request) {
	freshTailCount, err := parseIntParam(r, "fresh_tail_count", 0)
	if err != nil {
		slog.Warn("validation failed", "handler", "handleFormatContextWindow", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", "fresh_tail_count must be an integer")
		return
	}

	maxSummaryDepth, err := parseIntParam(r, "max_summary_depth", 0)
	if err != nil {
		slog.Warn("validation failed", "handler", "handleFormatContextWindow", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", "max_summary_depth must be an integer")
		return
	}

	includeParentRefs, err := parseBoolParam(r, "include_parent_refs", false)
	if err != nil {
		slog.Warn("validation failed", "handler", "handleFormatContextWindow", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", "include_parent_refs must be a boolean")
		return
	}

	req := &v1.FormatContextWindowRequest{
		SessionID:         r.URL.Query().Get("session_id"),
		ProjectID:         r.URL.Query().Get("project_id"),
		FreshTailCount:    freshTailCount,
		MaxSummaryDepth:   maxSummaryDepth,
		IncludeParentRefs: includeParentRefs,
	}
	if err := v1.ValidateFormatContextWindow(req); err != nil {
		slog.Warn("validation failed", "handler", "handleFormatContextWindow", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", err.Error())
		return
	}

	result, err := s.svc.FormatContextWindow(r.Context(), core.FormatContextWindowOptions{
		SessionID:         req.SessionID,
		ProjectID:         req.ProjectID,
		FreshTailCount:    req.FreshTailCount,
		MaxSummaryDepth:   req.MaxSummaryDepth,
		IncludeParentRefs: req.IncludeParentRefs,
	})
	if err != nil {
		slog.Error("handler error", "handler", "handleFormatContextWindow", "error", err)
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
	if err := decodeJSON(w, r, &req); err != nil {
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

func (s *Server) handleGrep(w nethttp.ResponseWriter, r *nethttp.Request) {
	maxGroupDepth, err := parseIntParam(r, "max_group_depth", 0)
	if err != nil {
		slog.Warn("validation failed", "handler", "handleGrep", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", "max_group_depth must be an integer")
		return
	}

	groupLimit, err := parseIntParam(r, "group_limit", 0)
	if err != nil {
		slog.Warn("validation failed", "handler", "handleGrep", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", "group_limit must be an integer")
		return
	}

	matchesPerGroup, err := parseIntParam(r, "matches_per_group", 0)
	if err != nil {
		slog.Warn("validation failed", "handler", "handleGrep", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", "matches_per_group must be an integer")
		return
	}

	req := &v1.GrepRequest{
		Pattern:         r.URL.Query().Get("pattern"),
		SessionID:       r.URL.Query().Get("session_id"),
		ProjectID:       r.URL.Query().Get("project_id"),
		MaxGroupDepth:   maxGroupDepth,
		GroupLimit:      groupLimit,
		MatchesPerGroup: matchesPerGroup,
	}
	if err := v1.ValidateGrep(req); err != nil {
		slog.Warn("validation failed", "handler", "handleGrep", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", err.Error())
		return
	}

	result, err := s.svc.Grep(r.Context(), req.Pattern, core.GrepOptions{
		SessionID:       req.SessionID,
		ProjectID:       req.ProjectID,
		MaxGroupDepth:   req.MaxGroupDepth,
		GroupLimit:      req.GroupLimit,
		MatchesPerGroup: req.MatchesPerGroup,
	})
	if err != nil {
		slog.Error("handler error", "handler", "handleGrep", "error", err)
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
	if err := decodeJSON(w, r, &req); err != nil {
		slog.Warn("invalid request body", "handler", "handleAddPolicy", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := v1.ValidatePolicyAdd(&v1.PolicyAddRequest{
		PatternType: req.PatternType,
		Pattern:     req.Pattern,
		Mode:        req.Mode,
		Priority:    req.Priority,
		MatchMode:   req.MatchMode,
		Metadata:    req.Metadata,
	}); err != nil {
		slog.Warn("validation failed", "handler", "handleAddPolicy", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", err.Error())
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
	if err := decodeJSON(w, r, &req); err != nil {
		slog.Warn("invalid request body", "handler", "handleRegisterProject", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := v1.ValidateRegisterProject(&v1.RegisterProjectRequest{
		Name:        req.Name,
		Path:        req.Path,
		Description: req.Description,
		Metadata:    req.Metadata,
	}); err != nil {
		slog.Warn("validation failed", "handler", "handleRegisterProject", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", err.Error())
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
	if err := decodeJSON(w, r, &req); err != nil {
		slog.Warn("invalid request body", "handler", "handleAddRelationship", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := v1.ValidateAddRelationship(&v1.AddRelationshipRequest{
		FromEntityID:     req.FromEntityID,
		ToEntityID:       req.ToEntityID,
		RelationshipType: req.RelationshipType,
		Metadata:         req.Metadata,
	}); err != nil {
		slog.Warn("validation failed", "handler", "handleAddRelationship", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", err.Error())
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
	limit, err := parseIntParam(r, "limit", 0)
	if err != nil {
		writeError(w, nethttp.StatusBadRequest, "bad_request", "limit must be an integer")
		return
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
	if err := v1.ValidateRunJob(&v1.RunJobRequest{Kind: kind}); err != nil {
		slog.Warn("validation failed", "handler", "handleRunJob", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", err.Error())
		return
	}
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
	if err := decodeJSON(w, r, &req); err != nil {
		slog.Warn("invalid request body", "handler", "handleRepair", "error", err)
		writeError(w, nethttp.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := v1.ValidateRepair(&v1.RepairRequest{Check: req.Check, Fix: req.Fix}); err != nil {
		slog.Warn("validation failed", "handler", "handleRepair", "error", err)
		writeError(w, nethttp.StatusBadRequest, "validation_error", err.Error())
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
	if err := decodeJSON(w, r, &req); err != nil {
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
	if err := decodeJSON(w, r, &req); err != nil {
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
