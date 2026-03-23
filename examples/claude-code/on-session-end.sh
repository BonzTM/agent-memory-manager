#!/usr/bin/env bash
# AMM hook: run background processing when session ends
set -euo pipefail

AMM="${AMM_BIN:-amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"

# Run reflection to extract memories from session events
AMM_DB_PATH="$DB" "$AMM" jobs run reflect >/dev/null 2>&1 || true

# Compress history into summaries
AMM_DB_PATH="$DB" "$AMM" jobs run compress_history >/dev/null 2>&1 || true

# Consolidate session summaries
AMM_DB_PATH="$DB" "$AMM" jobs run consolidate_sessions >/dev/null 2>&1 || true

# Extract claims from new memories
AMM_DB_PATH="$DB" "$AMM" jobs run extract_claims >/dev/null 2>&1 || true

# Form episodes
AMM_DB_PATH="$DB" "$AMM" jobs run form_episodes >/dev/null 2>&1 || true

exit 0
