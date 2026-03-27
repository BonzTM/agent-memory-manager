#!/usr/bin/env bash
# Generic AMM recall via HTTP API
# Usage: AMM_API_URL=http://localhost:8080 ./recall.sh "search query"
set -euo pipefail
AMM_API_URL="${AMM_API_URL:-http://localhost:8080}"
QUERY="${1:?Usage: $0 <query>}"
curl -s -X POST "${AMM_API_URL}/v1/recall" \
  -H "Content-Type: application/json" \
  -d "{\"query\": \"${QUERY}\", \"opts\": {\"mode\": \"ambient\", \"limit\": 20}}"
