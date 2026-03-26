# Hermes-Agent Integration Guide

Hermes and amm fit together cleanly as a sidecar pattern:

- **Hermes-agent** owns the runtime, hooks, scheduling, and agent behavior.
- **amm** owns durable history, recall, summaries, and maintenance jobs.

That means the integration contract stays simple:

- register `amm-mcp` if you want explicit amm tools inside Hermes
- use Hermes hooks or hook handlers to call amm-side helper scripts for capture/recall
- keep maintenance jobs as external `amm jobs run <kind>` calls against the same database

## Recommended Shape

Use Hermes and amm in three layers:

1. **MCP** for explicit agent-controlled memory access
2. **Hooks** for transparent event capture and ambient recall on the hot path
3. **Cron or scheduled jobs** for reflection, compression, and heavier maintenance

## 1. Register `amm-mcp`

Hermes supports MCP, so the lowest-friction explicit integration is to register `amm-mcp` in your Hermes configuration.

```yaml
mcp:
  servers:
    amm:
      command: /usr/local/bin/amm-mcp
      env:
        AMM_DB_PATH: /home/you/.amm/amm.db
```

Once that is in place, Hermes can call tools such as:

- `amm_recall`
- `amm_expand`
- `amm_remember`
- `amm_jobs_run`

Keep the mental model the same as every other runtime: Hermes asks for memory, but amm remains an external service boundary exposed through stdio MCP and the CLI.

## 2. Use Hook Handlers to Bridge Hermes Into amm

Hermes has its own hook registration model. This repo does **not** ship a Hermes-native plugin package or a full Hermes config tree. Instead, it ships amm-side helper scripts that a Hermes hook handler can call.

The helper pattern is intentionally small:

- pass the current user message to `examples/hermes-agent/on-user-message.sh`
- pass assistant responses to `examples/hermes-agent/on-assistant-message.sh`
- pass tool call/result payloads to `examples/hermes-agent/on-tool-use.sh`
- call `examples/hermes-agent/on-session-end.sh` when the session closes, or reuse the shared worker runner on a schedule

### Repo-shipped helper scripts

- [`examples/hermes-agent/on-user-message.sh`](../examples/hermes-agent/on-user-message.sh)
- [`examples/hermes-agent/on-assistant-message.sh`](../examples/hermes-agent/on-assistant-message.sh)
- [`examples/hermes-agent/on-tool-use.sh`](../examples/hermes-agent/on-tool-use.sh)
- [`examples/hermes-agent/on-session-end.sh`](../examples/hermes-agent/on-session-end.sh)

These helpers are **amm-side scripts**, not Hermes runtime code. Your Hermes hook handler is responsible for deciding when to call them and what environment variables or stdin payloads to pass.

## Suggested Environment Contract

To keep the helper scripts runtime-neutral, pass the following values from your Hermes hook handler when they are available:

- `AMM_SESSION_ID` — the current Hermes session/thread identifier
- `AMM_PROJECT_ID` — a stable project identifier for scoped recall
- stdin — message text for `on-user-message.sh` and `on-assistant-message.sh`
- stdin JSON for `on-tool-use.sh` with `tool_name`, `tool_input`, `tool_output`, `call_id`, and `status`

That keeps the amm scripts reusable even if your Hermes hook wiring changes over time.

## 3. Keep Background Workers External

amm does not ship an internal scheduler loop. Because SQLite only allows one writer at a time, we recommend running the **conservative baseline** maintenance jobs sequentially:

```bash
# Recommended: Serialized Baseline Runner
/path/to/agent-memory-manager/examples/scripts/run-workers.sh
```

For the baseline maintenance sequence, the runner in [`examples/scripts/run-workers.sh`](../examples/scripts/run-workers.sh) is the preferred starting point. It covers the full maintenance sequence. Structural repairs (`repair_links`) should be run separately as needed.

That means you can choose the trigger that fits Hermes best:

- Hermes cron or scheduled jobs (staggered or serialized baseline)
- a host-level cron or systemd timer (serialized baseline runner)
- a session-end hook that runs the repo-shipped warm-path sequence via `examples/hermes-agent/on-session-end.sh`

## Suggested Operational Pattern

Use a hot/warm/cold split:

- **Hot path**: Hermes hook handlers capture full transcript flow by sending user turns to `on-user-message.sh` (with ambient recall), assistant turns to `on-assistant-message.sh`, and tool activity to `on-tool-use.sh`
- **Warm path**: a session-end or periodic Hermes task runs the repo-shipped warm-path sequence serially via `examples/hermes-agent/on-session-end.sh`
- **Cold path**: scheduled jobs run the broader maintenance sequence through the shared runner or explicitly staggered entries

The repo-shipped session-end sequence runs `reflect`, `compress_history`, `consolidate_sessions`, `form_episodes`, `enrich_memories`, `rebuild_entity_graph`, and `lifecycle_review`.

That gives you immediate context injection without forcing the heavy jobs into the interactive loop.

## Instructions Snippet

If you want a Hermes-oriented instruction block, use something like this:

```md
## amm memory usage

- Treat amm as the durable memory substrate for this project.
- Use `amm_recall` or `amm recall --mode ambient` when resuming work, switching projects, or when important context may exist outside the immediate conversation.
- Use `amm_expand` only when a thin recall item needs to be opened in full.
- Use `amm_remember` for stable, high-confidence memories such as preferences, decisions, and durable constraints.
- Do not assume amm runs its own worker loop. Maintenance happens through external `amm jobs run <kind>` calls.
```

## Verification Checklist

- `amm-mcp` starts successfully with the configured `AMM_DB_PATH`
- Hermes can see and call the `amm` MCP server
- your Hermes hook handler can call `examples/hermes-agent/on-user-message.sh` with a sample prompt
- your Hermes hook handler can call `examples/hermes-agent/on-assistant-message.sh` with a sample response
- your Hermes hook handler can call `examples/hermes-agent/on-tool-use.sh` with sample JSON payload
- amm history shows captured `message_user`, `message_assistant`, `tool_call`, and `tool_result` events after helpers run
- `examples/hermes-agent/on-session-end.sh` can run without shell errors
- scheduled worker runs create summaries or memories as expected

## What This Repo Does Not Promise

- a built-in amm scheduler
- a Hermes-native plugin or SDK package in this repository
- a one-size-fits-all Hermes hook registration schema
- automatic execution of helper scripts or `maintenance.auto_*` flags without an external trigger
