#!/usr/bin/env bash
set -euo pipefail
AMM_API_URL="${AMM_API_URL:-http://localhost:8080}"
TYPE="${1:?Usage: $0 <type> <body> [tight_description]}"
BODY="${2:?Usage: $0 <type> <body> [tight_description]}"
TIGHT="${3:-$BODY}"
curl -s -X POST "${AMM_API_URL}/v1/memories" \
  -H "Content-Type: application/json" \
  -d "{\"type\": \"${TYPE}\", \"body\": \"${BODY}\", \"tight_description\": \"${TIGHT}\"}"
