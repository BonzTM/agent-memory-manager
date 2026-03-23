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

# 7. Recall
echo "--- Step 7: Recall ---"
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

# 8. Status
echo "--- Step 8: Status ---"
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
