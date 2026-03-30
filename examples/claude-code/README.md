# Claude Code Example

This directory contains the repo-shipped Claude Code reference configuration for AMM.

## Prerequisites

- `amm`, `amm-mcp`, and optionally `amm-http` installed in your PATH
- `jq`
- `python3`
- An initialized database at `~/.amm/amm.db` or your chosen `AMM_DB_PATH`

## Files

- `claude.json` — MCP server registration for `amm-mcp`
- `settings.json` — hook registrations for capture and stop-time maintenance
- `on-user-message.sh` — captures prompts and returns ambient recall hints
- `on-assistant-message.sh` — captures assistant replies
- `on-tool-use.sh` — captures tool calls and results
- `on-session-end.sh` — records `session_stop` and runs light maintenance jobs

## Install

```bash
mkdir -p ~/.amm/hooks

cp examples/claude-code/*.sh ~/.amm/hooks/
chmod +x ~/.amm/hooks/*.sh

cp examples/claude-code/claude.json ~/.claude.json
cp examples/claude-code/settings.json ~/.claude/settings.json
```

If you already have Claude config files, merge only the `mcpServers.amm` and `hooks` sections instead of overwriting them.

Replace `/home/you/.amm/amm.db` with your real database path if you do not use the default location.

## Verify

```bash
AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm status
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm-mcp
```

Expected: `amm status` reports `initialized: true`, and `amm-mcp` returns `serverInfo.name: "amm-mcp"`.

For the full runtime walkthrough, see [`../../docs/agent-onboarding.md`](../../docs/agent-onboarding.md).
