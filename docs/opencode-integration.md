# OpenCode Integration Guide

OpenCode fits amm well as an MCP-first runtime with a small plugin layer.

The safest truthful support boundary today is:

- **`amm-mcp` in `opencode.json`** for explicit amm tools
- **a local OpenCode plugin** for stable runtime glue (`shell.env`, `tool.execute.after`, session lifecycle markers)
- **external amm workers** for heavier maintenance jobs

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

That exposes amm explicitly through OpenCode's documented MCP surface.

## 2. Add a local plugin for runtime glue

OpenCode automatically loads local plugins from:

- `~/.config/opencode/plugins/` for global plugins
- `.opencode/plugins/` for project plugins

The shipped `amm.js` plugin stays on the stable documented boundary:

- `shell.env` injects amm-related env vars into shell/tool execution
- `tool.execute.after` records durable `tool_result` events in amm
- `event` handles only coarse session lifecycle markers such as `session.created` and `session.idle`
- maintenance jobs are executed asynchronously with process timeouts and a filesystem lock (`$AMM_DB_PATH.opencode-maintenance.lock`) so OpenCode's event loop is not blocked and overlapping workers are skipped

That gives you useful operational memory without promising transcript fidelity from undocumented hook behavior.

## 3. Keep workers external

OpenCode plugins can trigger light amm jobs, but the heavy maintenance loop still belongs outside the runtime. Because SQLite is a single-writer system, we recommend running the **conservative baseline** maintenance jobs sequentially using the shared worker runner:

```bash
# Recommended: Serialized Baseline Runner
/path/to/agent-memory-manager/examples/scripts/run-workers.sh
```

The baseline runner covers essential maintenance. Aggressive jobs (`decay_stale_memory`, `merge_duplicates`) or low-cadence repairs (`rebuild_indexes`) should be run separately. Structural repairs like `repair_links` should only be run via `amm repair --fix links`.

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
- **Plugin glue**: OpenCode injects amm env vars and records tool/lifecycle markers
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
- global install guidance that mirrors the same pattern we dogfood locally

## What this repo does not promise yet

- stable full-message capture from OpenCode conversations
- transcript reconstruction from `message.updated` / `message.part.updated`
- a native OpenCode amm npm package published independently of this repo

If OpenCode later documents richer message hooks as stable, this integration can expand. For now, MCP plus stable plugin glue is the supportable boundary.
