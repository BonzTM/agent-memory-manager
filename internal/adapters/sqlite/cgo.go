package sqlite

// This file ensures FTS5 is enabled when building with go-sqlite3.
// Build with: go build -tags fts5 ./...
//
// The mattn/go-sqlite3 driver requires CGO_ENABLED=1 and the "fts5" build tag
// to compile SQLite with FTS5 support.
