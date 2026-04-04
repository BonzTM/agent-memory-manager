# OpenCode Example

This directory ships the truthful OpenCode amm pattern for this repo.

The supported shape is:

- **MCP first** for explicit amm access through `/usr/local/bin/amm-mcp`
- **A small local plugin** for environment injection, full conversation event capture, and session lifecycle markers
- **External amm workers** for heavier maintenance jobs

## Files

- `opencode.json` — documented MCP config for `amm-mcp`
- `package.json` — marks the plugin directory as ESM so local `plugins/*.js` files can use `export`
- `plugins/amm.js` — local OpenCode plugin that:
  - injects `AMM_BIN`, `AMM_DB_PATH`, `AMM_PROJECT_ID`, and `AMM_SESSION_ID`
  - records `tool.execute.before` as amm `tool_call`
  - records `tool.execute.after` as amm `tool_result`
  - records user and assistant messages as amm `message_user` / `message_assistant`
  - deduplicates message events so the same message content is not ingested twice
  - records `session.created` / `session.idle`
  - runs `reflect` and `compress_history` on `session.idle` in a non-blocking background process with timeout guards and a lock file to prevent overlapping maintenance runs

## Global install

```bash
mkdir -p ~/.config/opencode/plugins

cp examples/opencode/opencode.json ~/.config/opencode/opencode.json
cp examples/opencode/package.json ~/.config/opencode/package.json
cp examples/opencode/plugins/amm.js ~/.config/opencode/plugins/amm.js
```

If you already have an existing `~/.config/opencode/opencode.json`, merge only the `mcp.amm` block instead of overwriting your file.

Replace placeholder paths such as `/home/you/.amm/amm.db` with your real paths before enabling the config.

## What this plugin captures

- session lifecycle markers (`session_start`, `session_idle`)
- user messages (`message_user`)
- assistant messages (`message_assistant`) from final message states
- tool calls from `tool.execute.before` (`tool_call`)
- tool results from `tool.execute.after` (`tool_result`)

Use amm's MCP surface for explicit recall and remember operations while this plugin handles the stable glue.

For the complete integration guide, configuration reference, and operational patterns, see [`../../docs/opencode-integration.md`](../../docs/opencode-integration.md).
