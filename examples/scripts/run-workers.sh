#!/usr/bin/env bash
# Suitable for cron: */30 * * * * /path/to/run-workers.sh
set -euo pipefail

AMM="${AMM_BIN:-/usr/local/bin/amm}"
export AMM_DB_PATH="${AMM_DB_PATH:-$HOME/.amm/amm.db}"
LOCK_DIR="${AMM_DB_PATH}.maintenance.lock"
LAST_RUN_MARKER="${AMM_DB_PATH}.last-full-maintenance"

# Skip if DB doesn't exist
[ -f "$AMM_DB_PATH" ] || exit 0

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
  exit 0
fi

cleanup_lock() {
  rm -rf "$LOCK_DIR" 2>/dev/null || true
}
trap cleanup_lock EXIT INT TERM

run_housekeeping() {
  $AMM jobs run cleanup_recall_history >/dev/null 2>&1 || true
  $AMM jobs run purge_old_events >/dev/null 2>&1 || true
  $AMM jobs run purge_old_jobs >/dev/null 2>&1 || true
  $AMM jobs run expire_retrieval_cache >/dev/null 2>&1 || true
  $AMM jobs run purge_relevance_feedback >/dev/null 2>&1 || true
  $AMM jobs run vacuum_analyze >/dev/null 2>&1 || true
}

# Skip the full pipeline if the DB hasn't been modified since the last
# successful full run. The DB file's mtime changes on every write (event
# ingestion, reflect creating memories, etc.), so comparing it against a
# marker file tells us whether anything happened since we last processed.
db_changed_since_last_run() {
  [ ! -f "$LAST_RUN_MARKER" ] && return 0
  [ "$AMM_DB_PATH" -nt "$LAST_RUN_MARKER" ]
}

if ! db_changed_since_last_run; then
  run_housekeeping
  exit 0
fi

# Phase 1: Extract memories from events (LLM calls, no embeddings).
$AMM jobs run reflect >/dev/null 2>&1 || true

# Phase 2: Build embeddings for all new memories/summaries in batches.
# Runs early so downstream jobs (merge_duplicates, lifecycle_review, etc.)
# have embeddings available for semantic dedup and scoring. Individual jobs
# do not embed per-item — this single batched pass is far more efficient.
$AMM jobs run rebuild_indexes >/dev/null 2>&1 || true

# Phase 3: Compress, consolidate, and structure.
$AMM jobs run compress_history >/dev/null 2>&1 || true
$AMM jobs run consolidate_sessions >/dev/null 2>&1 || true
$AMM jobs run build_topic_summaries >/dev/null 2>&1 || true

# Phase 4: Dedup, enrich, and link.
$AMM jobs run merge_duplicates >/dev/null 2>&1 || true
$AMM jobs run extract_claims >/dev/null 2>&1 || true
$AMM jobs run enrich_memories >/dev/null 2>&1 || true
$AMM jobs run rebuild_entity_graph >/dev/null 2>&1 || true
$AMM jobs run form_episodes >/dev/null 2>&1 || true

# Phase 5: Quality and lifecycle.
$AMM jobs run detect_contradictions >/dev/null 2>&1 || true
$AMM jobs run decay_stale_memory >/dev/null 2>&1 || true
$AMM jobs run lifecycle_review >/dev/null 2>&1 || true
$AMM jobs run cross_project_transfer >/dev/null 2>&1 || true
$AMM jobs run archive_session_traces >/dev/null 2>&1 || true

# Phase 6: Final index rebuild (catches anything created in phases 3-5).
$AMM jobs run rebuild_indexes >/dev/null 2>&1 || true
$AMM jobs run cleanup_recall_history >/dev/null 2>&1 || true
$AMM jobs run update_ranking_weights >/dev/null 2>&1 || true

# Phase 7: DB trim and compaction (runs last, after all writes are done).
$AMM jobs run purge_old_events >/dev/null 2>&1 || true
$AMM jobs run purge_old_jobs >/dev/null 2>&1 || true
$AMM jobs run expire_retrieval_cache >/dev/null 2>&1 || true
$AMM jobs run purge_relevance_feedback >/dev/null 2>&1 || true
$AMM jobs run vacuum_analyze >/dev/null 2>&1 || true

touch "$LAST_RUN_MARKER"
