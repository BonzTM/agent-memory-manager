package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net"
	nethttp "net/http"

	"github.com/bonztm/agent-memory-manager/internal/core"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

type Server struct {
	svc         core.Service
	server      *nethttp.Server
	addr        string
	corsOrigins string
	mcpHandler  *mcpserver.StreamableHTTPServer
}

type Config struct {
	Addr        string
	CORSOrigins string
	APIKey      string
}

func NewServer(svc core.Service, cfg Config) *Server {
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}

	if cfg.APIKey == "" {
		slog.Info("api key auth disabled, server is open")
	} else {
		slog.Info("api key auth enabled")
	}

	mcpSrv := newMCPBridge(svc, "1.0.0")
	mcpHandler := mcpserver.NewStreamableHTTPServer(mcpSrv)

	s := &Server{svc: svc, addr: cfg.Addr, corsOrigins: cfg.CORSOrigins, mcpHandler: mcpHandler}
	mux := nethttp.NewServeMux()
	s.registerRoutes(mux)

	handler := apiKeyAuth(cfg.APIKey)(contentTypeJSON(mux))
	if cfg.CORSOrigins != "" {
		handler = cors(cfg.CORSOrigins)(handler)
	}
	handler = requestLogging(handler)

	s.server = &nethttp.Server{
		Addr:    cfg.Addr,
		Handler: handler,
	}
	return s
}

func (s *Server) registerRoutes(mux *nethttp.ServeMux) {
	mux.Handle("/v1/mcp", s.mcpHandler)
	mux.Handle("/v1/mcp/", s.mcpHandler)
	mux.HandleFunc("GET /openapi.json", s.handleOpenAPISpec)
	mux.Handle("GET /swagger/", s.handleSwaggerUI())

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
