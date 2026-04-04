# OpenClaw AMM Plugin

Native OpenClaw plugin for [amm](https://github.com/bonztm/agent-memory-manager) (Agent Memory Manager). Targets **OpenClaw 2026.03.31+**.

- **Automatic ambient recall injection** via `before_prompt_build` — relevant memories prepended to every LLM prompt
- **Two-tier memory guidance** — system prompt teaches the agent to use built-in memory (MEMORY.md/USER.md) as a lean scratchpad and AMM as unlimited long-term storage
- **Event capture** for messages and tool invocations into amm history
- **Optional curated memory mirroring** — diffs MEMORY.md/USER.md on each turn and mirrors adds/removes/replacements to AMM durable memories
- **Dual transport** — local `amm` binary or remote HTTP API via `AMM_API_URL`
- **MCP sidecar** wiring for explicit agent tool access (`amm_recall`, `amm_remember`, `amm_expand`)

The plugin is **hot-path only**. It does not run maintenance jobs. Keep maintenance on an external schedule.

## Install

### Option A: npm install (HTTP API mode)

```bash
openclaw plugins install @bonztm/amm
```

**Requires `amm-http` running as an HTTP service** — the npm package does not include local binary support due to OpenClaw's security scanner restrictions on `child_process`.

After install, configure the plugin and MCP server in `~/.openclaw/openclaw.json`:

```json
{
  "mcp": {
    "servers": {
      "amm": {
        "url": "http://localhost:8080/v1/mcp",
        "transport": "streamable-http",
        "headers": {
          "Authorization": "Bearer your-amm-api-key"
        }
      }
    }
  },
  "plugins": {
    "allow": ["amm"],
    "entries": {
      "amm": {
        "enabled": true,
        "config": {
          "apiUrl": "http://localhost:8080",
          "apiKey": "your-amm-api-key",
          "projectId": "my-project",
          "recallLimit": 5
        }
      }
    }
  }
}
```

- `plugins.entries.amm.config.apiUrl` is **required** for npm installs — point it at your `amm-http` instance
- `mcp.servers.amm` gives the agent explicit tools (`amm_recall`, `amm_remember`, `amm_expand`, etc.)
- Restart OpenClaw after configuring

### Option B: Local install (binary + HTTP mode)

For environments where the `amm` binary and SQLite database are on the same machine as OpenClaw:

```bash
# Local binary mode (no HTTP server needed)
./install.sh

# With project scoping
./install.sh --project-id my-project --recall-limit 10

# HTTP API mode (remote amm-http server)
./install.sh --api-url http://localhost:8080 --api-key your-key
```

The install script automatically configures:
- The AMM plugin (ambient recall + event capture)
- An MCP server (explicit agent tools) — local `amm-mcp` binary for local installs, MCP-over-HTTP for `--api-url` installs
- The `plugins.allow` list

Run `./install.sh --help` for all options.

### Option C: Manual

Copy the plugin directory and update your `openclaw.json`:

```bash
cp -R examples/openclaw ~/.openclaw/extensions/amm
```

For local binary mode:

```json
{
  "mcp": {
    "servers": {
      "amm": {
        "command": "/usr/local/bin/amm-mcp",
        "args": [],
        "env": { "AMM_DB_PATH": "/home/you/.amm/amm.db" }
      }
    }
  },
  "plugins": {
    "allow": ["amm"],
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

For HTTP API mode:

```json
{
  "mcp": {
    "servers": {
      "amm": {
        "url": "http://your-amm-host:8080/v1/mcp",
        "transport": "streamable-http"
      }
    }
  },
  "plugins": {
    "allow": ["amm"],
    "entries": {
      "amm": {
        "enabled": true,
        "config": {
          "apiUrl": "http://your-amm-host:8080",
          "apiKey": "your-key",
          "projectId": "my-project"
        }
      }
    }
  }
}
```

## Files

- `openclaw.plugin.json` — native plugin manifest
- `package.json` — publishable as `@bonztm/amm`
- `index.ts` — plugin entry point with tool registration and hooks
- `install.sh` — one-command local installer
- `src/config.ts` — configuration resolution (plugin config + env vars)
- `src/transport.ts` — dual transport layer (binary CLI / HTTP API)
- `src/transport-http.ts` — HTTP-only transport (used by npm package)
- `src/recall.ts` — ambient recall query and rendering
- `src/capture.ts` — event normalization and ingestion
- `src/curated-sync.ts` — curated memory snapshot and reconciliation
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
| `syncCuratedMemory` | `AMM_OPENCLAW_SYNC_CURATED_MEMORY` | `false` | Enable curated memory mirroring |
| `curatedProjectId` | `AMM_OPENCLAW_CURATED_PROJECT_ID` | `projectId` | Override project ID for curated memory writes |
| `memoryScope` | `AMM_OPENCLAW_MEMORY_SCOPE` | `project` | AMM scope for MEMORY.md entries |
| `userScope` | `AMM_OPENCLAW_USER_SCOPE` | `global` | AMM scope for USER.md entries |
| `memoryType` | `AMM_OPENCLAW_MEMORY_TYPE` | `fact` | AMM memory type for MEMORY.md entries |
| `userType` | `AMM_OPENCLAW_USER_TYPE` | `preference` | AMM memory type for USER.md entries |
| `stateDir` | `AMM_OPENCLAW_STATE_DIR` | `~/.openclaw/state/amm-plugin` | Directory for sync state files |

### Transport Modes

**Binary mode** (default): The plugin calls the local `amm` binary via subprocess. Requires `amm` on PATH or configured via `ammBin`.

**HTTP API mode**: When `apiUrl` / `AMM_API_URL` is set, the plugin calls the AMM REST API (`POST /v1/events`, `POST /v1/recall`). Works with remote `amm-http` servers and supports bearer auth via `apiKey` / `AMM_API_KEY`.

**MCP server**: For explicit agent tool access (`amm_recall`, `amm_remember`, `amm_expand`), configure `amm-mcp` as an MCP server in `openclaw.json`. `install.sh` does this automatically. For manual setup:

```json
{
  "mcp": {
    "servers": {
      "amm": {
        "url": "http://localhost:8080/v1/mcp",
        "transport": "streamable-http"
      }
    }
  }
}
```

Or for local binary mode: `"command": "/usr/local/bin/amm-mcp"` with `"env": { "AMM_DB_PATH": "..." }`.

## How It Works

### Ambient Recall Injection

On every turn, the `before_prompt_build` hook:

1. Extracts the most recent user message from the conversation
2. Queries amm ambient recall with the user's message
3. Builds a system prompt block with two-tier memory guidance
4. Returns a `prependContext` block that OpenClaw injects before the LLM sees the prompt

The injected block includes both the two-tier memory guidance (teaching the agent about built-in memory vs AMM, MCP and CLI tool references, `amm_expand` with `max_depth`, and curated project ID scoping) and the ambient recall results.

This is transparent to the agent — it sees relevant memories and understands how to use AMM for deeper storage without any manual configuration.

### Curated Memory Mirroring

When `syncCuratedMemory` is enabled, the plugin mirrors OpenClaw's built-in MEMORY.md/USER.md to AMM:

1. On `before_agent_start`, snapshots MEMORY.md and USER.md
2. On `agent_end`, diffs current file contents against the snapshot
3. **Adds** create new AMM memories (fingerprint-deduped)
4. **Replaces** PATCH the existing AMM memory in place (preserving ID, access history, and entity links)
5. **Removes** delete the corresponding AMM memory

Replace detection works by finding the sync state record whose content no longer appears in the curated file, then PATCHing it with the new content. Falls back to creating a new memory if the old record can't be matched.

Sync state (AMM memory ID map and retry queue) is persisted under `stateDir` so it survives restarts. Failed operations are queued for manual inspection.

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

The plugin automatically injects two-tier memory guidance via the system prompt. If you want to add manual instructions, use something like:

```md
## amm memory usage

- You have two memory tiers: built-in memory (MEMORY.md/USER.md) for high-frequency context, and AMM for unlimited long-term storage.
- Ambient recall is injected automatically each turn — you do not need to call recall explicitly for basic context.
- Use amm_recall (MCP) or `amm recall` (CLI) when you need deeper or mode-specific memory queries beyond ambient.
- When a recalled memory is too thin, use amm_expand (MCP) or `amm expand` (CLI) with max_depth 1-2 for full context.
- Use amm_remember (MCP) or `amm remember` (CLI) for stable, high-confidence memories — especially when built-in memory is full.
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
