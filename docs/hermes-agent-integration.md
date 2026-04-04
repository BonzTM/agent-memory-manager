# Hermes-Agent Integration Guide

Hermes and AMM now fit together in two clean shapes: the newer external memory-provider architecture, and the older MCP + hook-plugin pattern kept for backwards compatibility. Optional helper scripts still cover shell-driven capture and warm-path maintenance. For HTTP API mode, see [API-mode examples](../examples/api-mode/) and [HTTP API Reference](http-api-reference.md).

- **Hermes-Agent** owns the runtime, plugin loading, hooks, scheduling, and agent behavior.
- **AMM** owns durable history, recall, summaries, and maintenance jobs.

That keeps the integration contract simple:

- register `amm-mcp` if you want explicit AMM tools inside Hermes
- on newer Hermes builds, install the repo-shipped AMM memory-provider example and set `memory.provider: amm`
- on older Hermes builds, use the repo-shipped hook plugin example for per-turn ambient recall injection and user/assistant capture
- use the optional helper scripts only when you want shell-hook wiring, raw tool event capture, or a separate session-end runner
- keep maintenance jobs external as `amm jobs run <kind>` calls against the same database

The plugin's hot-path transport is independent from the explicit MCP tool transport:

- **Binary mode**: plugin calls the local `amm` binary, and Hermes can talk to `amm-mcp` over stdio
- **API mode**: plugin calls the AMM REST API via `AMM_API_URL`, and Hermes can talk to `/v1/mcp` over MCP-over-HTTP

## Recommended Shape

Use Hermes and AMM in four layers:

1. **MCP** for explicit agent-controlled memory access (via stdio or HTTP)
2. **Memory provider** on newer Hermes builds for transparent ambient recall, turn sync, and built-in memory mirroring
3. **Legacy hook plugin** only when you still need the older plugin API
4. **Optional helper scripts / scheduled jobs** for shell-based bridging, explicit raw tool capture, and heavier maintenance

## 1. Register `amm-mcp`

Hermes supports MCP, so the lowest-friction explicit integration is to register the AMM MCP server.

### Option A: MCP-over-stdio (Local)

Register `amm-mcp` directly in your Hermes configuration.

```yaml
mcp_servers:
  amm:
    command: /usr/local/bin/amm-mcp
    env:
      AMM_DB_PATH: /home/you/.amm/amm.db
```

### Option B: MCP-over-HTTP (Remote/Sidecar)

When running AMM as a sidecar or remote server (via `amm-http`), Hermes can connect via the MCP Streamable HTTP protocol.

```yaml
mcp_servers:
  amm:
    url: http://localhost:8080/v1/mcp
```

This is the recommended method for Kubernetes deployments using the sidecar pattern (see `deploy/sidecar/`). If `AMM_API_KEY` is set on the server, ensure the client includes the appropriate authentication headers.

Once that is in place, Hermes can call tools such as:

- `amm_recall`
- `amm_expand`
- `amm_remember`
- `amm_jobs_run`

Keep the mental model the same as every other runtime: Hermes asks for memory, but AMM remains an external service boundary exposed through MCP and the CLI.

## 1.5. Configure Recommended Ingestion Policies

The optional Hermes tool-capture helper records `tool_call` and `tool_result` events via `on-tool-use.sh`. To prevent these from polluting extracted memories, **strongly consider** adding ignore policies after initialization:

```bash
amm policy-add --pattern-type kind --pattern "tool_call" --mode ignore --match-mode exact --priority 100
amm policy-add --pattern-type kind --pattern "tool_result" --mode ignore --match-mode exact --priority 100
```

Without these policies, the extraction pipeline treats raw tool invocation JSON (patch text, shell commands, API payloads) as meaningful content, producing low-quality memories. The meaningful information is already captured in `message_user` and `message_assistant` events. See [Configuration: Ingestion Policies](configuration.md#ingestion-policies) for the full reference.

## 2. Install The Recommended AMM Memory Provider

Newer Hermes builds expose an external memory-provider architecture. That is the cleaner integration point for AMM because it gives you first-class `prefetch()`, `sync_turn()`, and `on_memory_write()` hooks through Hermes' `MemoryProvider` interface.

This repo ships an AMM provider example at:

- [`examples/hermes-agent/memory/amm/plugin.yaml`](../examples/hermes-agent/memory/amm/plugin.yaml)
- [`examples/hermes-agent/memory/amm/__init__.py`](../examples/hermes-agent/memory/amm/__init__.py)
- [`examples/hermes-agent/memory/amm/README.md`](../examples/hermes-agent/memory/amm/README.md)

Set Hermes to use it with:

```yaml
memory:
  provider: amm
```

What the provider does:

- injects AMM ambient recall with `prefetch()`
- syncs completed turns into AMM with `sync_turn()`
- mirrors Hermes built-in curated memory into AMM via `on_memory_write()` reconciliation
- works against either the local `amm` binary or `AMM_API_URL`

Important:

- Hermes' current `on_memory_write(action, target, content)` bridge does not expose `old_text`, and `remove` is not fired through that bridge today.
- The repo-shipped AMM provider works around that by reconciling Hermes' built-in `MEMORY.md` / `USER.md` files against a local snapshot, then applying add/remove deltas to AMM durable memories.
- Because of that Hermes limitation, remove propagation is caught on session-end reconciliation instead of immediate callback parity.

## 2.5. Legacy Hook Plugin (Fallback)

Older Hermes plugin installs still support the hook-based directory plugin under `~/.hermes/plugins/<name>/`, and `pre_llm_call` hooks can return a `context` block that Hermes appends to the effective system prompt for the current turn. See the official Hermes [Plugins guide](https://hermes-agent.nousresearch.com/docs/user-guide/features/plugins/), [Hooks guide](https://hermes-agent.nousresearch.com/docs/user-guide/features/hooks/#plugin-hooks), and [`run_agent.py` implementation](https://github.com/NousResearch/hermes-agent/blob/v2026.3.30/run_agent.py).

This repo still ships the legacy Hermes directory plugin example at:

- [`examples/hermes-agent/amm-memory/plugin.yaml`](../examples/hermes-agent/amm-memory/plugin.yaml)
- [`examples/hermes-agent/amm-memory/__init__.py`](../examples/hermes-agent/amm-memory/__init__.py)

Install it globally:

```bash
mkdir -p ~/.hermes/plugins
cp -R examples/hermes-agent/amm-memory ~/.hermes/plugins/amm-memory
```

Or install it project-locally:

```bash
mkdir -p ./.hermes/plugins
cp -R examples/hermes-agent/amm-memory ./.hermes/plugins/amm-memory
HERMES_ENABLE_PROJECT_PLUGINS=true hermes
```

### What the legacy plugin does

- ingests the current user turn as `message_user` in `pre_llm_call`
- calls AMM ambient recall for the current turn using either the local CLI or `POST /v1/recall`
- renders a thin `amm ambient recall:` block and returns it as hook `context`
- ingests the final assistant response as `message_assistant` in `post_llm_call`
- optionally mirrors successful Hermes `memory` tool writes into AMM durable memories from `post_tool_call`

The plugin resolves general `project_id` like this:

- use `AMM_PROJECT_ID` when set
- otherwise, derive from `TERMINAL_CWD` when Hermes exposes it
- otherwise, on CLI sessions only, fall back to the current working directory basename

Curated-memory parity resolves its project override like this:

- use `AMM_HERMES_CURATED_PROJECT_ID` when set
- otherwise, fall back to the general plugin `project_id` resolution above

Recommended environment:

- `AMM_BIN` for local-binary mode
- `AMM_DB_PATH` for local-binary mode
- `AMM_API_URL` to switch the plugin into REST mode against `amm-http`
- `AMM_API_KEY` when the HTTP server requires bearer auth
- `AMM_HERMES_CURATED_PROJECT_ID` for curated-memory parity only when you want mirrored Hermes memories pinned to a specific AMM project without changing the plugin's general project resolution
- `AMM_HERMES_RECALL_LIMIT` to override the default recall block length (`5`)

Optional curated-memory parity settings:

- `AMM_HERMES_SYNC_CURATED_MEMORY=true` enables mirroring successful Hermes `memory` tool writes into AMM durable memories
- `AMM_HERMES_MEMORY_SCOPE` sets the AMM scope for Hermes `target="memory"` entries (`project` by default, falls back to `global` when no project can be resolved)
- `AMM_HERMES_USER_SCOPE` sets the AMM scope for Hermes `target="user"` entries (`global` by default)
- `AMM_HERMES_MEMORY_TYPE` sets the AMM memory type for Hermes `target="memory"` entries (`fact` by default)
- `AMM_HERMES_USER_TYPE` sets the AMM memory type for Hermes `target="user"` entries (`preference` by default)
- `AMM_HERMES_STATE_DIR` overrides the plugin state directory (defaults to `~/.hermes/state/amm-memory`)

Important:

- Do **not** also wire `on-user-message.sh` or `on-assistant-message.sh` for the same Hermes hot path when this plugin is enabled. That will duplicate `message_user` / `message_assistant` events.
- The repo-shipped plugin intentionally focuses on the hot path. It does **not** run maintenance jobs automatically.
- The current Hermes tool-hook API exposes `task_id`, not `session_id`, so this repo keeps raw tool-event capture as an optional helper-script path instead of pretending the plugin has better correlation than Hermes currently provides.
- When `AMM_API_URL` is set, the plugin uses the AMM REST API endpoints `POST /v1/events`, `POST /v1/recall`, and, when curated-memory parity is enabled, the durable memory endpoints under `/v1/memories`.
- Curated-memory parity mirrors **future successful Hermes `memory` tool writes**. It keeps a local AMM-ID map and failure queue under the plugin state directory so updates and deletes can target the correct AMM memory IDs.
- If you enable curated-memory parity on an instance that already has existing Hermes curated memory, plan a one-time backfill or accept that only new successful writes will be mirrored automatically.

## 2.6. Optional Helper Scripts

The repo still ships shell helpers for users who prefer hook-handler wiring or need behaviors outside the plugin's hot path:

- [`examples/hermes-agent/on-user-message.sh`](../examples/hermes-agent/on-user-message.sh)
- [`examples/hermes-agent/on-assistant-message.sh`](../examples/hermes-agent/on-assistant-message.sh)
- [`examples/hermes-agent/on-tool-use.sh`](../examples/hermes-agent/on-tool-use.sh)
- [`examples/hermes-agent/on-session-end.sh`](../examples/hermes-agent/on-session-end.sh)

Use them when you want one of these:

- shell-hook integration without installing a Hermes plugin
- explicit raw `tool_call` / `tool_result` ingestion via `on-tool-use.sh`
- a separate warm-path runner via `on-session-end.sh`

To keep the helper scripts runtime-neutral, pass the following values from your Hermes hook handler when they are available:

- `AMM_SESSION_ID` — the current Hermes session/thread identifier
- `AMM_PROJECT_ID` — a stable project identifier for scoped recall
- stdin — message text for `on-user-message.sh` and `on-assistant-message.sh`
- stdin JSON for `on-tool-use.sh` with `tool_name`, `tool_input`, `tool_output`, `call_id`, and `status`

That keeps the AMM scripts reusable even if your Hermes hook wiring changes over time.

## 3. Keep Background Workers External

AMM does not ship an internal scheduler loop. Because SQLite only allows one writer at a time, we recommend running the **conservative baseline** maintenance jobs sequentially:

```bash
# Recommended: Serialized Baseline Runner
/path/to/agent-memory-manager/examples/scripts/run-workers.sh
```

For the baseline maintenance sequence, the runner in [`examples/scripts/run-workers.sh`](../examples/scripts/run-workers.sh) is the preferred starting point. It covers the full maintenance sequence. Structural repairs (`repair_links`) should be run separately as needed.

That means you can choose the trigger that fits Hermes best:

- Hermes cron or scheduled jobs (staggered or serialized baseline)
- a host-level cron or systemd timer (serialized baseline runner)
- a real session-end hook or wrapper that runs the repo-shipped warm-path sequence via `examples/hermes-agent/on-session-end.sh`

## Suggested Operational Pattern

Use a hot/warm/cold split:

- **Hot path**: the repo-shipped Hermes plugin injects AMM ambient recall every turn and captures `message_user` / `message_assistant` via either the local binary or the HTTP API, depending on whether `AMM_API_URL` is set
- **Warm path**: a real session-end hook, wrapper script, or periodic Hermes task runs the repo-shipped warm-path sequence serially via `examples/hermes-agent/on-session-end.sh`
- **Cold path**: scheduled jobs run the broader maintenance sequence through the shared runner or explicitly staggered entries

The repo-shipped session-end sequence runs `reflect`, `compress_history`, `consolidate_sessions`, `form_episodes`, `enrich_memories`, `rebuild_entity_graph`, and `lifecycle_review`.

That gives you immediate context injection without forcing the heavy jobs into the interactive loop.

## Instructions Snippet

If you want a Hermes-oriented instruction block, use something like this:

```md
## amm memory usage

- Treat AMM as the durable memory substrate for this project.
- Use `amm_recall` or `amm recall --mode ambient` when resuming work, switching projects, or when important context may exist outside the immediate conversation.
- Use `amm_expand` only when a thin recall item needs to be opened in full.
- Use `amm_remember` for stable, high-confidence memories such as preferences, decisions, and durable constraints.
- Do not assume AMM runs its own worker loop. Maintenance happens through external `amm jobs run <kind>` calls.
```

## Verification Checklist

- `amm-mcp` starts successfully with the configured `AMM_DB_PATH`
- Hermes can see and call the `amm` MCP server
- `python3 -m py_compile examples/hermes-agent/memory/amm/__init__.py` succeeds on newer-provider installs
- `python3 -m py_compile examples/hermes-agent/amm-memory/__init__.py` succeeds on legacy-hook installs
- Hermes is configured with `memory.provider: amm` when using the new provider shape
- a sample Hermes turn produces `message_user` and `message_assistant` events in AMM history
- if `AMM_HERMES_SYNC_CURATED_MEMORY=true`, a successful built-in Hermes memory write creates mirrored AMM durable memory on the configured path
- if `AMM_API_URL` is set, the same sample Hermes turn succeeds against the AMM HTTP server without the local `amm` binary on PATH
- if you use the optional tool helper, `examples/hermes-agent/on-tool-use.sh` accepts a sample JSON payload
- `examples/hermes-agent/on-session-end.sh` can run without shell errors
- scheduled worker runs create summaries or memories as expected

## What This Repo Does Not Promise

- a built-in AMM scheduler
- a one-size-fits-all Hermes config tree
- automatic maintenance execution without an external trigger
