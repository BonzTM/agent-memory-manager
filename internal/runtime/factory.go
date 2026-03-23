package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshd-04/agent-memory-manager/internal/adapters/sqlite"
	"github.com/joshd-04/agent-memory-manager/internal/core"
	"github.com/joshd-04/agent-memory-manager/internal/service"
)

// NewService creates a fully initialized AMM service from the given config.
// Returns the Service interface, a cleanup function, and any error.
// The caller must invoke the cleanup function when done (typically via defer).
func NewService(cfg Config) (core.Service, func(), error) {
	dbDir := filepath.Dir(cfg.Storage.DBPath)
	if err := os.MkdirAll(dbDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create db directory %s: %w", dbDir, err)
	}

	ctx := context.Background()

	db, err := sqlite.Open(ctx, cfg.Storage.DBPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}

	if err := sqlite.Migrate(ctx, db); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("run migrations: %w", err)
	}

	repo := &sqlite.SQLiteRepository{DB: db}
	svc := service.New(repo)

	cleanup := func() {
		db.Close()
	}

	return svc, cleanup, nil
}
