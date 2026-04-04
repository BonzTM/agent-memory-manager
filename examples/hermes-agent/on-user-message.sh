#!/usr/bin/env bash
set -euo pipefail

AMM="${AMM_BIN:-/usr/local/bin/amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"
SESSION_ID="${AMM_SESSION_ID:-$(uuidgen 2>/dev/null || python3 -c 'import uuid; print(uuid.uuid4())')}"
PROJECT_ID="${AMM_PROJECT_ID:-}"

if [ "$#" -gt 0 ]; then
  PROMPT="$1"
else
  PROMPT="$(cat)"
fi

[ -n "$PROMPT" ] || exit 0

echo "{
  \"kind\": \"message_user\",
  \"source_system\": \"hermes-agent\",
  \"session_id\": \"$SESSION_ID\",
  \"project_id\": \"$PROJECT_ID\",
  \"actor_type\": \"user\",
  \"content\": $(printf '%s' "$PROMPT" | python3 -c 'import sys, json; print(json.dumps(sys.stdin.read()))'),
  \"metadata\": {\"hook_event\": \"user_message\", \"cwd\": $(printf '%s' "${PWD}" | python3 -c 'import sys, json; print(json.dumps(sys.stdin.read()))')},
  \"occurred_at\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"
}" | AMM_DB_PATH="$DB" "$AMM" ingest event --in - >/dev/null 2>&1

RECALL=$(AMM_DB_PATH="$DB" "$AMM" recall --mode ambient --session "$SESSION_ID" --project "$PROJECT_ID" "$PROMPT" 2>/dev/null || echo '{}')

ITEMS=$(printf '%s' "$RECALL" | python3 -c '
import json, sys
try:
    data = json.load(sys.stdin)
    items = data.get("result", {}).get("items", [])
    if items:
        print("amm ambient memory recall (queried from the user\'s prompt — use amm_expand or `amm expand` with --max-depth 1 on any item ID for full context):")
        for item in items[:5]:
            kind = item.get("kind", "")
            desc = item.get("tight_description", "")
            score = item.get("score", 0)
            item_id = item.get("id", "")
            id_suffix = f" [{item_id}]" if item_id else ""
            print(f"- [{kind}] {desc} (score: {score:.2f}){id_suffix}")
except Exception:
    pass
' 2>/dev/null)

[ -n "$ITEMS" ] && printf '%s\n' "$ITEMS"
