#!/usr/bin/env bash
set -euo pipefail
AMM_API_URL="${AMM_API_URL:-http://localhost:8080}"
KIND="${1:?Usage: $0 <kind> <source_system> <content>}"
SOURCE="${2:?Usage: $0 <kind> <source_system> <content>}"
CONTENT="${3:?Usage: $0 <kind> <source_system> <content>}"
curl -s -X POST "${AMM_API_URL}/v1/events" \
  -H "Content-Type: application/json" \
  -H "X-API-Key: ${AMM_API_KEY:-}" \
  -d "{\"kind\": \"${KIND}\", \"source_system\": \"${SOURCE}\", \"content\": \"${CONTENT}\"}"
