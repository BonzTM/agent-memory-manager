# Codex Example

This directory contains the repo-shipped Codex MCP + hooks integration for AMM.

## Prerequisites

- `amm` and `amm-mcp` installed in your PATH
- `python3`
- An initialized database at `~/.amm/amm.db` or your chosen `AMM_DB_PATH`

## Files

- `config.toml` — registers `amm-mcp` as a Codex MCP server
- `hooks.json` — wires Codex hooks to the provided Python scripts
- `session-start.py` — records `session_start`
- `user-prompt-submit.py` — captures prompts and returns ambient recall context
- `session-stop.py` — imports transcript history when available and records `session_stop`

## Install

```bash
mkdir -p ~/.amm/hooks

cp examples/codex/session-start.py ~/.amm/hooks/codex-session-start.py
cp examples/codex/user-prompt-submit.py ~/.amm/hooks/codex-user-prompt-submit.py
cp examples/codex/session-stop.py ~/.amm/hooks/codex-stop.py
chmod +x ~/.amm/hooks/codex-*.py

cp examples/codex/config.toml ~/.codex/config.toml
cp examples/codex/hooks.json ~/.codex/hooks.json
```

If you already have Codex config, merge the `mcp_servers.amm` block and the `hooks` entries instead of overwriting existing files.

Replace placeholder paths such as `/home/you/.amm/amm.db` with your real paths. Set `AMM_PROJECT_ID` before running the hooks if you want project-scoped recall.

## Verify

```bash
AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm status
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm-mcp
```

For the complete operational model, see [`../../docs/codex-integration.md`](../../docs/codex-integration.md).
