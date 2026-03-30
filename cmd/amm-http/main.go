package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	httpapi "github.com/bonztm/agent-memory-manager/internal/adapters/http"
	"github.com/bonztm/agent-memory-manager/internal/buildinfo"
	"github.com/bonztm/agent-memory-manager/internal/runtime"
)

func main() {
	if hasVersionFlag(os.Args[1:]) {
		fmt.Printf("amm-http version %s (%s)\n", buildinfo.Version, buildinfo.CommitShort)
		return
	}

	cfg := runtime.LoadConfigWithEnv()
	svc, cleanup, err := runtime.NewService(cfg)
	if err != nil {
		slog.Error("amm-http", "error", err)
		os.Exit(1)
	}
	defer cleanup()

	server := httpapi.NewServer(svc, httpapi.Config{
		Addr:        cfg.HTTP.Addr,
		CORSOrigins: cfg.HTTP.CORSOrigins,
		APIKey:      cfg.API.Key,
	})

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			slog.Error("amm-http", "error", err)
			os.Exit(1)
		}
	case sig := <-sigCh:
		slog.Info("amm-http shutting down", "signal", sig.String())
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			slog.Error("amm-http", "error", err)
			os.Exit(1)
		}
		if err := <-errCh; err != nil && err != http.ErrServerClosed {
			slog.Error("amm-http", "error", err)
			os.Exit(1)
		}
	}
}

func hasVersionFlag(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "version", "--version", "-v":
			return true
		}
	}
	return false
}
