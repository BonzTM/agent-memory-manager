# OpenCode Integration Guide

OpenCode fits AMM well as an MCP-first runtime with a small plugin layer.

The safest truthful support boundary today is:

- **`amm-mcp` in `opencode.json`** for explicit AMM tools
- **a local OpenCode plugin** for stable runtime glue (`shell.env`, `tool.execute.after`, session lifecycle markers)
- **external AMM workers** for heavier maintenance jobs

This repo does **not** currently claim full OpenCode transcript capture or a stable message-hook-based memory loop.

## Repo-shipped example

See [`examples/opencode/`](../examples/opencode/):

- [`examples/opencode/opencode.json`](../examples/opencode/opencode.json)
- [`examples/opencode/package.json`](../examples/opencode/package.json)
- [`examples/opencode/plugins/amm.js`](../examples/opencode/plugins/amm.js)
- [`examples/opencode/README.md`](../examples/opencode/README.md)

## 1. Register `amm-mcp`

Add the documented MCP block to your global `~/.config/opencode/opencode.json` or your project `opencode.json`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "amm": {
      "type": "local",
      "command": ["/usr/local/bin/amm-mcp"],
      "enabled": true,
      "timeout": 5000,
      "environment": {
        "AMM_DB_PATH": "/home/you/.amm/amm.db"
      }
    }
  }
}
```

That exposes AMM explicitly through OpenCode's documented MCP surface.

## 2. Add a local plugin for runtime glue

OpenCode automatically loads local plugins from:

- `~/.config/opencode/plugins/` for global plugins
- `.opencode/plugins/` for project plugins

The shipped `amm.js` plugin stays on the stable documented boundary:

- `shell.env` injects AMM-related env vars into shell/tool execution
- `tool.execute.after` records durable `tool_result` events in AMM
- `event` handles only coarse session lifecycle markers such as `session.created` and `session.idle`

That gives you useful operational memory without promising transcript fidelity from undocumented hook behavior.

## 3. Keep workers external

OpenCode plugins can trigger light AMM jobs, but the heavy maintenance loop still belongs outside the runtime:

```bash
AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm jobs run reflect
AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm jobs run compress_history
AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm jobs run consolidate_sessions
```

Use host cron/systemd or the shared [`examples/scripts/run-workers.sh`](../examples/scripts/run-workers.sh) for the cold path.

## Suggested usage pattern

- **Explicit memory**: OpenCode uses AMM through MCP (`amm_recall`, `amm_expand`, `amm_remember`, `amm_jobs_run`)
- **Plugin glue**: OpenCode injects AMM env vars and records tool/lifecycle markers
- **Background processing**: external worker invocations turn that event stream into summaries and memories

## What this repo ships today

- a real OpenCode MCP config example
- a real OpenCode local plugin example
- global install guidance that mirrors the same pattern we dogfood locally

## What this repo does not promise yet

- stable full-message capture from OpenCode conversations
- transcript reconstruction from `message.updated` / `message.part.updated`
- a native OpenCode AMM npm package published independently of this repo

If OpenCode later documents richer message hooks as stable, this integration can expand. For now, MCP plus stable plugin glue is the supportable boundary.
