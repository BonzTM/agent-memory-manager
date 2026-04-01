#!/usr/bin/env bash
# AMM maintenance pipeline — runs all background jobs in dependency order.
# Suitable for cron: */30 * * * * /path/to/run-workers.sh
#
# Logging:
#   - Timestamps on every line (cron-friendly).
#   - Each job prints its name, duration, and exit status.
#   - Summary at the end: total jobs, passed, failed, duration.
#   - When run interactively (tty detected), output is unbuffered.
set -euo pipefail

AMM="${AMM_BIN:-/usr/local/bin/amm}"
export AMM_DB_PATH="${AMM_DB_PATH:-$HOME/.amm/amm.db}"
LOCK_DIR="${AMM_DB_PATH}.maintenance.lock"
LAST_RUN_MARKER="${AMM_DB_PATH}.last-full-maintenance"

# ── Logging ──────────────────────────────────────────────────────────────────

log() { printf '%s [amm-maintenance] %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$*"; }
log_phase() { log "── $1 ──"; }

# ── Pre-flight ───────────────────────────────────────────────────────────────

if [ ! -f "$AMM_DB_PATH" ]; then
  log "SKIP db not found at $AMM_DB_PATH"
  exit 0
fi

# ── Locking ──────────────────────────────────────────────────────────────────

acquire_lock() {
  if mkdir "$LOCK_DIR" 2>/dev/null; then
    printf '%s\n' "$$" > "$LOCK_DIR/pid"
    return 0
  fi

  if [ -f "$LOCK_DIR/pid" ]; then
    pid=$(cat "$LOCK_DIR/pid" 2>/dev/null || true)
    if [ -n "${pid:-}" ] && kill -0 "$pid" 2>/dev/null; then
      return 1
    fi
  fi

  rm -rf "$LOCK_DIR" 2>/dev/null || return 1
  mkdir "$LOCK_DIR" 2>/dev/null || return 1
  printf '%s\n' "$$" > "$LOCK_DIR/pid"
  return 0
}

if ! acquire_lock; then
  log "SKIP another maintenance run is active"
  exit 0
fi

cleanup_lock() { rm -rf "$LOCK_DIR" 2>/dev/null || true; }
trap cleanup_lock EXIT INT TERM

# ── Job runner ───────────────────────────────────────────────────────────────

TOTAL=0
PASSED=0
FAILED=0
PIPELINE_START=$(date +%s)

run_job() {
  local job="$1"
  local start end elapsed rc
  TOTAL=$((TOTAL + 1))
  start=$(date +%s)
  if $AMM jobs run "$job" 2>&1; then
    rc=0
  else
    rc=$?
  fi
  end=$(date +%s)
  elapsed=$((end - start))

  if [ "$rc" -eq 0 ]; then
    PASSED=$((PASSED + 1))
    log "OK   $job (${elapsed}s)"
  else
    FAILED=$((FAILED + 1))
    log "FAIL $job (${elapsed}s, exit=$rc)"
  fi
}

# ── Change detection ─────────────────────────────────────────────────────────
# Skip the full pipeline if the DB hasn't been modified since the last
# successful full run. Housekeeping still runs unconditionally.

db_changed_since_last_run() {
  [ ! -f "$LAST_RUN_MARKER" ] && return 0
  [ "$AMM_DB_PATH" -nt "$LAST_RUN_MARKER" ]
}

run_housekeeping() {
  log_phase "Housekeeping"
  for job in cleanup_recall_history purge_old_events purge_old_jobs \
             expire_retrieval_cache purge_relevance_feedback vacuum_analyze; do
    run_job "$job"
  done
}

if ! db_changed_since_last_run; then
  log "No DB changes since last full run — housekeeping only"
  run_housekeeping
  log "Done (housekeeping only): $PASSED passed, $FAILED failed"
  exit 0
fi

# ── Full pipeline ────────────────────────────────────────────────────────────

log "Starting full maintenance pipeline"

log_phase "Phase 1: Extract memories from events"
run_job reflect

log_phase "Phase 2: Build embeddings"
run_job rebuild_indexes

log_phase "Phase 3: Compress and structure"
for job in compress_history consolidate_sessions build_topic_summaries; do
  run_job "$job"
done

log_phase "Phase 4: Dedup, enrich, and link"
for job in merge_duplicates extract_claims enrich_memories \
           rebuild_entity_graph form_episodes; do
  run_job "$job"
done

log_phase "Phase 5: Quality and lifecycle"
for job in detect_contradictions decay_stale_memory lifecycle_review \
           cross_project_transfer archive_session_traces; do
  run_job "$job"
done

log_phase "Phase 6: Final index pass"
for job in rebuild_indexes cleanup_recall_history update_ranking_weights; do
  run_job "$job"
done

log_phase "Phase 7: DB trim and compaction"
for job in purge_old_events purge_old_jobs expire_retrieval_cache \
           purge_relevance_feedback vacuum_analyze; do
  run_job "$job"
done

touch "$LAST_RUN_MARKER"

PIPELINE_END=$(date +%s)
PIPELINE_ELAPSED=$((PIPELINE_END - PIPELINE_START))
log "Done: $TOTAL jobs ($PASSED passed, $FAILED failed) in ${PIPELINE_ELAPSED}s"
