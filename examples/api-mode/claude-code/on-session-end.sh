#!/usr/bin/env bash
set -euo pipefail
AMM_API_URL="${AMM_API_URL:-http://localhost:8080}"
SESSION_CONTENT="Claude Code session ended for project: ${CLAUDE_PROJECT_ID:-unknown}"
curl -s -X POST "${AMM_API_URL}/v1/events" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ${AMM_API_KEY:-}" \
  -d "{\"kind\": \"session_stop\", \"source_system\": \"claude-code\", \"project_id\": \"${CLAUDE_PROJECT_ID:-unknown}\", \"content\": \"${SESSION_CONTENT}\", \"metadata\": {\"hook_event\": \"session_end\", \"cwd\": $(printf '%s' "${PWD}" | python3 -c 'import sys, json; print(json.dumps(sys.stdin.read()))')}}"
