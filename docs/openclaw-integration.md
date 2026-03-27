# OpenClaw Integration Guide

OpenClaw is the runtime in this pairing; AMM is the memory substrate. For HTTP API mode, see [API-mode examples](../examples/api-mode/) and [HTTP API Reference](http-api-reference.md).

That distinction matters, because this repository only promises the AMM side of the boundary:

- `amm` for ingestion, recall, expansion, and maintenance jobs
- `amm-mcp` for stdio MCP access
- `AMM_DB_PATH` for selecting the SQLite database
- external `amm jobs run <kind>` worker execution

This repo now ships a **real OpenClaw example** under [`examples/openclaw/`](../examples/openclaw/). It is an integration bundle built from confirmed OpenClaw surfaces, not a native OpenClaw npm plugin package.

## Repo-Shipped Example

The example in [`examples/openclaw/`](../examples/openclaw/) uses three pieces:

1. **`openclaw.json`** — a real config example that wires `amm-mcp` through `plugins.entries.acpx.config.mcpServers`
2. **Native hooks** — repo-local `HOOK.md` + `handler.ts` directories loaded through `hooks.internal.load.extraDirs`
3. **External workers** — amm maintenance stays outside the runtime as `amm jobs run <kind>` or the shared [`examples/scripts/run-workers.sh`](../examples/scripts/run-workers.sh)

This is the recommended shape for a truthful OpenClaw integration today.

### 1. MCP Sidecar

Run `amm-mcp` as an OpenClaw-managed MCP subprocess. That gives agents explicit access to:

- `amm_recall`
- `amm_expand`
- `amm_remember`
- `amm_jobs_run`

The repo example uses the confirmed OpenClaw v2026.3.22 config path:

```json
{
  "plugins": {
    "entries": {
      "acpx": {
        "enabled": true,
        "config": {
          "mcpServers": {
            "amm": {
              "command": "/usr/local/bin/amm-mcp",
              "env": {
                "AMM_DB_PATH": "/home/you/.amm/amm.db"
              }
            }
          }
        }
      }
    }
  }
}
```

### 2. Hook Bridge for Capture

The shipped hooks are native OpenClaw hook directories, not pseudo-plugin files:

```text
examples/openclaw/
  openclaw.json
  README.md
  hooks/
    amm-memory-capture/
      HOOK.md
      handler.ts
    amm-session-maintenance/
      HOOK.md
      handler.ts
```

The example uses hooks for **capture and maintenance only**:

- `amm-memory-capture` listens to `message:preprocessed`, `message:sent`, `tool:called`, and `tool:completed` (plus compatible `function:*` payloads)
- `amm-session-maintenance` listens to `command:stop`

This guide intentionally does **not** claim that OpenClaw message hooks are a supported mutation surface for automatic ambient recall injection. The docs expose `bodyForAgent` for inspection, but they do not clearly document mutation semantics. Ambient recall therefore stays on the explicit MCP path.

## Responsibility Split

| Concern | OpenClaw owns | amm owns |
|---|---|---|
| Runtime lifecycle | hooks, background processes, plugin loading, scheduling | none |
| Memory storage | none | SQLite database and canonical memory/history records |
| Explicit memory tools | MCP subprocess management, tool exposure | `amm-mcp` implementation |
| Automatic capture | deciding when to fire hooks and what payload to pass | ingesting events |
| Explicit recall | deciding when agents call `amm_recall` or `amm_expand` | serving recall |
| Maintenance | deciding when jobs run | executing `reflect`, `compress_history`, and the other jobs |

## Practical Integration Flow

The repo-shipped flow looks like this:

1. OpenClaw receives an inbound message.
2. `amm-memory-capture` records the enriched inbound body as a `message_user` event.
3. When OpenClaw invokes a tool/function, `amm-memory-capture` records a `tool_call` event with tool name and input arguments.
4. When that tool/function returns, `amm-memory-capture` records a `tool_result` event with output content.
5. The agent can call `amm_recall` explicitly when memory context is needed.
6. When OpenClaw sends a reply, `amm-memory-capture` records a `message_assistant` event.
7. When `/stop` is issued, `amm-session-maintenance` records a stop event and runs the warm-path jobs.
8. On a longer cadence, a host-level scheduler runs the cold-path jobs against the same amm database.

That gives you a real OpenClaw integration without coupling amm to undocumented hook mutation internals.

## Example Files to Start From

- [`examples/openclaw/openclaw.json`](../examples/openclaw/openclaw.json)
- [`examples/openclaw/cron.add.reflect.json`](../examples/openclaw/cron.add.reflect.json)
- [`examples/openclaw/README.md`](../examples/openclaw/README.md)
- [`examples/openclaw/hooks/amm-memory-capture/HOOK.md`](../examples/openclaw/hooks/amm-memory-capture/HOOK.md)
- [`examples/openclaw/hooks/amm-memory-capture/handler.ts`](../examples/openclaw/hooks/amm-memory-capture/handler.ts)
- [`examples/openclaw/hooks/amm-session-maintenance/HOOK.md`](../examples/openclaw/hooks/amm-session-maintenance/HOOK.md)
- [`examples/openclaw/hooks/amm-session-maintenance/handler.ts`](../examples/openclaw/hooks/amm-session-maintenance/handler.ts)

## Worker Strategy

amm background jobs stay external. Because SQLite supports only one writer at a time, we recommend running the **conservative baseline** maintenance jobs sequentially:

- **Warm path**: the `command:stop` hook runs `reflect`, `compress_history`, and `consolidate_sessions` serially
- **Cold path**: host cron or systemd runs the full **baseline** maintenance sequence via `examples/scripts/run-workers.sh`

The sequence runs the baseline jobs in a deterministic order:

```bash
# Recommended: Serialized Baseline Runner
/path/to/agent-memory-manager/examples/scripts/run-workers.sh
```

The baseline runner now includes all maintenance jobs. Structural repairs like `repair_links` should be run separately via `amm repair --fix links` as needed.

For a shared baseline, reuse [`examples/scripts/run-workers.sh`](../examples/scripts/run-workers.sh).

That means you can choose any of these operational patterns:

- a host-level cron or systemd timer that invokes the serialized runner directly
- an OpenClaw-owned background process or native plugin service that shells out to `amm jobs run ...` sequentially
- an OpenClaw cron task that tells an isolated agent turn to call `amm_jobs_run` for a single job kind

The shipped example prefers the first host-level option for deterministic execution.

If you want the third option, start from [`examples/openclaw/cron.add.reflect.json`](../examples/openclaw/cron.add.reflect.json). That artifact is intentionally small: it schedules a recurring isolated turn, sets `delivery.mode` to `none`, and asks the agent to run only `amm_jobs_run({"kind":"reflect"})`. It is useful when you want OpenClaw to own the schedule, but it remains a secondary example because it spends an agent turn on maintenance and is less deterministic than direct host scheduling.

## Agent Instructions Snippet

If you want an OpenClaw-oriented instructions block, use something like this:

```md
## amm memory usage

- Treat amm as the durable memory system for this project.
- Use `amm_recall` or `amm recall --mode ambient` when prior context may matter.
- Expand only the amm items you actually need before acting.
- Use `amm_remember` for stable, high-confidence information such as decisions, preferences, and constraints.
- Do not assume amm schedules its own maintenance. Worker jobs run through external `amm jobs run <kind>` calls.
```

## Verification Checklist

- `amm-mcp` can be launched by OpenClaw as a subprocess
- the OpenClaw runtime can call `amm_recall` successfully
- the `amm-memory-capture` hook can ingest inbound/outbound message events and tool call/result events into amm history
- explicit `amm_recall` returns thin hints when the agent requests them
- session-end or scheduled jobs can run the serialized warm-path jobs (`reflect`, `compress_history`, `consolidate_sessions`)
- the same `AMM_DB_PATH` is visible to every OpenClaw-owned subprocess that calls amm

## What This Repo Does Not Promise

- a built-in amm scheduler or daemon
- a repo-shipped native OpenClaw npm plugin package
- a single mandatory OpenClaw configuration schema
- automatic context mutation through undocumented OpenClaw hook internals
