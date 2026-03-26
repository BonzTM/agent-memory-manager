#!/usr/bin/env bash
set -euo pipefail

AMM="${AMM_BIN:-/usr/local/bin/amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"
PAYLOAD=""

if [ ! -t 0 ]; then
  PAYLOAD="$(cat || true)"
fi

if [ -n "$PAYLOAD" ]; then
  ASSISTANT_EVENT=$(printf '%s' "$PAYLOAD" | CLAUDE_SESSION_ID="${CLAUDE_SESSION_ID:-}" CLAUDE_PROJECT_ID="${CLAUDE_PROJECT_ID:-}" python3 -c '
import json
import os
import sys
from datetime import datetime, timezone


def now_rfc3339() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


raw = sys.stdin.read().strip()
if not raw:
    raise SystemExit(0)

try:
    payload = json.loads(raw)
except json.JSONDecodeError:
    raise SystemExit(0)

session_id = payload.get("session_id") or payload.get("sessionId") or os.environ.get("CLAUDE_SESSION_ID", "")
project_id = payload.get("project_id") or payload.get("projectId") or os.environ.get("CLAUDE_PROJECT_ID", "")
last_assistant_message = payload.get("last_assistant_message") or payload.get("lastAssistantMessage") or ""

if last_assistant_message:
    print(json.dumps(
        {
            "kind": "message_assistant",
            "source_system": "claude-code",
            "session_id": session_id,
            "project_id": project_id,
            "actor_type": "assistant",
            "content": last_assistant_message,
            "metadata": {"hook_event": "Stop"},
            "occurred_at": now_rfc3339(),
        }
    ))
else:
    print("")
')

  [ -n "$ASSISTANT_EVENT" ] && printf '%s' "$ASSISTANT_EVENT" | AMM_DB_PATH="$DB" "$AMM" ingest event --in - >/dev/null 2>&1 || true
fi

STOP_EVENT=$(CLAUDE_SESSION_ID="${CLAUDE_SESSION_ID:-}" CLAUDE_PROJECT_ID="${CLAUDE_PROJECT_ID:-}" python3 -c '
import json
import os
from datetime import datetime, timezone


def now_rfc3339() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


print(json.dumps({
    "kind": "session_stop",
    "source_system": "claude-code",
    "session_id": os.environ.get("CLAUDE_SESSION_ID", ""),
    "project_id": os.environ.get("CLAUDE_PROJECT_ID", ""),
    "content": "Claude stop hook executed.",
    "metadata": {"hook_event": "Stop"},
    "occurred_at": now_rfc3339(),
}))
')

printf '%s' "$STOP_EVENT" | AMM_DB_PATH="$DB" "$AMM" ingest event --in - >/dev/null 2>&1 || true

AMM_DB_PATH="$DB" "$AMM" jobs run reflect >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run rebuild_indexes >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run compress_history >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run consolidate_sessions >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run extract_claims >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run form_episodes >/dev/null 2>&1 || true

exit 0
