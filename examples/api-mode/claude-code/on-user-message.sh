#!/usr/bin/env bash
# amm api-mode hook: capture user message via HTTP and return ambient recall
set -euo pipefail

AMM_API_URL="${AMM_API_URL:-http://localhost:8080}"

PAYLOAD=""
if [ ! -t 0 ]; then
  PAYLOAD="$(cat || true)"
fi

[ -n "$PAYLOAD" ] || exit 0

# Parse stdin JSON and build event + extract prompt
EVENT_AND_PROMPT=$(printf '%s' "$PAYLOAD" | python3 -c '
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
prompt = payload.get("prompt") or payload.get("message") or ""
cwd = payload.get("cwd") or os.environ.get("PWD", "")

if not prompt.strip():
    raise SystemExit(0)

ts = datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")

event = {
    "kind": "message_user",
    "source_system": "claude-code",
    "session_id": session_id,
    "project_id": project_id,
    "actor_type": "user",
    "content": prompt,
    "metadata": {"hook_event": "UserMessage", "cwd": cwd},
    "occurred_at": ts,
}

print(json.dumps(event, ensure_ascii=False))
print(prompt)
')

[ -n "$EVENT_AND_PROMPT" ] || exit 0

EVENT_JSON=$(echo "$EVENT_AND_PROMPT" | head -1)
PROMPT=$(echo "$EVENT_AND_PROMPT" | tail -n +2)

# Ingest user message event via HTTP
curl -s -X POST "${AMM_API_URL}/v1/events" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ${AMM_API_KEY:-}" \
  -d "$EVENT_JSON" >/dev/null 2>&1 || true

# Request ambient recall and output hints
RECALL=$(curl -s -X POST "${AMM_API_URL}/v1/recall" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ${AMM_API_KEY:-}" \
  -d "{\"query\": $(printf '%s' "$PROMPT" | python3 -c 'import sys,json; print(json.dumps(sys.stdin.read()))'), \"opts\": {\"mode\": \"ambient\", \"limit\": 5}}" 2>/dev/null || echo '{}')

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
