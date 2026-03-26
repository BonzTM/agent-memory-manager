# OpenClaw Example

This directory ships a **real OpenClaw v2026.3.22 example** for amm.

It is a repo-local integration bundle, not a native OpenClaw npm plugin package. The example stays inside confirmed OpenClaw surfaces:

- `~/.openclaw/openclaw.json` configuration
- `plugins.entries.acpx.config.mcpServers` for `amm-mcp`
- native Gateway hooks with `HOOK.md` + `handler.ts`
- external amm workers via `amm jobs run <kind>` or the shared `examples/scripts/run-workers.sh`

## Included Files

- `openclaw.json` — example OpenClaw config fragment that wires `amm-mcp` and loads the hook directories in this folder
- `cron.add.reflect.json` — optional `cron.add` payload for an OpenClaw-owned isolated maintenance turn that calls `amm_jobs_run`
- `hooks/amm-memory-capture/` — native OpenClaw hook that captures inbound and outbound message events into amm
- `hooks/amm-session-maintenance/` — native OpenClaw hook that runs light amm maintenance on `command:stop`

## What This Example Does

1. Exposes amm to OpenClaw through MCP
2. Captures enriched inbound messages from `message:preprocessed`
3. Captures outbound messages from `message:sent`
4. Runs warm-path maintenance (`reflect`, `compress_history`, `consolidate_sessions`) when `/stop` is issued

## What This Example Does Not Do

- It does **not** mutate OpenClaw message bodies to inject ambient recall automatically.
- It does **not** ship `openclaw.plugin.json` or a native plugin package.
- It does **not** make OpenClaw cron run arbitrary shell commands directly.

Ambient recall stays on the explicit MCP path through `amm_recall`.

## Install Steps

1. Build and install `amm` and `amm-mcp`.
2. Copy or merge the contents of `openclaw.json` into `~/.openclaw/openclaw.json`.
3. Replace the placeholder absolute paths with your real paths.
4. Restart OpenClaw or let its config hot reload apply the changes.
5. Verify the hooks are visible:

```bash
openclaw hooks list
openclaw hooks check
```

## Worker Scheduling

The example uses a split maintenance model designed for SQLite's single-writer constraint:

- **Warm path**: `hooks/amm-session-maintenance` runs `reflect`, `compress_history`, and `consolidate_sessions` serially on `command:stop`
- **Cold path**: use a host-level cron or systemd timer to run the **serialized baseline sequence** via `examples/scripts/run-workers.sh`

Example host cron entry:

```cron
# Baseline maintenance sequence
*/30 * * * * AMM_DB_PATH=/home/you/.amm/amm.db /home/you/src/agent-memory-manager/examples/scripts/run-workers.sh
```

The baseline runner now covers the full maintenance sequence. Structural repairs like `repair_links` should be run separately via `amm repair --fix links` as needed.

The smallest optional variant is [`cron.add.reflect.json`](./cron.add.reflect.json). It creates a recurring isolated turn that asks the agent to call `amm_jobs_run` with `{"kind":"reflect"}` and sets `delivery.mode` to `none`, so the run stays internal unless the turn itself fails.

Example install command:

```bash
openclaw gateway call cron.add --params "$(cat /home/you/src/agent-memory-manager/examples/openclaw/cron.add.reflect.json)"
```

Use this only if you specifically want OpenClaw to own the schedule. The host-level cron or systemd path above remains the default because it is more deterministic and avoids spending an agent turn on routine maintenance.

## Operational Notes

- Set `AMM_PROJECT_ID` to a stable project identifier if you want project-scoped recall.
- Keep `AMM_DB_PATH` identical across `amm`, `amm-mcp`, hooks, and external worker invocations.
- Use MCP for explicit amm access (`amm_recall`, `amm_expand`, `amm_remember`, `amm_jobs_run`).
