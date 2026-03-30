#!/usr/bin/env bash
set -euo pipefail
AMM_API_URL="${AMM_API_URL:-http://localhost:8080}"
SESSION_CONTENT="Claude Code session ended for project: ${CLAUDE_PROJECT_ID:-unknown}"
curl -s -X POST "${AMM_API_URL}/v1/events" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ${AMM_API_KEY:-}" \
  -d "{\"kind\": \"session_stop\", \"source_system\": \"claude-code\", \"content\": \"${SESSION_CONTENT}\"}"
