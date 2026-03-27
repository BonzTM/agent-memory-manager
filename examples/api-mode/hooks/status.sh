#!/usr/bin/env bash
set -euo pipefail
AMM_API_URL="${AMM_API_URL:-http://localhost:8080}"
curl -s "${AMM_API_URL}/v1/status" | python3 -m json.tool
