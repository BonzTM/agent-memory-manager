package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net"
	nethttp "net/http"

	"github.com/bonztm/agent-memory-manager/internal/core"
)

type Server struct {
	svc         core.Service
	server      *nethttp.Server
	addr        string
	corsOrigins string
}

type Config struct {
	Addr        string
	CORSOrigins string
}

func NewServer(svc core.Service, cfg Config) *Server {
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}

	s := &Server{svc: svc, addr: cfg.Addr, corsOrigins: cfg.CORSOrigins}
	mux := nethttp.NewServeMux()
	s.registerRoutes(mux)

	handler := requestLogging(contentTypeJSON(mux))
	if cfg.CORSOrigins != "" {
		handler = requestLogging(cors(cfg.CORSOrigins)(contentTypeJSON(mux)))
	}

	s.server = &nethttp.Server{
		Addr:    cfg.Addr,
		Handler: handler,
	}
	return s
}

func (s *Server) registerRoutes(mux *nethttp.ServeMux) {
	mux.HandleFunc("POST /v1/init", s.handleInit)
	mux.HandleFunc("POST /v1/events", s.handleIngestEvent)
	mux.HandleFunc("POST /v1/transcripts", s.handleIngestTranscript)
	mux.HandleFunc("POST /v1/memories", s.handleRemember)
	mux.HandleFunc("GET /v1/memories/{id}", s.handleGetMemory)
	mux.HandleFunc("PATCH /v1/memories/{id}", s.handleUpdateMemory)
	mux.HandleFunc("PATCH /v1/memories/{id}/share", s.handleShareMemory)
	mux.HandleFunc("DELETE /v1/memories/{id}", s.handleForgetMemory)
	mux.HandleFunc("POST /v1/recall", s.handleRecall)
	mux.HandleFunc("POST /v1/describe", s.handleDescribe)
	mux.HandleFunc("GET /v1/expand/{id}", s.handleExpand)
	mux.HandleFunc("POST /v1/history", s.handleHistory)
	mux.HandleFunc("GET /v1/policies", s.handleListPolicies)
	mux.HandleFunc("POST /v1/policies", s.handleAddPolicy)
	mux.HandleFunc("DELETE /v1/policies/{id}", s.handleRemovePolicy)
	mux.HandleFunc("POST /v1/projects", s.handleRegisterProject)
	mux.HandleFunc("GET /v1/projects/{id}", s.handleGetProject)
	mux.HandleFunc("GET /v1/projects", s.handleListProjects)
	mux.HandleFunc("DELETE /v1/projects/{id}", s.handleRemoveProject)
	mux.HandleFunc("POST /v1/relationships", s.handleAddRelationship)
	mux.HandleFunc("GET /v1/relationships/{id}", s.handleGetRelationship)
	mux.HandleFunc("GET /v1/relationships", s.handleListRelationships)
	mux.HandleFunc("DELETE /v1/relationships/{id}", s.handleRemoveRelationship)
	mux.HandleFunc("GET /v1/summaries/{id}", s.handleGetSummary)
	mux.HandleFunc("GET /v1/episodes/{id}", s.handleGetEpisode)
	mux.HandleFunc("GET /v1/entities/{id}", s.handleGetEntity)
	mux.HandleFunc("POST /v1/jobs/{kind}", s.handleRunJob)
	mux.HandleFunc("POST /v1/repair", s.handleRepair)
	mux.HandleFunc("POST /v1/explain-recall", s.handleExplainRecall)
	mux.HandleFunc("GET /v1/status", s.handleStatus)
	mux.HandleFunc("POST /v1/reset-derived", s.handleResetDerived)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
}

func (s *Server) ListenAndServe() error {
	slog.Info("http server starting", "addr", s.addr)
	err := s.server.ListenAndServe()
	if errors.Is(err, nethttp.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Serve(ln net.Listener) error {
	slog.Info("http server starting", "addr", ln.Addr().String())
	err := s.server.Serve(ln)
	if errors.Is(err, nethttp.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
