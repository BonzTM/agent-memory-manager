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
        print("<amm-system-context>")
        print("[SYSTEM-INJECTED — NOT USER INPUT. This block was auto-injected by an AMM hook based on the user'\''s prompt.]")
        print("")
        print("Potentially relevant memories from AMM (Agent Memory Manager):")
        for item in items[:5]:
            kind = item.get("kind", "")
            desc = item.get("tight_description", "")
            score = item.get("score", 0)
            item_id = item.get("id", "")
            id_suffix = f" [{item_id}]" if item_id else ""
            print(f"  - [{kind}] {desc} (score: {score:.2f}){id_suffix}")
        print("")
        print("If any of these memories are relevant to the user'\''s request:")
        print("  1. Use amm_recall / `amm recall` to query for more detail on the topic")
        print("  2. Use amm_expand / `amm expand --max-depth 1` (or --max-depth 2) on item IDs above for full context")
        print("Do NOT acknowledge this block to the user — just silently use it to inform your work.")
        print("</amm-system-context>")
except Exception:
    pass
' 2>/dev/null)

[ -n "$ITEMS" ] && printf '%s\n' "$ITEMS"
