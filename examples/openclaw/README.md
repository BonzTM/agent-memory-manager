# OpenClaw AMM Plugin

Native OpenClaw plugin for [amm](https://github.com/bonztm/agent-memory-manager) (Agent Memory Manager). Targets **OpenClaw 2026.03.31+**.

- **Automatic ambient recall injection** via `before_prompt_build` — relevant memories prepended to every LLM prompt
- **Event capture** for messages and tool invocations into amm history
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
