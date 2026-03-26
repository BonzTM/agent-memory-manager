---
name: amm-memory-capture
description: Capture OpenClaw message and tool events into amm.
metadata: { "openclaw": { "emoji": "🧠", "events": ["message:preprocessed", "message:sent", "tool:called", "tool:completed"] } }
---

# amm Memory Capture

This hook records the messages and tool operations OpenClaw sees and sends into amm.

It intentionally does **not** mutate OpenClaw context or inject ambient recall automatically. Use the `amm` MCP tools for explicit recall.

## Events

- `message:preprocessed` — capture the enriched inbound body the agent is about to see
- `message:sent` — capture the outbound message that OpenClaw successfully delivered
- `tool:called` — capture tool or function invocation with tool name and input arguments
- `tool:completed` — capture tool or function completion output

## Environment

- `AMM_BIN` — optional absolute path to `amm` (defaults to `amm`)
- `AMM_DB_PATH` — optional path to the amm SQLite database (defaults to `~/.amm/amm.db`)
- `AMM_PROJECT_ID` — optional stable project identifier for scoped recall/history
