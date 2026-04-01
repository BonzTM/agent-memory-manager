#!/usr/bin/env bash
# amm api-mode hook: capture assistant message via HTTP
set -euo pipefail

AMM_API_URL="${AMM_API_URL:-http://localhost:8080}"

PAYLOAD=""
if [ ! -t 0 ]; then
  PAYLOAD="$(cat || true)"
fi

[ -n "$PAYLOAD" ] || exit 0

EVENT_JSON=$(printf '%s' "$PAYLOAD" | python3 -c '
import json, os, sys
from datetime import datetime, timezone

raw = sys.stdin.read().strip()
if not raw:
    raise SystemExit(0)

try:
    payload = json.loads(raw)
except json.JSONDecodeError:
    raise SystemExit(0)

session_id = payload.get("session_id") or payload.get("sessionId") or ""
project_id = payload.get("project_id") or payload.get("projectId") or ""
cwd = payload.get("cwd") or os.environ.get("PWD", "")
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

ts = datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")

event = {
    "kind": "message_assistant",
    "source_system": "claude-code",
    "session_id": session_id,
    "project_id": project_id,
    "actor_type": "assistant",
    "content": assistant_message,
    "metadata": {"hook_event": "AssistantResponse", "cwd": cwd},
    "occurred_at": ts,
}

print(json.dumps(event, ensure_ascii=False))
')

[ -n "$EVENT_JSON" ] || exit 0

curl -s -X POST "${AMM_API_URL}/v1/events" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ${AMM_API_KEY:-}" \
  -d "$EVENT_JSON" >/dev/null 2>&1 || true

exit 0
