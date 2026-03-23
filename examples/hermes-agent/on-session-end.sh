#!/usr/bin/env bash
set -euo pipefail

AMM="${AMM_BIN:-amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"

AMM_DB_PATH="$DB" "$AMM" jobs run reflect >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run compress_history >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run consolidate_sessions >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run form_episodes >/dev/null 2>&1 || true
