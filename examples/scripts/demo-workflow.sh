#!/usr/bin/env bash
# amm Demo: Full workflow from ingest to recall
# Usage: ./demo-workflow.sh
set -euo pipefail

AMM="${AMM_BIN:-/usr/local/bin/amm}"
DB=$(mktemp -d)/demo.db
export AMM_DB_PATH="$DB"

# Helper to parse JSON results
parse_field() {
  python3 -c "import sys,json; d=json.load(sys.stdin); print($1)"
}

echo "=== amm Demo Workflow ==="
echo "Database: $DB"
echo ""

# 1. Initialize
echo "--- Step 1: Initialize ---"
$AMM init 2>/dev/null | parse_field '"  Initialized:", d["result"]["db_path"]'
echo ""

# 2. Ingest some events
echo "--- Step 2: Ingest Events ---"
for i in 1 2 3; do
  echo '{"kind":"message_user","source_system":"demo","session_id":"demo-session","privacy_level":"private","content":"I prefer to use Go for backend services because of its simplicity and performance","occurred_at":"2026-03-23T10:0'"${i}"':00Z"}' \
    | $AMM ingest event --in - | parse_field '"  Ingested:", d["result"]["id"]'
done

echo '{"kind":"message_user","source_system":"demo","session_id":"demo-session","privacy_level":"private","content":"We decided to use SQLite for the local data store","occurred_at":"2026-03-23T10:04:00Z"}' \
  | $AMM ingest event --in - | parse_field '"  Ingested:", d["result"]["id"]'
echo ""

# 3. Remember explicit memories
echo "--- Step 3: Remember Explicit Memories ---"
$AMM remember --type preference --scope global --subject "Developer" \
  --body "Prefers Go for backend development" \
  --tight "Prefers Go for backends" \
  | parse_field '"  Remembered:", d["result"]["id"], "("+d["result"]["type"]+")"'

$AMM remember --type decision --scope global --subject "amm" \
  --body "Using SQLite as the primary data store for local-first deployment" \
  --tight "SQLite for local storage" \
  | parse_field '"  Remembered:", d["result"]["id"], "("+d["result"]["type"]+")"'
echo ""

# 4. Run reflection
echo "--- Step 4: Reflect (auto-extract from events) ---"
$AMM jobs run reflect | parse_field '"  Memories created:", d["result"]["result"].get("memories_created", "0")'
echo ""

# 5. Compress history
echo "--- Step 5: Compress History ---"
$AMM jobs run compress_history | parse_field '"  Summaries created:", d["result"]["result"].get("summaries_created", "0")'
echo ""

# 6. Consolidate sessions
echo "--- Step 6: Consolidate Sessions ---"
$AMM jobs run consolidate_sessions | parse_field '"  Summaries created:", d["result"]["result"].get("summaries_created", "0")'
echo ""

# 7. Merge duplicates
echo "--- Step 7: Merge Duplicates ---"
$AMM jobs run merge_duplicates | parse_field '"  Merges performed:", d["result"]["result"].get("merges_performed", "0")'
echo ""

# 8. Extract claims
echo "--- Step 8: Extract Claims ---"
$AMM jobs run extract_claims | parse_field '"  Claims created:", d["result"]["result"].get("claims_created", "0")'
echo ""

# 9. Form episodes
echo "--- Step 9: Form Episodes ---"
$AMM jobs run form_episodes | parse_field '"  Episodes created:", d["result"]["result"].get("episodes_created", "0")'
echo ""

# 10. Detect contradictions
echo "--- Step 10: Detect Contradictions ---"
$AMM jobs run detect_contradictions | parse_field '"  Contradictions found:", d["result"]["result"].get("contradictions_found", "0")'
echo ""

# 11. Decay stale memories
echo "--- Step 11: Decay Stale Memories ---"
$AMM jobs run decay_stale_memory | parse_field '"  Memories decayed:", d["result"]["result"].get("memories_decayed", "0")'
echo ""

echo "--- Step 12: Archive Session Traces ---"
$AMM jobs run archive_session_traces | parse_field '"  Memories archived:", d["result"]["result"].get("memories_archived", "0")'
echo ""

# 13. Rebuild indexes
echo "--- Step 13: Rebuild Indexes ---"
$AMM jobs run rebuild_indexes | parse_field '"  Result:", d["result"]["result"].get("action", "done")'
echo ""

# 14. DB Trim and Compaction (Phase 7)
echo "--- Step 14: DB Trim and Compaction ---"
$AMM jobs run purge_old_events | parse_field '"  Events deleted:", d["result"]["result"].get("deleted", "0")'
$AMM jobs run purge_old_jobs | parse_field '"  Jobs deleted:", d["result"]["result"].get("deleted", "0")'
$AMM jobs run expire_retrieval_cache | parse_field '"  Cache entries expired:", d["result"]["result"].get("deleted", "0")'
$AMM jobs run purge_relevance_feedback | parse_field '"  Feedback rows deleted:", d["result"]["result"].get("deleted", "0")'
$AMM jobs run vacuum_analyze | parse_field '"  Status:", d["result"]["result"].get("status", "done")'
echo ""

# 15. Repair links
echo "--- Step 15: Repair Links ---"
$AMM repair --check | python3 -c "
import sys, json
d = json.load(sys.stdin)
r = d.get('result', {})
print('  Checked:', r.get('checked', 0), ' Issues:', r.get('issues', 0))
"
echo ""

# 16. Recall
echo "--- Step 16: Recall ---"
echo "  Ambient recall for 'Go backend':"
$AMM recall --mode ambient "Go backend" | python3 -c "
import sys, json
d = json.load(sys.stdin)
items = d.get('result', {}).get('items', [])
for item in items:
    print('    [%s] %s (score: %.2f)' % (item['kind'], item['tight_description'], item['score']))
if not items:
    print('    (no results)')
"
echo ""
echo "  Facts recall for 'SQLite':"
$AMM recall --mode facts "SQLite" | python3 -c "
import sys, json
d = json.load(sys.stdin)
items = d.get('result', {}).get('items', [])
for item in items:
    print('    [%s] %s (score: %.2f)' % (item['kind'], item['tight_description'], item['score']))
if not items:
    print('    (no results)')
"
echo ""

# 17. Status
echo "--- Step 17: Status ---"
$AMM status | python3 -c "
import sys, json
d = json.load(sys.stdin)
r = d['result']
print('  Events:   ', r['event_count'])
print('  Memories: ', r['memory_count'])
print('  Summaries:', r['summary_count'])
print('  Episodes: ', r['episode_count'])
"

echo ""
echo "=== Demo Complete ==="
echo "Database at: $DB"
