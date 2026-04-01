#!/usr/bin/env bash
set -euo pipefail
AMM_API_URL="${AMM_API_URL:-http://localhost:8080}"
PROJECT_ID="${CLAUDE_PROJECT_ID:-unknown}"

# Emit session_start event
curl -s -X POST "${AMM_API_URL}/v1/events" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ${AMM_API_KEY:-}" \
  -d "{\"kind\": \"session_start\", \"source_system\": \"claude-code\", \"project_id\": \"${PROJECT_ID}\", \"content\": \"Claude Code session started.\", \"metadata\": {\"hook_event\": \"session_start\", \"cwd\": $(printf '%s' "${PWD}" | python3 -c 'import sys, json; print(json.dumps(sys.stdin.read()))')}}" >/dev/null 2>&1 || true

# Return ambient recall for session context
QUERY="context for project: ${PROJECT_ID}"
curl -s -X POST "${AMM_API_URL}/v1/recall" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ${AMM_API_KEY:-}" \
  -d "{\"query\": \"${QUERY}\", \"opts\": {\"mode\": \"ambient\", \"limit\": 10}}"
