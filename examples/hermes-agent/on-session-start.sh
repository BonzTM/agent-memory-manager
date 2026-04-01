#!/usr/bin/env bash
set -euo pipefail

AMM="${AMM_BIN:-/usr/local/bin/amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"
SESSION_ID="${AMM_SESSION_ID:-$(uuidgen 2>/dev/null || python3 -c 'import uuid; print(uuid.uuid4())')}"
PROJECT_ID="${AMM_PROJECT_ID:-}"

echo "{
  \"kind\": \"session_start\",
  \"source_system\": \"hermes-agent\",
  \"session_id\": \"$SESSION_ID\",
  \"project_id\": \"$PROJECT_ID\",
  \"actor_type\": \"system\",
  \"content\": \"Hermes agent session started.\",
  \"metadata\": {\"hook_event\": \"session_start\", \"cwd\": $(printf '%s' "${PWD}" | python3 -c 'import sys, json; print(json.dumps(sys.stdin.read()))')},
  \"occurred_at\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"
}" | AMM_DB_PATH="$DB" "$AMM" ingest event --in - >/dev/null 2>&1

# Return ambient recall for session context
RECALL=$(AMM_DB_PATH="$DB" "$AMM" recall --mode ambient --session "$SESSION_ID" --project "$PROJECT_ID" "session start context" 2>/dev/null || echo '{}')

ITEMS=$(printf '%s' "$RECALL" | python3 -c '
import json, sys
try:
    data = json.load(sys.stdin)
    items = data.get("result", {}).get("items", [])
    if items:
        print("amm ambient recall:")
        for item in items[:5]:
            kind = item.get("kind", "")
            desc = item.get("tight_description", "")
            score = item.get("score", 0)
            print(f"- [{kind}] {desc} (score: {score:.2f})")
except Exception:
    pass
' 2>/dev/null)

[ -n "$ITEMS" ] && printf '%s\n' "$ITEMS"
