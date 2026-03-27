# OpenCode Integration Guide

AMM fits OpenCode well as an MCP-first runtime with a small plugin layer. For HTTP API mode, see [API-mode examples](../examples/api-mode/) and [HTTP API Reference](http-api-reference.md).

The supported integration boundary is:

- **`amm-mcp` in `opencode.json`** for explicit amm tools
- **a local OpenCode plugin** for runtime glue (`shell.env`, message/tool capture, session lifecycle markers)
- **external amm workers** for heavier maintenance jobs

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

That exposes amm explicitly through OpenCode's documented MCP surface.

## 2. Add a local plugin for runtime glue

OpenCode automatically loads local plugins from:

- `~/.config/opencode/plugins/` for global plugins
- `.opencode/plugins/` for project plugins

The shipped `amm.js` plugin captures the full conversation transcript boundary exposed by OpenCode events:

- `shell.env` injects amm-related env vars into shell/tool execution
- `tool.execute.before` records durable `tool_call` events in amm
- `tool.execute.after` records durable `tool_result` events in amm
- `event` records `message_user` and `message_assistant` from `message.created` / final `message.updated` events with dedupe protections
- `event` also handles session lifecycle markers such as `session.created` and `session.idle`
- maintenance jobs are executed asynchronously with process timeouts and a filesystem lock (`$AMM_DB_PATH.opencode-maintenance.lock`) so OpenCode's event loop is not blocked and overlapping workers are skipped

## 3. Keep workers external

OpenCode plugins can trigger light amm jobs, but the heavy maintenance loop still belongs outside the runtime. Because SQLite is a single-writer system, we recommend running the **conservative baseline** maintenance jobs sequentially using the shared worker runner:

```bash
# Recommended: Serialized Baseline Runner
/path/to/agent-memory-manager/examples/scripts/run-workers.sh
```

The baseline runner covers the full maintenance sequence. Structural repairs like `repair_links` should be run separately via `amm repair --fix links` as needed.

Use host cron/systemd or the shared [`examples/scripts/run-workers.sh`](../examples/scripts/run-workers.sh) for the cold-path baseline.

## Default operator contract

OpenCode operators should use the same durable-memory loop as every other runtime:

- ask AMM for ambient recall at task start, repo switch, or resume
- expand only the AMM items needed for the current decision
- explicitly remember only stable, reusable knowledge
- use the OpenCode plugin only for the stable capture surfaces it really exposes
- keep heavier AMM jobs outside the OpenCode runtime

If the repo also uses ACM, ACM owns task workflow and AMM owns durable memory.

## Suggested usage pattern

- **Explicit memory**: OpenCode uses amm through MCP (`amm_recall`, `amm_expand`, `amm_remember`, `amm_jobs_run`)
- **Plugin glue**: OpenCode injects amm env vars and records message, tool-call, tool-result, and lifecycle markers
- **Background processing**: external worker invocations turn that event stream into summaries and memories

## Suggested repo instructions snippet

```md
## amm memory usage

- Treat amm as the durable memory system for this repository.
- At task start, repo switch, or resume after interruption, consult amm via `amm_recall` or `amm recall --mode ambient`.
- If amm returns thin recall items, expand only the items you actually need before acting.
- Record only stable, high-confidence memories explicitly with `amm_remember`; let background workers extract the rest from history.
- Do not assume amm runs its own scheduler. Maintenance jobs run externally via `amm jobs run <kind>`.
```

## What this repo ships today

- a real OpenCode MCP config example
- a real OpenCode local plugin example
- message + tool-call + tool-result capture into AMM event kinds (`message_user`, `message_assistant`, `tool_call`, `tool_result`)
- global install guidance that mirrors the same pattern we dogfood locally

## Current limits

- capture quality depends on OpenCode event payload shape and availability in your build
- a native OpenCode amm npm package is not published independently of this repo
