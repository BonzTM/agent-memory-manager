#!/usr/bin/env bash
# AMM OpenClaw Plugin Installer
#
# One-command install for the AMM memory plugin into OpenClaw.
# Claims the memory slot, providing memory_search/memory_get tools,
# ambient recall injection, and conversation event capture.
#
# Usage:
#   # Install from npm (published package)
#   openclaw plugins install @bonztm/amm
#
#   # Install from local directory (development / release binary)
#   ./install.sh [options]
#
# Options:
#   --amm-bin PATH        Path to the amm binary (default: amm on PATH)
#   --db-path PATH        Path to the amm SQLite database (default: ~/.amm/amm.db)
#   --api-url URL         Use HTTP API mode instead of local binary
#   --api-key KEY         API key for HTTP API mode
#   --project-id ID       Default project ID for scoped recall
#   --recall-limit N      Max ambient recall items per turn (default: 5)
#   --slot                Claim the memory slot (default: true)
#   --no-slot             Don't claim the memory slot (hooks only)
#   --mcp                 Also configure amm-mcp as an MCP sidecar
#   --help                Show this help message

set -euo pipefail

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------

OPENCLAW_DIR="${OPENCLAW_DIR:-$HOME/.openclaw}"
OPENCLAW_CONFIG="${OPENCLAW_DIR}/openclaw.json"
PLUGIN_NAME="amm"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

AMM_BIN=""
DB_PATH=""
API_URL=""
API_KEY=""
PROJECT_ID=""
RECALL_LIMIT=""
CLAIM_SLOT=true
INSTALL_MCP=false

# ---------------------------------------------------------------------------
# Parse args
# ---------------------------------------------------------------------------

while [[ $# -gt 0 ]]; do
  case "$1" in
    --amm-bin)     AMM_BIN="$2"; shift 2 ;;
    --db-path)     DB_PATH="$2"; shift 2 ;;
    --api-url)     API_URL="$2"; shift 2 ;;
    --api-key)     API_KEY="$2"; shift 2 ;;
    --project-id)  PROJECT_ID="$2"; shift 2 ;;
    --recall-limit) RECALL_LIMIT="$2"; shift 2 ;;
    --slot)        CLAIM_SLOT=true; shift ;;
    --no-slot)     CLAIM_SLOT=false; shift ;;
    --mcp)         INSTALL_MCP=true; shift ;;
    --help|-h)
      sed -n '2,/^$/{ s/^# //; s/^#$//; p }' "$0"
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

# ---------------------------------------------------------------------------
# Pre-flight
# ---------------------------------------------------------------------------

echo "AMM OpenClaw Plugin Installer"
echo "=============================="
echo ""

if [ ! -d "$OPENCLAW_DIR" ]; then
  echo "Error: OpenClaw directory not found at $OPENCLAW_DIR"
  echo "Make sure OpenClaw is installed. See https://openclaws.io"
  exit 1
fi

# Verify amm is available (unless using HTTP API mode)
if [ -z "$API_URL" ]; then
  AMM_CHECK="${AMM_BIN:-amm}"
  if ! command -v "$AMM_CHECK" >/dev/null 2>&1; then
    echo "Error: amm binary not found on PATH."
    echo "Install amm first, or use --api-url for HTTP mode."
    exit 1
  fi
  echo "  amm binary: $(command -v "$AMM_CHECK")"
fi

# ---------------------------------------------------------------------------
# Install plugin files
# ---------------------------------------------------------------------------

PLUGIN_DIR="${OPENCLAW_DIR}/extensions/${PLUGIN_NAME}"
echo "  Installing to: $PLUGIN_DIR"

mkdir -p "$PLUGIN_DIR/src"

cp "$SCRIPT_DIR/openclaw.plugin.json"  "$PLUGIN_DIR/openclaw.plugin.json"
cp "$SCRIPT_DIR/package.json"          "$PLUGIN_DIR/package.json"
cp "$SCRIPT_DIR/src/config.ts"         "$PLUGIN_DIR/src/config.ts"

# Local install includes the full dual-transport (binary + HTTP).
# The npm-published package uses transport-http.ts only (scanner-safe).
# Rewrite imports to use the full transport.ts for binary mode support.
cp "$SCRIPT_DIR/src/transport.ts"        "$PLUGIN_DIR/src/transport.ts"
cp "$SCRIPT_DIR/src/transport-http.ts"   "$PLUGIN_DIR/src/transport-http.ts"

sed 's|transport-http\.ts|transport.ts|g' "$SCRIPT_DIR/index.ts"       > "$PLUGIN_DIR/index.ts"
sed 's|transport-http\.ts|transport.ts|g' "$SCRIPT_DIR/src/capture.ts" > "$PLUGIN_DIR/src/capture.ts"
sed 's|transport-http\.ts|transport.ts|g' "$SCRIPT_DIR/src/recall.ts"  > "$PLUGIN_DIR/src/recall.ts"

echo "  Plugin files copied."

# ---------------------------------------------------------------------------
# Build plugin config
# ---------------------------------------------------------------------------

CONFIG_ENTRIES=""
[ -n "$AMM_BIN" ]      && CONFIG_ENTRIES="${CONFIG_ENTRIES}\"ammBin\": \"$AMM_BIN\", "
[ -n "$DB_PATH" ]      && CONFIG_ENTRIES="${CONFIG_ENTRIES}\"dbPath\": \"$DB_PATH\", "
[ -n "$API_URL" ]      && CONFIG_ENTRIES="${CONFIG_ENTRIES}\"apiUrl\": \"$API_URL\", "
[ -n "$API_KEY" ]      && CONFIG_ENTRIES="${CONFIG_ENTRIES}\"apiKey\": \"$API_KEY\", "
[ -n "$PROJECT_ID" ]   && CONFIG_ENTRIES="${CONFIG_ENTRIES}\"projectId\": \"$PROJECT_ID\", "
[ -n "$RECALL_LIMIT" ] && CONFIG_ENTRIES="${CONFIG_ENTRIES}\"recallLimit\": $RECALL_LIMIT, "

# Strip trailing comma-space
CONFIG_ENTRIES="${CONFIG_ENTRIES%, }"

# ---------------------------------------------------------------------------
# Update openclaw.json
# ---------------------------------------------------------------------------

if [ ! -f "$OPENCLAW_CONFIG" ]; then
  echo '{}' > "$OPENCLAW_CONFIG"
fi

# Use python3 for safe JSON manipulation
python3 -c "
import json, sys

config_path = '$OPENCLAW_CONFIG'
plugin_name = '$PLUGIN_NAME'
claim_slot = $( [ "$CLAIM_SLOT" = true ] && echo True || echo False )
install_mcp = $( [ "$INSTALL_MCP" = true ] && echo True || echo False )
config_entries = '$CONFIG_ENTRIES'
amm_bin = '${AMM_BIN:-/usr/local/bin/amm}'
db_path = '${DB_PATH:-}'

with open(config_path) as f:
    config = json.load(f)

# Ensure structure
config.setdefault('plugins', {})
config['plugins'].setdefault('entries', {})

# Add/update plugin entry
plugin_config = {}
if config_entries:
    # Parse the config entries
    plugin_config = json.loads('{' + config_entries + '}')

config['plugins']['entries'][plugin_name] = {
    'enabled': True,
    'config': plugin_config,
}

# Claim memory slot if requested
if claim_slot:
    config['plugins'].setdefault('slots', {})
    config['plugins']['slots']['memory'] = plugin_name
    print(f'  Memory slot: claimed by {plugin_name}')
else:
    print(f'  Memory slot: not claimed (hooks only)')

# Add MCP sidecar if requested
if install_mcp:
    config['plugins']['entries'].setdefault('acpx', {'enabled': True, 'config': {}})
    config['plugins']['entries']['acpx'].setdefault('config', {})
    config['plugins']['entries']['acpx']['config'].setdefault('mcpServers', {})
    config['plugins']['entries']['acpx']['config']['mcpServers']['amm'] = {
        'command': amm_bin,
        'args': ['--mcp'],
        'env': {'AMM_DB_PATH': db_path or '~/.amm/amm.db'},
    }
    print('  MCP sidecar: configured via acpx')

with open(config_path, 'w') as f:
    json.dump(config, f, indent=2)
    f.write('\n')

print(f'  Config updated: {config_path}')
"

# ---------------------------------------------------------------------------
# Verify
# ---------------------------------------------------------------------------

echo ""
echo "Installation complete!"
echo ""
echo "Restart OpenClaw to activate the plugin."
echo ""

if [ -n "$API_URL" ]; then
  echo "Mode: HTTP API ($API_URL)"
else
  echo "Mode: Local binary ($(command -v "${AMM_BIN:-amm}"))"
fi

if [ "$CLAIM_SLOT" = true ]; then
  echo "Slot: memory (replaces OpenClaw built-in memory-core)"
  echo ""
  echo "The agent now has memory_search and memory_get tools"
  echo "plus automatic ambient recall on every turn."
else
  echo "Slot: none (hooks only — ambient recall + event capture)"
fi

echo ""
echo "To verify after restart:"
echo "  openclaw plugins list"
echo ""
echo "To uninstall:"
echo "  rm -rf $PLUGIN_DIR"
echo "  # Remove the '$PLUGIN_NAME' entry from $OPENCLAW_CONFIG"
