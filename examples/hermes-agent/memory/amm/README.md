# AMM Hermes Memory Provider Example

This example targets Hermes' newer external memory-provider architecture instead of the older hook-only directory plugin shape.

Install this provider under Hermes as `plugins/memory/amm/` and select it with `memory.provider: amm` in `config.yaml`.

## What it does

- injects AMM ambient recall on each turn through the `prefetch()` memory-provider hook
- ingests completed user/assistant turns into AMM through `sync_turn()`
- mirrors Hermes built-in curated memory files into AMM durable memories
- uses either the local `amm` binary or the AMM HTTP API via `AMM_API_URL`

## Important limits

Hermes' current `on_memory_write(action, target, content)` bridge does **not** expose `old_text` for replace or any callback for remove. This provider works around that by reconciling the current built-in `MEMORY.md` / `USER.md` file contents against an in-memory snapshot:

- `add` and `replace` are reconciled immediately after the bridge fires
- `remove` is reconciled on `on_session_end()`

That means curated-memory parity is much cleaner than the old `post_tool_call` scraping path, but remove propagation still depends on Hermes firing a real session-end lifecycle.

## Install

Until Hermes exposes external memory-provider discovery from user plugin directories cleanly, the conservative install shape is to vendor this directory into Hermes' `plugins/memory/amm/` tree or package it with your Hermes build.

Repo path:

- `examples/hermes-agent/memory/amm/plugin.yaml`
- `examples/hermes-agent/memory/amm/__init__.py`

## Config

Set `memory.provider: amm` in Hermes config, then use env vars as needed:

- `AMM_API_URL` for HTTP mode
- `AMM_API_KEY` for HTTP auth
- `AMM_BIN` / `AMM_DB_PATH` for local-binary mode
- `AMM_PROJECT_ID` for general AMM project scoping
- `AMM_HERMES_CURATED_PROJECT_ID` for curated-memory parity only
- `AMM_HERMES_SYNC_CURATED_MEMORY=true` to enable built-in memory mirroring
- `AMM_HERMES_RECALL_LIMIT` to change ambient recall length
- `AMM_HERMES_MEMORY_SCOPE`, `AMM_HERMES_USER_SCOPE`, `AMM_HERMES_MEMORY_TYPE`, `AMM_HERMES_USER_TYPE` for mirrored-memory policy

## Legacy plugin

For older Hermes builds that do not support memory providers yet, keep using the legacy hook plugin under `examples/hermes-agent/amm-memory/`.
