# Hermes-Agent Example

This directory contains both Hermes integration shapes shipped by the repo — the newer memory-provider example, the legacy hook plugin example, and optional helper scripts for using AMM with Hermes-Agent.

## Prerequisites

- `amm` and optionally `amm-mcp` installed in your PATH for local-binary mode, or `amm-http` reachable over the network for API mode
- `python3`
- An initialized local database at `~/.amm/amm.db` or your chosen `AMM_DB_PATH`, or a reachable initialized AMM HTTP service

## Files

- `memory/amm/` — Hermes external memory-provider example for newer Hermes builds
- `amm-legacy/` — legacy Hermes hook plugin for older builds or fallback installs
- `on-user-message.sh` — optional shell helper that ingests `message_user` and prints ambient recall hints
- `on-assistant-message.sh` — optional shell helper that ingests `message_assistant`
- `on-tool-use.sh` — optional shell helper that ingests `tool_call` and `tool_result`
- `on-session-end.sh` — optional warm-path maintenance runner

## Install The Recommended Provider

For Hermes builds with the external memory-provider architecture (v0.7.0+), install the AMM provider and set `memory.provider: amm` in `config.yaml`.

```bash
# Vendor into Hermes' plugin tree
mkdir -p ~/.hermes/plugins/memory/amm
cp examples/hermes-agent/memory/amm/plugin.yaml ~/.hermes/plugins/memory/amm/
cp examples/hermes-agent/memory/amm/__init__.py ~/.hermes/plugins/memory/amm/
```

Then in your Hermes `config.yaml`:

```yaml
memory:
  provider: amm
```

Configure the transport with env vars:

- **Local binary mode**: `AMM_BIN` (default `/usr/local/bin/amm`) + `AMM_DB_PATH`
- **HTTP API mode**: `AMM_API_URL` + optionally `AMM_API_KEY`

To enable curated-memory mirroring, set `AMM_HERMES_SYNC_CURATED_MEMORY=true`. See [`memory/amm/README.md`](memory/amm/README.md) for all env vars.

## Install The Legacy Hook Plugin

If your Hermes build does not expose the newer memory-provider architecture yet, install the legacy hook plugin instead:

```bash
mkdir -p ~/.hermes/plugins
cp -R examples/hermes-agent/amm-legacy ~/.hermes/plugins/amm-legacy
```

For a project-local install instead, copy the same directory to `./.hermes/plugins/amm-legacy` and start Hermes with `HERMES_ENABLE_PROJECT_PLUGINS=true`.

Recommended environment:

- `AMM_BIN` for local-binary mode
- `AMM_DB_PATH` for local-binary mode
- `AMM_API_URL` to switch the plugin from local-binary mode to HTTP API mode
- `AMM_API_KEY` when the AMM HTTP server requires bearer auth
- `AMM_PROJECT_ID` for a stable general plugin project identifier, especially outside CLI sessions
- `AMM_HERMES_CURATED_PROJECT_ID` when you want curated-memory parity writes pinned to a specific AMM project without changing the plugin's general project resolution
- `AMM_HERMES_RECALL_LIMIT` to override the default ambient recall block length (`5`)

Important:

- Do not wire `on-user-message.sh` or `on-assistant-message.sh` for the same Hermes hot path when the plugin is enabled. That will duplicate `message_user` / `message_assistant` events.
- The plugin is intentionally hot-path only. It does not run maintenance jobs automatically.
- When `AMM_API_URL` is set, the plugin uses the REST API (`/v1/events` and `/v1/recall`) instead of the local `amm` binary.

## Install Optional Helper Scripts

If you prefer shell-hook wiring, want explicit raw tool event capture, or need a separate session-end runner, copy the helper scripts somewhere Hermes can call them:

```bash
mkdir -p ~/.amm/hooks
cp examples/hermes-agent/*.sh ~/.amm/hooks/
chmod +x ~/.amm/hooks/*.sh
```

Pass these values from your Hermes hook handler when available:

- `AMM_DB_PATH`
- `AMM_PROJECT_ID`
- `AMM_SESSION_ID`
- stdin payloads or message text expected by the helper script

## Verify

```bash
# Check both plugins compile
python3 -m py_compile examples/hermes-agent/memory/amm/__init__.py
python3 -m py_compile examples/hermes-agent/amm-legacy/__init__.py
```

After installing either plugin, start Hermes and confirm it loads:

```bash
hermes plugins list
```

HTTP-mode smoke test:

```bash
curl -s http://localhost:8080/v1/status
```

Helper-only smoke test:

```bash
printf 'test message' | AMM_DB_PATH=~/.amm/amm.db ~/.amm/hooks/on-user-message.sh
AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm history --limit 5
```

For the runtime contract and MCP options, see [`../../docs/hermes-agent-integration.md`](../../docs/hermes-agent-integration.md).

