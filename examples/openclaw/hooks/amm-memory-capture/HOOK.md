---
name: amm-memory-capture
description: Capture OpenClaw inbound and outbound message events into AMM.
metadata: { "openclaw": { "emoji": "🧠", "events": ["message:preprocessed", "message:sent"] } }
---

# AMM Memory Capture

This hook records the messages OpenClaw sees and sends into AMM.

It intentionally does **not** mutate OpenClaw context or inject ambient recall automatically. Use the `amm` MCP tools for explicit recall.

## Events

- `message:preprocessed` — capture the enriched inbound body the agent is about to see
- `message:sent` — capture the outbound message that OpenClaw successfully delivered

## Environment

- `AMM_BIN` — optional absolute path to `amm` (defaults to `amm`)
- `AMM_DB_PATH` — optional path to the AMM SQLite database (defaults to `~/.amm/amm.db`)
- `AMM_PROJECT_ID` — optional stable project identifier for scoped recall/history
