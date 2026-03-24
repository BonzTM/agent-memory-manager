#!/usr/bin/env bash
# Suitable for cron: */30 * * * * /path/to/run-workers.sh
set -euo pipefail

AMM="${AMM_BIN:-/usr/local/bin/amm}"
export AMM_DB_PATH="${AMM_DB_PATH:-$HOME/.amm/amm.db}"

# Skip if DB doesn't exist
[ -f "$AMM_DB_PATH" ] || exit 0

$AMM jobs run reflect >/dev/null 2>&1 || true
$AMM jobs run compress_history >/dev/null 2>&1 || true
$AMM jobs run consolidate_sessions >/dev/null 2>&1 || true
$AMM jobs run extract_claims >/dev/null 2>&1 || true
$AMM jobs run form_episodes >/dev/null 2>&1 || true
$AMM jobs run detect_contradictions >/dev/null 2>&1 || true
$AMM jobs run cleanup_recall_history >/dev/null 2>&1 || true
