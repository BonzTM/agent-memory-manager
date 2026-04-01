#!/usr/bin/env bash
# amm api-mode hook: capture tool use events via HTTP
set -euo pipefail

AMM_API_URL="${AMM_API_URL:-http://localhost:8080}"
HOOK_STATUS="${1:-success}"

PAYLOAD=""
if [ ! -t 0 ]; then
  PAYLOAD="$(cat || true)"
fi

[ -n "$PAYLOAD" ] || exit 0

EVENT_LINES=$(printf '%s' "$PAYLOAD" | HOOK_STATUS="$HOOK_STATUS" python3 -c '
import json, os, sys
from datetime import datetime, timezone

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
session_id = payload.get("session_id") or payload.get("sessionId") or ""
project_id = payload.get("project_id") or payload.get("projectId") or ""
cwd = payload.get("cwd") or os.environ.get("PWD", "")
status = os.environ.get("HOOK_STATUS", "success")

ts = datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")

if status == "pre":
    hook_event = "PreToolUse"
    succeeded = None
elif status == "failure":
    hook_event = "PostToolUseFailure"
    succeeded = False
else:
    hook_event = "PostToolUse"
    succeeded = True

if isinstance(tool_input, str):
    tool_input_text = tool_input
elif tool_input is None:
    tool_input_text = ""
else:
    tool_input_text = json.dumps(tool_input, ensure_ascii=False)

tool_call_event = {
    "kind": "tool_call",
    "source_system": "claude-code",
    "session_id": session_id,
    "project_id": project_id,
    "actor_type": "tool",
    "content": json.dumps({"tool_name": tool_name, "tool_input": tool_input}, ensure_ascii=False),
    "metadata": {
        "hook_event": hook_event,
        "tool_name": tool_name,
        "tool_input": tool_input_text,
        "cwd": cwd,
    },
    "occurred_at": ts,
}

if succeeded is not None:
    tool_call_event["metadata"]["succeeded"] = "true" if succeeded else "false"

print(json.dumps(tool_call_event, ensure_ascii=False))

if status == "pre":
    raise SystemExit(0)

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
        "hook_event": hook_event,
        "tool_name": tool_name,
        "tool_input": tool_input_text,
        "succeeded": "true" if succeeded else "false",
        "cwd": cwd,
    },
    "occurred_at": ts,
}

print(json.dumps(event, ensure_ascii=False))
')

if [ -n "$EVENT_LINES" ]; then
  while IFS= read -r EVENT_JSON; do
    [ -n "$EVENT_JSON" ] || continue
    curl -s -X POST "${AMM_API_URL}/v1/events" \
      -H "Content-Type: application/json" \
      -H "X-API-Key: ${AMM_API_KEY:-}" \
      -d "$EVENT_JSON" >/dev/null 2>&1 || true
  done <<< "$EVENT_LINES"
fi

exit 0
