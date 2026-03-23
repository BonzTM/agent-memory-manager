---
name: amm-session-maintenance
description: Run warm-path amm maintenance when OpenClaw stops a session.
metadata: { "openclaw": { "emoji": "♻️", "events": ["command:stop"] } }
---

# amm Session Maintenance

This hook records a lightweight session-stop event and then runs the warm-path amm jobs:

- `reflect`
- `compress_history`
- `consolidate_sessions`

It does not run the heavier maintenance jobs. Keep those on the cold path through host cron or another external trigger.

## Environment

- `AMM_BIN` — optional absolute path to `amm` (defaults to `amm`)
- `AMM_DB_PATH` — optional path to the amm SQLite database (defaults to `~/.amm/amm.db`)
- `AMM_PROJECT_ID` — optional stable project identifier for scoped recall/history
