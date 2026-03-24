# CLAUDE.md

Claude companion for amm (Agent Memory Manager). Primary contract is `AGENTS.md`.

- Follow `AGENTS.md` first. If this file conflicts, `AGENTS.md` wins.
- If ACM is available in your session, also follow [.acm/AGENTS-ACM.md](.acm/AGENTS-ACM.md).

## amm-Specific Notes

- amm is Go, API-first, CLI/MCP-driven. All business logic flows through `internal/core/service.go`.
- `refined-spec.md` and `technical-blueprint.md` are the design authority. Flag divergence.
- Canonical tables are truth; derived tables (FTS5, embeddings) are rebuildable.
- Schema changes go through `internal/adapters/sqlite/migrations.go` — no ad-hoc DDL.
- CLI (`cmd/amm`) and MCP (`cmd/amm-mcp`) must expose the same commands.
- Contract changes must update `internal/contracts/v1` and `spec/v1` together.
- Go behavior changes need test coverage. Prefer targeted package tests before the full suite.
