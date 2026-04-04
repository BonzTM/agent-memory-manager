#!/usr/bin/env bash
# AMM OpenClaw Plugin Installer
#
# One-command install for the AMM memory plugin into OpenClaw.
# Provides ambient recall injection and conversation event capture.
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

sed 's|transport-http\.ts|transport.ts|g' "$SCRIPT_DIR/index.ts"              > "$PLUGIN_DIR/index.ts"
sed 's|transport-http\.ts|transport.ts|g' "$SCRIPT_DIR/src/capture.ts"       > "$PLUGIN_DIR/src/capture.ts"
sed 's|transport-http\.ts|transport.ts|g' "$SCRIPT_DIR/src/recall.ts"        > "$PLUGIN_DIR/src/recall.ts"
sed 's|transport-http\.ts|transport.ts|g' "$SCRIPT_DIR/src/curated-sync.ts"  > "$PLUGIN_DIR/src/curated-sync.ts"

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

# Use python3 for safe JSON manipulation (handles trailing commas in JSONC)
python3 -c "
import json, os, re, sys

def load_jsonc(path):
    with open(path) as f:
        text = f.read()
    # Strip trailing commas before } or ] (JSONC -> JSON)
    text = re.sub(r',\s*([}\]])', r'\1', text)
    return json.loads(text)

config_path = '$OPENCLAW_CONFIG'
plugin_name = '$PLUGIN_NAME'
install_mcp = $( [ "$INSTALL_MCP" = true ] && echo True || echo False )
config_entries = '$CONFIG_ENTRIES'
amm_bin = '${AMM_BIN:-/usr/local/bin/amm}'
db_path = '${DB_PATH:-}'

config = load_jsonc(config_path)

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


# Configure amm-mcp as an MCP server so OpenClaw exposes
# AMM tools (amm_recall, amm_remember, amm_expand, etc.) to agents.
config.setdefault('mcp', {})
config['mcp'].setdefault('servers', {})
api_url = '$API_URL'
if 'amm' not in config['mcp']['servers']:
    if api_url:
        # HTTP transport: connect to remote amm-http MCP endpoint
        mcp_url = api_url.rstrip('/')
        if not mcp_url.endswith('/v1/mcp'):
            mcp_url = mcp_url.rstrip('/') + '/v1/mcp'
        config['mcp']['servers']['amm'] = {
            'url': mcp_url,
            'transport': 'streamable-http',
        }
        api_key_val = '$API_KEY'
        if api_key_val:
            config['mcp']['servers']['amm']['headers'] = {
                'Authorization': f'Bearer {api_key_val}',
            }
        print(f'  MCP server: configured (HTTP: {mcp_url})')
    else:
        # Stdio transport: spawn local amm-mcp binary
        config['mcp']['servers']['amm'] = {
            'command': amm_bin + '-mcp',
            'args': [],
            'env': {'AMM_DB_PATH': db_path or os.path.expanduser('~/.amm/amm.db')},
        }
        print('  MCP server: configured (amm-mcp)')
else:
    print('  MCP server: already configured')

# Ensure amm is in the allow list
config['plugins'].setdefault('allow', [])
if plugin_name not in config['plugins']['allow']:
    config['plugins']['allow'].append(plugin_name)
    print(f'  Added {plugin_name} to plugins.allow')

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

echo "Features: ambient recall injection + event capture"

echo ""
echo "To verify after restart:"
echo "  openclaw plugins list"
echo ""
echo "To uninstall:"
echo "  rm -rf $PLUGIN_DIR"
echo "  # Remove the '$PLUGIN_NAME' entry from $OPENCLAW_CONFIG"
