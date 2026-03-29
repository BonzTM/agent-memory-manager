package httpapi

import (
	"embed"
	"io/fs"
	nethttp "net/http"
)

//go:embed openapi_spec.json
var openapiSpec []byte

//go:embed swagger/*
var swaggerEmbedFS embed.FS

var swaggerFS = mustSwaggerFS()

func mustSwaggerFS() fs.FS {
	f, err := fs.Sub(swaggerEmbedFS, "swagger")
	if err != nil {
		panic(err)
	}
	return f
}

func (s *Server) handleOpenAPISpec(w nethttp.ResponseWriter, _ *nethttp.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(nethttp.StatusOK)
	_, _ = w.Write(openapiSpec)
}

func (s *Server) handleSwaggerUI() nethttp.Handler {
	return nethttp.StripPrefix("/swagger/", nethttp.FileServer(nethttp.FS(swaggerFS)))
}
