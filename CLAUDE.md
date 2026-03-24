# CLAUDE.md

Claude companion for amm (Agent Memory Manager). Primary contract is `AGENTS.md`.

## Source Of Truth

- Follow `AGENTS.md` first.
- Use this file only to map Claude's workflow to the repo contract.
- If this file conflicts with `AGENTS.md`, `AGENTS.md` wins.

If ACM is available in your session, also follow [.acm/AGENTS-ACM.md](.acm/AGENTS-ACM.md).  If you are unaware or unsure of what ACM is, do not read the file.

## ACM Workflow (when available)

See [.acm/acm-work-loop.md](.acm/acm-work-loop.md) for the full command reference. Claude slash-command equivalents:

| AGENTS-ACM.md step | Claude command |
|---|---|
| `acm context` | `/acm-context [phase] <task>` |
| `acm work` | `/acm-work` |
| `acm verify` | `/acm-verify` |
| `acm review --run` | `/acm-review <id> {"run":true}` |
| `acm done` | `/acm-done` |

Direct CLI (`acm sync`, `acm health`, `acm history`, `acm status`) has no slash-command wrappers — call those directly.

## amm-Specific Notes

- amm is Go, API-first, CLI/MCP-driven. All business logic flows through `internal/core/service.go`.
- `refined-spec.md` and `technical-blueprint.md` are the design authority. Flag divergence.
- Canonical tables are truth; derived tables (FTS5, embeddings) are rebuildable.
- Schema changes go through `internal/adapters/sqlite/migrations.go` — no ad-hoc DDL.
- CLI (`cmd/amm`) and MCP (`cmd/amm-mcp`) must expose the same commands.
- Contract changes must update `internal/contracts/v1` and `spec/v1` together.
- Go behavior changes need test coverage. Prefer targeted package tests before the full suite.
