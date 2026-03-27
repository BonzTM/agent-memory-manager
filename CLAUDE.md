# CLAUDE.md

Claude companion for amm (Agent Memory Manager). Primary contract is `AGENTS.md`.

- Follow `AGENTS.md` first. If this file conflicts, `AGENTS.md` wins.
- If ACM is available in your session, also follow [.acm/AGENTS-ACM.md](.acm/AGENTS-ACM.md).

## Claude-Specific Notes

- Prefer targeted package tests (`go test ./internal/<pkg>/...`) before running the full suite.
- Use AMM's own MCP tools (`amm_recall`, `amm_remember`, etc.) for durable memory — do not write to the SQLite database directly.
- When making contract or schema changes, verify CLI/MCP parity by checking both `cmd/amm` and `cmd/amm-mcp` wiring.
