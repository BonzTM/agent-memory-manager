#!/usr/bin/env bash
set -euo pipefail

AMM="${AMM_BIN:-/usr/local/bin/amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"
export SESSION_ID="${HERMES_SESSION_ID:-}"
export PROJECT_ID="${HERMES_PROJECT_ID:-}"

# Ingest session_stop marker so consolidation knows the session boundary.
STOP_EVENT=$(python3 -c '
import json, datetime, os
print(json.dumps({
    "kind": "session_stop",
    "source_system": "hermes",
    "session_id": os.environ.get("SESSION_ID", ""),
    "project_id": os.environ.get("PROJECT_ID", ""),
    "content": "Hermes session ended.",
    "metadata": {"hook_event": "session_end", "cwd": os.environ.get("PWD", "")},
    "occurred_at": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
}))
')

printf '%s' "$STOP_EVENT" | AMM_DB_PATH="$DB" "$AMM" ingest event --in - >/dev/null 2>&1 || true

# Maintenance jobs (reflect, consolidate_sessions, compress_history, etc.) should
# run on a schedule via cron/systemd timer, not in the stop hook. See
# examples/scripts/run-workers.sh for a ready-made maintenance script.

exit 0
