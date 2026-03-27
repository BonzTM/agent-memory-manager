package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

// DB wraps a SQLite connection with amm-specific configuration.
type DB struct {
	db   *sql.DB
	path string
	mu   sync.RWMutex
}

// Open creates or opens a SQLite database at the given path.
func Open(ctx context.Context, dbPath string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", dbPath)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	// Single writer, multiple readers for WAL mode.
	sqlDB.SetMaxOpenConns(1)
	return &DB{db: sqlDB, path: dbPath}, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// Conn returns the underlying *sql.DB for direct queries.
func (d *DB) Conn() *sql.DB {
	return d.db
}

// Path returns the database file path.
func (d *DB) Path() string {
	return d.path
}

// ExecContext runs a statement with exclusive write access.
func (d *DB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.db.ExecContext(ctx, query, args...)
}

// QueryContext runs a query with shared read access.
func (d *DB) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.db.QueryContext(ctx, query, args...)
}

// QueryRowContext runs a single-row query with shared read access.
func (d *DB) QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.db.QueryRowContext(ctx, query, args...)
}
