#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT_DIR="${1:-$REPO_ROOT}"

cd "$REPO_ROOT"

echo "Building amm..."
GO="${GO:-$(command -v go 2>/dev/null || echo /usr/local/go/bin/go)}"
"$GO" build -o "$OUT_DIR/amm" ./cmd/amm

echo "Building amm-mcp..."
"$GO" build -o "$OUT_DIR/amm-mcp" ./cmd/amm-mcp

echo "Building amm-http..."
"$GO" build -o "$OUT_DIR/amm-http" ./cmd/amm-http

echo "Done. Binaries in $OUT_DIR"