#!/usr/bin/env bash
set -euo pipefail

AMM="${AMM_BIN:-/usr/local/bin/amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"

AMM_DB_PATH="$DB" "$AMM" jobs run reflect >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run rebuild_indexes >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run compress_history >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run consolidate_sessions >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run form_episodes >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run enrich_memories >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run rebuild_entity_graph >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run lifecycle_review >/dev/null 2>&1 || true
