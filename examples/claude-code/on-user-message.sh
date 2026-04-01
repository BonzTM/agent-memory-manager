#!/usr/bin/env bash
# amm hook: capture user message and return ambient recall hints
# Install: cp to ~/.amm/hooks/on-user-message.sh && chmod +x

set -euo pipefail

AMM="${AMM_BIN:-/usr/local/bin/amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"

PAYLOAD=""
if [ ! -t 0 ]; then
  PAYLOAD="$(cat || true)"
fi

[ -n "$PAYLOAD" ] || exit 0

# Parse fields and build event from stdin JSON
EVENT_AND_PROMPT=$(printf '%s' "$PAYLOAD" | python3 -c '
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

session_id = payload.get("session_id") or payload.get("sessionId") or ""
project_id = payload.get("project_id") or payload.get("projectId") or ""
prompt = payload.get("prompt") or payload.get("message") or ""
cwd = payload.get("cwd") or os.environ.get("PWD", "")

if not prompt.strip():
    raise SystemExit(0)

event = {
    "kind": "message_user",
    "source_system": "claude-code",
    "session_id": session_id,
    "project_id": project_id,
    "actor_type": "user",
    "content": prompt,
    "metadata": {
        "hook_event": "UserMessage",
        "cwd": cwd,
    },
    "occurred_at": now_rfc3339(),
}

# Output event JSON on first line, prompt on second line
print(json.dumps(event, ensure_ascii=False))
print(prompt)
')

[ -n "$EVENT_AND_PROMPT" ] || exit 0

EVENT_JSON=$(echo "$EVENT_AND_PROMPT" | head -1)
PROMPT=$(echo "$EVENT_AND_PROMPT" | tail -n +2)

# Ingest the user message as an event
printf '%s' "$EVENT_JSON" | AMM_DB_PATH="$DB" "$AMM" ingest event --in - >/dev/null 2>&1

# Request ambient recall and output hints for injection
RECALL=$(AMM_DB_PATH="$DB" "$AMM" recall --mode ambient "$PROMPT" 2>/dev/null || echo '{}')

# Extract hints if any items returned
ITEMS=$(echo "$RECALL" | python3 -c '
import sys, json
try:
    data = json.load(sys.stdin)
    items = data.get("result", {}).get("items", [])
    if items:
        print("amm recall hints:")
        for item in items[:5]:
            desc = item.get("tight_description", "")
            kind = item.get("kind", "")
            score = item.get("score", 0)
            print(f"  [{kind}] {desc} (score: {score:.2f})")
except:
    pass
' 2>/dev/null)

[ -n "$ITEMS" ] && echo "$ITEMS"
exit 0
