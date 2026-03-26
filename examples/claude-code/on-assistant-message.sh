#!/usr/bin/env bash
set -euo pipefail

AMM="${AMM_BIN:-/usr/local/bin/amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"
PAYLOAD=""

if [ ! -t 0 ]; then
  PAYLOAD="$(cat || true)"
fi

[ -n "$PAYLOAD" ] || exit 0

EVENT_JSON=$(printf '%s' "$PAYLOAD" | CLAUDE_SESSION_ID="${CLAUDE_SESSION_ID:-}" CLAUDE_PROJECT_ID="${CLAUDE_PROJECT_ID:-}" python3 -c '
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
assistant_message = (
    payload.get("assistant_message")
    or payload.get("assistantMessage")
    or payload.get("message")
    or payload.get("response")
    or payload.get("content")
    or ""
)

if not isinstance(assistant_message, str):
    assistant_message = json.dumps(assistant_message, ensure_ascii=False)

assistant_message = assistant_message.strip()
if not assistant_message:
    raise SystemExit(0)

event = {
    "kind": "message_assistant",
    "source_system": "claude-code",
    "session_id": session_id,
    "project_id": project_id,
    "actor_type": "assistant",
    "content": assistant_message,
    "metadata": {"hook_event": "AssistantResponse"},
    "occurred_at": now_rfc3339(),
}

print(json.dumps(event, ensure_ascii=False))
')

[ -n "$EVENT_JSON" ] && printf '%s' "$EVENT_JSON" | AMM_DB_PATH="$DB" "$AMM" ingest event --in - >/dev/null 2>&1
exit 0
