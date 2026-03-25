#!/usr/bin/env bash
# Suitable for cron: */30 * * * * /path/to/run-workers.sh
set -euo pipefail

AMM="${AMM_BIN:-/usr/local/bin/amm}"
export AMM_DB_PATH="${AMM_DB_PATH:-$HOME/.amm/amm.db}"
LOCK_DIR="${AMM_DB_PATH}.maintenance.lock"

# Skip if DB doesn't exist
[ -f "$AMM_DB_PATH" ] || exit 0

acquire_lock() {
  if mkdir "$LOCK_DIR" 2>/dev/null; then
    printf '%s\n' "$$" > "$LOCK_DIR/pid"
    return 0
  fi

  if [ -f "$LOCK_DIR/pid" ]; then
    pid=$(cat "$LOCK_DIR/pid" 2>/dev/null || true)
    if [ -n "${pid:-}" ] && kill -0 "$pid" 2>/dev/null; then
      return 1
    fi
  fi

  rm -rf "$LOCK_DIR" 2>/dev/null || return 1
  mkdir "$LOCK_DIR" 2>/dev/null || return 1
  printf '%s\n' "$$" > "$LOCK_DIR/pid"
  return 0
}

if ! acquire_lock; then
  exit 0
fi

cleanup_lock() {
  rm -rf "$LOCK_DIR" 2>/dev/null || true
}
trap cleanup_lock EXIT INT TERM

$AMM jobs run reflect >/dev/null 2>&1 || true
$AMM jobs run compress_history >/dev/null 2>&1 || true
$AMM jobs run consolidate_sessions >/dev/null 2>&1 || true
$AMM jobs run extract_claims >/dev/null 2>&1 || true
$AMM jobs run form_episodes >/dev/null 2>&1 || true
$AMM jobs run detect_contradictions >/dev/null 2>&1 || true
$AMM jobs run cleanup_recall_history >/dev/null 2>&1 || true
