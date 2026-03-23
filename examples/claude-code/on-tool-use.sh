#!/usr/bin/env bash
set -euo pipefail

AMM="${AMM_BIN:-/usr/local/bin/amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"
HOOK_STATUS="${1:-success}"

PAYLOAD=""
if [ ! -t 0 ]; then
  PAYLOAD="$(cat || true)"
fi

[ -n "$PAYLOAD" ] || exit 0

EVENT_JSON=$(printf '%s' "$PAYLOAD" | HOOK_STATUS="$HOOK_STATUS" CLAUDE_SESSION_ID="${CLAUDE_SESSION_ID:-}" CLAUDE_PROJECT_ID="${CLAUDE_PROJECT_ID:-}" python3 -c '
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

tool_name = payload.get("tool_name") or payload.get("toolName") or "unknown_tool"
tool_input = payload.get("tool_input") or payload.get("toolInput")
tool_result = payload.get("tool_result") or payload.get("toolResult")
session_id = payload.get("session_id") or payload.get("sessionId") or os.environ.get("CLAUDE_SESSION_ID", "")
project_id = payload.get("project_id") or payload.get("projectId") or os.environ.get("CLAUDE_PROJECT_ID", "")
status = os.environ.get("HOOK_STATUS", "success")

if isinstance(tool_result, str):
    content = tool_result
elif tool_result is None:
    content = f"Claude tool {tool_name} completed with no structured result."
else:
    content = json.dumps(tool_result, ensure_ascii=False)

event = {
    "kind": "tool_result",
    "source_system": "claude-code",
    "session_id": session_id,
    "project_id": project_id,
    "actor_type": "tool",
    "content": content,
    "metadata": {
        "hook_event": "PostToolUse" if status == "success" else "PostToolUseFailure",
        "tool_name": tool_name,
        "tool_input": tool_input if isinstance(tool_input, str) or tool_input is None else json.dumps(tool_input, ensure_ascii=False),
        "succeeded": "true" if status == "success" else "false",
    },
    "occurred_at": now_rfc3339(),
}

print(json.dumps(event))
')

[ -n "$EVENT_JSON" ] && printf '%s' "$EVENT_JSON" | AMM_DB_PATH="$DB" "$AMM" ingest event --in - >/dev/null 2>&1
exit 0
