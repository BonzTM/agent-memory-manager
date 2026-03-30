# Hermes-Agent Example

This directory contains the repo-shipped helper scripts for using AMM with Hermes-Agent.

## Prerequisites

- `amm` and optionally `amm-mcp` installed in your PATH
- `python3`
- An initialized database at `~/.amm/amm.db` or your chosen `AMM_DB_PATH`

## Files

- `on-user-message.sh` — ingests `message_user` and prints ambient recall hints
- `on-assistant-message.sh` — ingests `message_assistant`
- `on-tool-use.sh` — ingests `tool_call` and `tool_result`
- `on-session-end.sh` — runs warm-path maintenance jobs

## Install

Copy these scripts somewhere Hermes can call them from its hook system:

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
printf 'test message' | AMM_DB_PATH=~/.amm/amm.db ~/.amm/hooks/on-user-message.sh
AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm history --limit 5
```

For the runtime contract and MCP options, see [`../../docs/hermes-agent-integration.md`](../../docs/hermes-agent-integration.md).
