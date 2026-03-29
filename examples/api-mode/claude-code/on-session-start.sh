#!/usr/bin/env bash
set -euo pipefail
AMM_API_URL="${AMM_API_URL:-http://localhost:8080}"
QUERY="context for project: ${CLAUDE_PROJECT_ID:-unknown}"
curl -s -X POST "${AMM_API_URL}/v1/recall" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ${AMM_API_KEY:-}" \
  -d "{\"query\": \"${QUERY}\", \"opts\": {\"mode\": \"ambient\", \"limit\": 10}}"
