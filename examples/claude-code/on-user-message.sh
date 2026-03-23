#!/usr/bin/env bash
# AMM hook: capture user message and return ambient recall hints
# Install: cp to ~/.amm/hooks/on-user-message.sh && chmod +x

set -euo pipefail

AMM="${AMM_BIN:-/usr/local/bin/amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"

PROMPT="${1:-}"
SESSION_ID="${CLAUDE_SESSION_ID:-$(date +%Y%m%d)}"
PROJECT_ID="${CLAUDE_PROJECT_ID:-}"

# Skip empty prompts
[ -z "$PROMPT" ] && exit 0

# Ingest the user message as an event
echo "{
  \"kind\": \"message_user\",
  \"source_system\": \"claude-code\",
  \"session_id\": \"$SESSION_ID\",
  \"project_id\": \"$PROJECT_ID\",
  \"actor_type\": \"user\",
  \"content\": $(echo "$PROMPT" | python3 -c 'import sys,json; print(json.dumps(sys.stdin.read()))'),
  \"occurred_at\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"
}" | AMM_DB_PATH="$DB" "$AMM" ingest event --in - >/dev/null 2>&1

# Request ambient recall and output hints for injection
RECALL=$(AMM_DB_PATH="$DB" "$AMM" recall --mode ambient "$PROMPT" 2>/dev/null || echo '{}')

# Extract hints if any items returned
ITEMS=$(echo "$RECALL" | python3 -c '
import sys, json
try:
    data = json.load(sys.stdin)
    items = data.get("result", {}).get("items", [])
    if items:
        print("AMM recall hints:")
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
