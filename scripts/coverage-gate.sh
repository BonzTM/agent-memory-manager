#!/usr/bin/env bash
set -euo pipefail

THRESHOLD=85
FAILED=0

if [ -x "/usr/local/go/bin/go" ]; then
  GO_BIN="/usr/local/go/bin/go"
else
  GO_BIN="go"
fi

PACKAGES=(
  ./internal/adapters/cli
  ./internal/adapters/mcp
  ./internal/adapters/sqlite
  ./internal/contracts/v1
  ./internal/service
)

for pkg in "${PACKAGES[@]}"; do
  line=$($GO_BIN test -tags fts5 "$pkg" -count=1 -cover 2>&1 | grep "^ok" || true)
  if [ -z "$line" ]; then
    echo "FAIL: $pkg — tests did not pass or no coverage output"
    FAILED=1
    continue
  fi

  pct=$(echo "$line" | grep -oP 'coverage: \K[0-9]+(\.[0-9]+)?' || echo "0")
  if [ -z "$pct" ]; then
    echo "FAIL: $pkg — could not parse coverage"
    FAILED=1
    continue
  fi

  pass=$(awk "BEGIN { print ($pct >= $THRESHOLD) ? 1 : 0 }")
  if [ "$pass" -eq 1 ]; then
    echo "PASS: $pkg — ${pct}% (threshold: ${THRESHOLD}%)"
  else
    echo "FAIL: $pkg — ${pct}% < ${THRESHOLD}%"
    FAILED=1
  fi
done

if [ "$FAILED" -ne 0 ]; then
  echo ""
  echo "Coverage gate FAILED: one or more packages below ${THRESHOLD}%"
  exit 1
fi

echo ""
echo "Coverage gate PASSED: all packages >= ${THRESHOLD}%"
