# Hermes-Agent Example

This directory contains the repo-shipped Hermes plugin example plus optional helper scripts for using AMM with Hermes-Agent.

## Prerequisites

- `amm` and optionally `amm-mcp` installed in your PATH for local-binary mode, or `amm-http` reachable over the network for API mode
- `python3`
- An initialized local database at `~/.amm/amm.db` or your chosen `AMM_DB_PATH`, or a reachable initialized AMM HTTP service

## Files

- `amm-memory/` — Hermes directory plugin that ingests `message_user` / `message_assistant` and injects ambient recall every turn through `pre_llm_call`
- `on-user-message.sh` — optional shell helper that ingests `message_user` and prints ambient recall hints
- `on-assistant-message.sh` — optional shell helper that ingests `message_assistant`
- `on-tool-use.sh` — optional shell helper that ingests `tool_call` and `tool_result`
- `on-session-end.sh` — optional warm-path maintenance runner

## Install The Plugin

Hermes loads directory plugins from `~/.hermes/plugins/<name>/`. Install the repo-shipped plugin example like this:

```bash
mkdir -p ~/.hermes/plugins
cp -R examples/hermes-agent/amm-memory ~/.hermes/plugins/amm-memory
```

For a project-local install instead, copy the same directory to `./.hermes/plugins/amm-memory` and start Hermes with `HERMES_ENABLE_PROJECT_PLUGINS=true`.

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
python3 -m py_compile examples/hermes-agent/amm-memory/__init__.py
```

Then, after copying the plugin into `~/.hermes/plugins/amm-memory`, start Hermes and confirm the plugin is loaded:

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
