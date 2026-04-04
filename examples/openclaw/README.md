# OpenClaw AMM Plugin

Native OpenClaw plugin for [amm](https://github.com/bonztm/agent-memory-manager) (Agent Memory Manager). Targets **OpenClaw 2026.03.31+**.

- **Automatic ambient recall injection** via `before_prompt_build`
- **Two-tier memory guidance** — system prompt teaches the agent to use built-in memory as a lean scratchpad and AMM as unlimited long-term storage
- **Event capture** for messages and tool invocations
- **Optional curated memory mirroring** of MEMORY.md/USER.md to AMM
- **Dual transport** — local `amm` binary or remote HTTP API via `AMM_API_URL`
- **MCP sidecar** wiring for explicit agent tool access

The plugin is **hot-path only**. It does not run maintenance jobs.

## Prerequisites

- `amm` and optionally `amm-mcp` installed in your PATH for local-binary mode, or `amm-http` reachable over the network for API mode
- An initialized database at `~/.amm/amm.db` or your chosen `AMM_DB_PATH`

## Install

### Option A: npm install (HTTP API mode)

```bash
openclaw plugins install @bonztm/amm
```

Requires `amm-http` running as an HTTP service. After install, configure the plugin and MCP server in `~/.openclaw/openclaw.json`. See the [integration guide](../../docs/openclaw-integration.md#install) for the full config example.

### Option B: Local install (binary + HTTP mode)

```bash
# Local binary mode (no HTTP server needed)
./install.sh

# With project scoping
./install.sh --project-id my-project --recall-limit 10

# HTTP API mode (remote amm-http server)
./install.sh --api-url http://localhost:8080 --api-key your-key
```

The install script automatically configures the plugin, MCP server, and `plugins.allow` list. Run `./install.sh --help` for all options.

### Option C: Manual

```bash
cp -R examples/openclaw ~/.openclaw/extensions/amm
```

Then merge the config into your `~/.openclaw/openclaw.json`. See the [integration guide](../../docs/openclaw-integration.md#install) for local and HTTP config examples.

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

## Verify

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

For the complete integration guide — configuration reference, transport options, architecture details, curated memory mirroring, maintenance, and operational patterns — see [`../../docs/openclaw-integration.md`](../../docs/openclaw-integration.md).
