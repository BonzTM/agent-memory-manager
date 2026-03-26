#!/usr/bin/env bash
set -euo pipefail

AMM="${AMM_BIN:-/usr/local/bin/amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"
SESSION_ID="${AMM_SESSION_ID:-$(date +%Y%m%d)}"
PROJECT_ID="${AMM_PROJECT_ID:-}"

if [ "$#" -gt 0 ]; then
  RESPONSE="$1"
else
  RESPONSE="$(cat)"
fi

[ -n "$RESPONSE" ] || exit 0

echo "{
  \"kind\": \"message_assistant\",
  \"source_system\": \"hermes-agent\",
  \"session_id\": \"$SESSION_ID\",
  \"project_id\": \"$PROJECT_ID\",
  \"actor_type\": \"assistant\",
  \"content\": $(printf '%s' "$RESPONSE" | python3 -c 'import sys, json; print(json.dumps(sys.stdin.read()))'),
  \"metadata\": {\"hook_event\": \"assistant_message\"},
  \"occurred_at\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"
}" | AMM_DB_PATH="$DB" "$AMM" ingest event --in - >/dev/null 2>&1
