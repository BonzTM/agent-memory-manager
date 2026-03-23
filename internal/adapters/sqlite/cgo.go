//go:build fts5

package sqlite

// This file enforces the fts5 build tag for the entire sqlite package.
// Without it, the mattn/go-sqlite3 driver won't compile SQLite with FTS5 support,
// and migrations will fail at runtime with "no such module: fts5".
//
// Build with: CGO_ENABLED=1 go build -tags fts5 ./...
