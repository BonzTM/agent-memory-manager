# CLAUDE.md

Claude companion for AMM (Agent Memory Manager). Primary contract is `AGENTS.md`.

## Source Of Truth

- Follow `AGENTS.md` first.
- Use this file only to map Claude's workflow to the repo contract.
- If this file conflicts with `AGENTS.md`, `AGENTS.md` wins.

## Claude Workflow

1. Start with `/acm-context ...`.
2. Read the returned hard rules before touching files.
3. Use `/acm-work ...` when the task is multi-step, spans multiple files, or needs durable state.
4. Use `/acm-verify ...` before `/acm-done ...` for any code, config, schema, or executable behavior change.
5. Use `/acm-done ...` to close the task; include changed files for file-backed work.
6. Use `/acm-memory ...` for durable decisions and gotchas.

If the task changes rules, tags, tests, workflows, or tool-surface behavior, run direct CLI `acm sync --mode working_tree --insert-new-candidates` and `acm health --include-details` before `/acm-done`.

If you need historical discovery after compaction, use direct CLI `acm history` then `acm fetch`.
If you need runtime or setup diagnostics, use direct CLI `acm status`.

## AMM-Specific Notes

- AMM is Go, API-first, CLI/MCP-driven. All business logic flows through `internal/core/service.go`.
- `refined-spec.md` and `technical-blueprint.md` are the design authority. Flag divergence.
- Canonical tables are truth; derived tables (FTS5, embeddings) are rebuildable.
- Schema changes go through `internal/adapters/sqlite/migrations.go` — no ad-hoc DDL.
- CLI (`cmd/amm`) and MCP (`cmd/amm-mcp`) must expose the same commands.
- Contract changes must update `internal/contracts/v1` and `spec/v1` together.
- Go behavior changes need test coverage. Prefer targeted package tests before the full suite.

## Ruleset Maintenance

When `.acm/acm-rules.yaml`, `.acm/acm-tags.yaml`, `.acm/acm-tests.yaml`, or `.acm/acm-workflows.yaml` changes, refresh broker state with `acm sync` or `acm health --apply`, then run `acm health`.
