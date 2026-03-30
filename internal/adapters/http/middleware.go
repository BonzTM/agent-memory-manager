package httpapi

import (
	"crypto/subtle"
	"log/slog"
	"mime"
	nethttp "net/http"
	"strings"
	"time"
)

type statusCapturingResponseWriter struct {
	nethttp.ResponseWriter
	status int
}

func (w *statusCapturingResponseWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusCapturingResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(nethttp.Flusher); ok {
		f.Flush()
	}
}

func requestLogging(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		sw := &statusCapturingResponseWriter{ResponseWriter: w, status: nethttp.StatusOK}
		next.ServeHTTP(sw, r)
		if sw.status >= 200 && sw.status < 300 {
			slog.Debug("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration", time.Since(start),
			)
			return
		}

		slog.Info("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration", time.Since(start),
		)
	})
}

func contentTypeJSON(next nethttp.Handler) nethttp.Handler {
	return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/mcp") {
			next.ServeHTTP(w, r)
			return
		}

		switch r.Method {
		case nethttp.MethodPost, nethttp.MethodPatch, nethttp.MethodPut:
			mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || mediaType != "application/json" {
				writeError(w, nethttp.StatusUnsupportedMediaType, "unsupported_media_type", "content-type must be application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func cors(origins string) func(nethttp.Handler) nethttp.Handler {
	allowedOrigins := parseOrigins(origins)
	allowedMethods := "GET,POST,PATCH,PUT,DELETE,OPTIONS"
	allowedHeaders := "Content-Type,Authorization"

	return func(next nethttp.Handler) nethttp.Handler {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			origin := strings.TrimSpace(r.Header.Get("Origin"))
			if allowOrigin(origin, allowedOrigins) {
				if len(allowedOrigins) == 1 && allowedOrigins[0] == "*" {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
				}
				w.Header().Set("Access-Control-Allow-Methods", allowedMethods)
				w.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
			}

			if r.Method == nethttp.MethodOptions {
				w.WriteHeader(nethttp.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func apiKeyAuth(key string) func(nethttp.Handler) nethttp.Handler {
	if key == "" {
		return func(next nethttp.Handler) nethttp.Handler {
			return next
		}
	}

	return func(next nethttp.Handler) nethttp.Handler {
		return nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
			if r.Method == nethttp.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			if r.URL.Path == "/healthz" || r.URL.Path == "/openapi.json" || strings.HasPrefix(r.URL.Path, "/swagger/") {
				next.ServeHTTP(w, r)
				return
			}

			provided := bearerToken(r.Header.Get("Authorization"))
			if provided == "" {
				provided = strings.TrimSpace(r.Header.Get("X-API-Key"))
			}

			if provided == "" {
				slog.Warn("auth rejected: missing API key",
					"path", r.URL.Path,
					"method", r.Method,
					"remote_addr", r.RemoteAddr,
				)
				writeError(w, nethttp.StatusUnauthorized, "unauthorized", "API key required")
				return
			}

			if subtle.ConstantTimeCompare([]byte(provided), []byte(key)) != 1 {
				slog.Warn("auth rejected: invalid API key",
					"path", r.URL.Path,
					"method", r.Method,
					"remote_addr", r.RemoteAddr,
				)
				writeError(w, nethttp.StatusUnauthorized, "unauthorized", "invalid API key")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func bearerToken(authHeader string) string {
	authHeader = strings.TrimSpace(authHeader)
	if len(authHeader) <= 7 || !strings.EqualFold(authHeader[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(authHeader[7:])
}

func parseOrigins(origins string) []string {
	parts := strings.Split(origins, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func allowOrigin(origin string, allowed []string) bool {
	if origin == "" || len(allowed) == 0 {
		return false
	}
	for _, candidate := range allowed {
		if candidate == "*" || candidate == origin {
			return true
		}
	}
	return false
}
