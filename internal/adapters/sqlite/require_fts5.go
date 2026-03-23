//go:build !fts5

package sqlite

// This file is only compiled when the fts5 build tag is NOT set.
// It produces a compile-time error to enforce the tag.

func init() {
	// AMM requires FTS5 support. Build with: CGO_ENABLED=1 go build -tags fts5 ./...
	var _ = _AMM_REQUIRES_FTS5_BUILD_TAG_
}
