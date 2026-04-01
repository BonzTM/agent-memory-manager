# OpenClaw AMM Plugin

Native OpenClaw plugin for [amm](https://github.com/bonztm/agent-memory-manager) (Agent Memory Manager). Targets **OpenClaw 2026.03.31+**.

**Claims the memory slot** — replaces OpenClaw's built-in `memory-core` with AMM's full extraction and recall pipeline.

- **`memory_search` / `memory_get` tools** registered via the memory slot
- **Automatic ambient recall injection** via `before_prompt_build` — relevant memories prepended to every LLM prompt
- **Event capture** for messages and tool invocations into amm history
- **Dual transport** — local `amm` binary or remote HTTP API via `AMM_API_URL`

The plugin is **hot-path only**. It does not run maintenance jobs. Keep maintenance on an external schedule.

## Install

### Option A: OpenClaw Plugin Manager (Recommended)

```bash
openclaw plugins install @bonztm/amm
```

This installs the plugin, enables it, and claims the memory slot automatically.

### Option B: Install Script

For local/release builds:

```bash
# Basic install (local binary mode)
./install.sh

# With options
./install.sh --project-id my-project --recall-limit 10

# HTTP API mode (remote amm-http server)
./install.sh --api-url http://localhost:8080 --api-key your-key

# With MCP sidecar for the full tool suite
./install.sh --mcp --amm-bin /usr/local/bin/amm-mcp
```

Run `./install.sh --help` for all options.

### Option C: Manual

Copy the plugin directory and update your `openclaw.json`:

```bash
cp -R examples/openclaw ~/.openclaw/extensions/amm
```

```json
{
  "plugins": {
    "slots": {
      "memory": "amm"
    },
    "entries": {
      "amm": {
        "enabled": true,
        "config": {
          "projectId": "my-project"
        }
      }
    }
  }
}
```

## Files

- `openclaw.plugin.json` — native plugin manifest (`kind: "memory"`)
- `package.json` — publishable as `@bonztm/amm`
- `index.ts` — plugin entry point with tool registration and hooks
- `install.sh` — one-command local installer
- `src/config.ts` — configuration resolution (plugin config + env vars)
- `src/transport.ts` — dual transport layer (binary CLI / HTTP API)
- `src/recall.ts` — ambient recall query and rendering
- `src/capture.ts` — event normalization and ingestion
- `openclaw.json` — example OpenClaw config fragment

## Configuration

The plugin reads configuration from OpenClaw plugin config (via `configSchema`) with environment variable fallbacks:

| Plugin Config | Environment Variable | Default | Description |
|---------------|----------------------|---------|-------------|
| `ammBin` | `AMM_BIN` | `amm` | Path to local `amm` binary |
| `dbPath` | `AMM_DB_PATH` | `~/.amm/amm.db` | SQLite database path (binary mode) |
| `apiUrl` | `AMM_API_URL` | unset | AMM HTTP API base URL (switches to HTTP transport) |
| `apiKey` | `AMM_API_KEY` | unset | Bearer token for HTTP API auth |
| `projectId` | `AMM_PROJECT_ID` | unset | Stable project identifier for scoped recall |
| `recallLimit` | `AMM_OPENCLAW_RECALL_LIMIT` | `5` | Max recall items per turn |

### Transport Modes

**Binary mode** (default): The plugin calls the local `amm` binary via subprocess. Requires `amm` on PATH or configured via `ammBin`.

**HTTP API mode**: When `apiUrl` / `AMM_API_URL` is set, the plugin calls the AMM REST API (`POST /v1/events`, `POST /v1/recall`). Works with remote `amm-http` servers and supports bearer auth via `apiKey` / `AMM_API_KEY`.

**MCP sidecar**: For explicit agent tool access, wire `amm-mcp` separately in the `acpx` plugin config (shown in `openclaw.json`). This is independent of the plugin's hot-path transport. For remote/sidecar deployments, use MCP-over-HTTP:

```json
{
  "plugins": {
    "entries": {
      "acpx": {
        "enabled": true,
        "config": {
          "mcpServers": {
            "amm": {
              "url": "http://localhost:8080/v1/mcp"
            }
          }
        }
      }
    }
  }
}
```

## How It Works

### Ambient Recall Injection

On every turn, the `before_prompt_build` hook:

1. Extracts the most recent user message from the conversation
2. Queries amm ambient recall with the user's message
4. Returns a `prependContext` block that OpenClaw injects before the LLM sees the prompt

The injected block looks like:

```xml
<amm-context>
amm ambient recall:
- [memory] User prefers factory pattern for widget creation (score: 0.94)
- [summary] Previous project used event-driven architecture (score: 0.87)
</amm-context>
```

This is transparent to the agent — it sees relevant memories without needing to call any tool.

### Event Capture

Plugin-registered hooks automatically capture:

- `message:preprocessed` — inbound user messages (enriched body)
- `message:sent` — outbound assistant messages
- `tool:called` — tool/function invocations with name and arguments
- `tool:completed` — tool/function results

All events are normalized to the amm event schema with `source_system: "openclaw"`.

## Maintenance

This plugin does **not** run maintenance jobs. amm does not ship an internal scheduler.

Run the maintenance pipeline externally:

```bash
# Host cron — recommended
*/30 * * * * AMM_DB_PATH=/home/you/.amm/amm.db /path/to/examples/scripts/run-workers.sh
```

Or if using `amm-http` remotely, run maintenance on the server host.

See [`examples/scripts/run-workers.sh`](../scripts/run-workers.sh) for the full baseline maintenance sequence.

## MCP Tools

When `amm-mcp` is wired as an MCP sidecar, agents get explicit access to:

- `amm_recall` — query memories with full mode control
- `amm_expand` — expand a thin recall item to its full content
- `amm_remember` — create durable memories explicitly
- `amm_jobs_run` — trigger individual maintenance jobs

These complement the automatic ambient recall — use them when the agent needs deeper memory access than the per-turn injection provides.

## Agent Instructions Snippet

```md
## amm memory usage

- Treat amm as the durable memory substrate for this project.
- Ambient recall is injected automatically each turn — you do not need to call recall explicitly for basic context.
- Use `amm_recall` when you need deeper or mode-specific memory queries beyond ambient.
- Use `amm_expand` only when a thin recall item needs to be opened in full.
- Use `amm_remember` for stable, high-confidence memories such as preferences, decisions, and constraints.
- Do not assume amm runs its own maintenance. Worker jobs run through external scheduling.
```

## Verification

After installation:

```bash
# Verify the plugin is loaded
openclaw plugins list

# Verify MCP sidecar
openclaw mcp list
```

Then start a conversation and check that events appear in amm history:

```bash
amm history --limit 5
```
