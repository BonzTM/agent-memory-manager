# OpenClaw Integration Guide

OpenClaw is the runtime; amm is the memory substrate.

> **Quick links**
>
> - [Example files](../examples/openclaw/) — plugin source, install script, config fragment
> - [Agent Onboarding](agent-onboarding.md) — database init, LLM config, worker scheduling
> - [HTTP API Reference](http-api-reference.md) — REST endpoints for API mode
> - [MCP Reference](mcp-reference.md) — tool definitions and schemas
> - [API-mode examples](../examples/api-mode/) — HTTP transport patterns

## Install

### npm (HTTP API mode)

```bash
openclaw plugins install @bonztm/amm
```

Requires `amm-http` running as an HTTP service. The npm package uses HTTP transport only — OpenClaw's security scanner blocks local binary (`child_process`) imports.

After install, configure the plugin **and** MCP server in `~/.openclaw/openclaw.json`:

```json
{
  "mcp": {
    "servers": {
      "amm": {
        "url": "http://localhost:8080/v1/mcp",
        "transport": "streamable-http",
        "headers": { "Authorization": "Bearer your-amm-api-key" }
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

- **`plugins.entries.amm`** — the plugin (ambient recall + event capture). `apiUrl` is **required** for npm installs.
- **`mcp.servers.amm`** — gives agents explicit tools (`amm_recall`, `amm_remember`, `amm_expand`). Uses MCP-over-HTTP for npm installs.

Restart OpenClaw after configuring.

### Local install (binary + HTTP mode)

For environments where the `amm` binary and SQLite database are on the same machine:

```bash
cd examples/openclaw && ./install.sh
```

The install script automatically configures the plugin, an MCP server (local `amm-mcp` binary or MCP-over-HTTP if `--api-url` is set), and the `plugins.allow` list. No manual config editing needed.

```bash
# HTTP mode (remote amm-http)
./install.sh --api-url http://your-host:8080 --api-key your-key

# With project scoping
./install.sh --project-id my-project
```

See [`examples/openclaw/README.md`](../examples/openclaw/README.md) for all install options.

## What the Plugin Does

1. **Ambient recall injection** — the `before_prompt_build` hook queries amm and returns a `prependContext` block with relevant memories before the LLM sees the prompt
2. **Two-tier memory guidance** — system prompt block teaches the agent to use built-in memory (MEMORY.md/USER.md) as a lean scratchpad and AMM (via MCP tools or CLI) as unlimited long-term storage, with `amm_expand` / `amm expand` with `max_depth` for deeper context
3. **Event capture** — plugin-registered hooks capture `message:preprocessed`, `message:sent`, `tool:called`, and `tool:completed` events into amm history
4. **Optional curated memory mirroring** — diffs MEMORY.md/USER.md on each `agent_end` and mirrors adds/removes/replacements to AMM durable memories with in-place PATCH for replacements
5. **MCP tools** — `amm-mcp` configured as an MCP server gives agents `amm_recall`, `amm_remember`, `amm_expand`, and 30+ other tools
6. **Dual transport** — local `amm` binary (default) or HTTP API via `AMM_API_URL`

The plugin is **hot-path only**. It does not run maintenance jobs.

## Responsibility Split

| Concern | OpenClaw owns | amm owns |
|---|---|---|
| Runtime lifecycle | plugin loading, hooks, scheduling | none |
| Memory storage | none | SQLite database, canonical memory/history records |
| Ambient recall injection | `before_prompt_build` hook execution | ambient recall query and result rendering |
| Explicit memory tools | MCP subprocess management, tool exposure | `amm-mcp` implementation |
| Event capture | hook firing and payload delivery | event ingestion |
| Maintenance | deciding when jobs run (external schedule) | executing `reflect`, `compress_history`, and other jobs |

## Plugin Architecture

```text
examples/openclaw/
  openclaw.plugin.json          # Native plugin manifest
  package.json                  # Published as @bonztm/amm on npm
  index.ts                      # Plugin entry point — hooks + system prompt
  install.sh                    # One-command local installer
  src/
    config.ts                   # Config resolution (plugin config + env)
    transport.ts                # Dual transport (binary CLI / HTTP API)
    transport-http.ts           # HTTP-only transport (npm package)
    recall.ts                   # Ambient recall query + rendering
    capture.ts                  # Event normalization + ingestion
    curated-sync.ts             # Curated memory snapshot + reconciliation
  openclaw.json                 # Example OpenClaw config fragment
```

### Hook Registration

The plugin registers hooks in its `register()` function:

- `before_prompt_build` — builds two-tier memory system prompt, runs ambient recall, and returns `{ prependContext: "<amm-context>...</amm-context>" }`
- `message:preprocessed` — captures enriched inbound messages
- `message:sent` — captures outbound assistant messages
- `tool:called` — captures tool invocations with name and arguments
- `tool:completed` — captures tool results
- `before_agent_start` — snapshots MEMORY.md/USER.md for curated memory sync (when enabled)
- `agent_end` — diffs curated files and mirrors changes to AMM (when enabled)

### Ambient Recall Flow

1. OpenClaw fires `before_prompt_build` before each LLM call
2. The plugin extracts the most recent user message from the conversation
3. The plugin queries `amm recall --mode ambient` (or `POST /v1/recall`)
5. The plugin renders the recall items into a text block
6. The plugin returns `{ prependContext: "<amm-context>\namm ambient recall:\n- [memory] ...\n</amm-context>" }`
7. OpenClaw prepends this to the prompt — the LLM sees relevant memories automatically

This mirrors the Hermes plugin's `pre_llm_call` → `{"context": "..."}` pattern.

## Transport Options

### Binary Mode (Default)

The plugin calls the local `amm` binary via `spawnSync`:

- Recall: `amm recall --mode ambient --json --session <id> --project <id> <query>`
- Ingest: `amm ingest event --in -` with JSON on stdin

Environment: `AMM_BIN`, `AMM_DB_PATH`

### HTTP API Mode

When `apiUrl` (or `AMM_API_URL`) is set, the plugin switches to the REST API:

- Recall: `POST /v1/recall`
- Ingest: `POST /v1/events`

Environment: `AMM_API_URL`, `AMM_API_KEY`

This works with remote `amm-http` servers, sidecar deployments, and Kubernetes pods. No local binary needed.

### MCP Server

For explicit agent tool access (`amm_recall`, `amm_remember`, `amm_expand`, etc.), configure `amm-mcp` as an MCP server in `openclaw.json`:

**Local (stdio):**
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
  }
}
```

**Remote (MCP-over-HTTP):**
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

The MCP server is independent of the plugin's hot-path transport. The plugin handles ambient recall and event capture; the MCP server gives agents explicit tools. `install.sh` configures both automatically.

## Recommended Ingestion Policies

The plugin captures `tool_call` and `tool_result` events. To prevent these from polluting extracted memories, add ignore policies:

```bash
amm policy-add --pattern-type kind --pattern "tool_call" --mode ignore --match-mode exact --priority 100
amm policy-add --pattern-type kind --pattern "tool_result" --mode ignore --match-mode exact --priority 100
```

Without these, the extraction pipeline treats raw tool JSON as meaningful content. The meaningful information is already captured in `message_user` and `message_assistant` events. See [Configuration: Ingestion Policies](configuration.md#ingestion-policies).

## Configuration

Plugin config (`openclaw.json`) takes precedence over environment variables:

| Plugin Config | Env Variable | Default | Description |
|---------------|-------------|---------|-------------|
| `ammBin` | `AMM_BIN` | `amm` | Path to local binary |
| `dbPath` | `AMM_DB_PATH` | `~/.amm/amm.db` | SQLite database path |
| `apiUrl` | `AMM_API_URL` | unset | HTTP API base URL |
| `apiKey` | `AMM_API_KEY` | unset | Bearer token for HTTP API |
| `projectId` | `AMM_PROJECT_ID` | unset | Stable project identifier |
| `recallLimit` | `AMM_OPENCLAW_RECALL_LIMIT` | `5` | Max recall items per turn |
| `syncCuratedMemory` | `AMM_OPENCLAW_SYNC_CURATED_MEMORY` | `false` | Enable curated memory mirroring |
| `curatedProjectId` | `AMM_OPENCLAW_CURATED_PROJECT_ID` | `projectId` | Override project ID for curated memory writes |
| `memoryScope` | `AMM_OPENCLAW_MEMORY_SCOPE` | `project` | AMM scope for MEMORY.md entries |
| `userScope` | `AMM_OPENCLAW_USER_SCOPE` | `global` | AMM scope for USER.md entries |
| `memoryType` | `AMM_OPENCLAW_MEMORY_TYPE` | `fact` | AMM memory type for MEMORY.md entries |
| `userType` | `AMM_OPENCLAW_USER_TYPE` | `preference` | AMM memory type for USER.md entries |
| `stateDir` | `AMM_OPENCLAW_STATE_DIR` | `~/.openclaw/state/amm-plugin` | Directory for sync state files |

## Installation

### Managed Plugin

```bash
cp -R examples/openclaw ~/.openclaw/plugins/amm
```

### Workspace Plugin

```bash
cp -R examples/openclaw .openclaw/plugins/amm
```

Then merge the contents of `openclaw.json` into your `~/.openclaw/openclaw.json`.

## Maintenance

The plugin does **not** run maintenance jobs. amm does not ship an internal scheduler.

Run the maintenance pipeline externally via host cron or systemd:

```bash
*/30 * * * * AMM_DB_PATH=/home/you/.amm/amm.db /path/to/examples/scripts/run-workers.sh
```

If using `amm-http` remotely, run maintenance on the server host.

See [`examples/scripts/run-workers.sh`](../examples/scripts/run-workers.sh) for the full baseline maintenance sequence.

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

## Verification Checklist

- `openclaw plugins list` shows `amm` as loaded
- a sample conversation produces `message_user` and `message_assistant` events in `amm history --limit 5`
- ambient recall items appear in the LLM prompt when relevant memories exist
- if `AMM_API_URL` is set, the plugin works without the local `amm` binary on PATH
- `amm-mcp` is callable via the MCP sidecar (if wired)
- scheduled worker runs via `run-workers.sh` complete without errors

## What This Repo Does Not Promise

- a built-in amm scheduler or daemon
- a one-size-fits-all OpenClaw configuration
- automatic maintenance without an external trigger
