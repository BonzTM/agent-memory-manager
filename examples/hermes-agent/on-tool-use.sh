#!/usr/bin/env bash
set -euo pipefail

AMM="${AMM_BIN:-/usr/local/bin/amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"
SESSION_ID="${AMM_SESSION_ID:-$(uuidgen 2>/dev/null || python3 -c 'import uuid; print(uuid.uuid4())')}"
PROJECT_ID="${AMM_PROJECT_ID:-}"

PAYLOAD="$(cat)"
[ -n "$PAYLOAD" ] || exit 0

TOOL_NAME=$(printf '%s' "$PAYLOAD" | python3 -c 'import json, sys; data = json.load(sys.stdin); print(data.get("tool_name", ""))')
CALL_ID=$(printf '%s' "$PAYLOAD" | python3 -c 'import json, sys; data = json.load(sys.stdin); print(data.get("call_id", ""))')
STATUS=$(printf '%s' "$PAYLOAD" | python3 -c 'import json, sys; data = json.load(sys.stdin); print(data.get("status", ""))')

CALL_CONTENT=$(printf '%s' "$PAYLOAD" | python3 -c '
import json, sys
data = json.load(sys.stdin)
print(json.dumps({
    "tool_name": data.get("tool_name", ""),
    "tool_input": data.get("tool_input", "")
}))
')

RESULT_CONTENT=$(printf '%s' "$PAYLOAD" | python3 -c '
import json, sys
data = json.load(sys.stdin)
print(json.dumps(data.get("tool_output", "")))
')

HAS_RESULT=$(printf '%s' "$PAYLOAD" | python3 -c '
import json, sys
data = json.load(sys.stdin)
if "tool_output" in data and data.get("tool_output") not in (None, ""):
    print("1")
else:
    print("0")
')

echo "{
  \"kind\": \"tool_call\",
  \"source_system\": \"hermes-agent\",
  \"session_id\": \"$SESSION_ID\",
  \"project_id\": \"$PROJECT_ID\",
  \"actor_type\": \"tool\",
  \"content\": $CALL_CONTENT,
  \"metadata\": {\"hook_event\": \"tool_use\", \"tool_name\": $(printf '%s' "$TOOL_NAME" | python3 -c 'import sys, json; print(json.dumps(sys.stdin.read()))'), \"call_id\": $(printf '%s' "$CALL_ID" | python3 -c 'import sys, json; print(json.dumps(sys.stdin.read()))'), \"status\": $(printf '%s' "$STATUS" | python3 -c 'import sys, json; print(json.dumps(sys.stdin.read()))'), \"cwd\": $(printf '%s' "${PWD}" | python3 -c 'import sys, json; print(json.dumps(sys.stdin.read()))')},
  \"occurred_at\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"
}" | AMM_DB_PATH="$DB" "$AMM" ingest event --in - >/dev/null 2>&1

if [ "$HAS_RESULT" = "1" ]; then
  echo "{
  \"kind\": \"tool_result\",
  \"source_system\": \"hermes-agent\",
  \"session_id\": \"$SESSION_ID\",
  \"project_id\": \"$PROJECT_ID\",
  \"actor_type\": \"tool\",
  \"content\": $RESULT_CONTENT,
  \"metadata\": {\"hook_event\": \"tool_use\", \"tool_name\": $(printf '%s' "$TOOL_NAME" | python3 -c 'import sys, json; print(json.dumps(sys.stdin.read()))'), \"call_id\": $(printf '%s' "$CALL_ID" | python3 -c 'import sys, json; print(json.dumps(sys.stdin.read()))'), \"status\": $(printf '%s' "$STATUS" | python3 -c 'import sys, json; print(json.dumps(sys.stdin.read()))'), \"cwd\": $(printf '%s' "${PWD}" | python3 -c 'import sys, json; print(json.dumps(sys.stdin.read()))')},
  \"occurred_at\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"
}" | AMM_DB_PATH="$DB" "$AMM" ingest event --in - >/dev/null 2>&1
fi
