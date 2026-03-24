# CODEX.md

Codex companion for amm (Agent Memory Manager). Primary contract is `AGENTS.md`.

## Source Of Truth

- Follow `AGENTS.md` first.
- Use this file only to map Codex's workflow to the repo contract.
- If this file conflicts with `AGENTS.md`, `AGENTS.md` wins.

## Codex Workflow

For non-trivial work (multi-step, multi-file, or governed), use the ACM task loop:

1. Start with `acm context ...`.
2. Read the returned hard rules before touching files.
3. Use `acm work ...` when the task is multi-step, spans multiple files, or needs durable state.
4. Use `acm verify ...` before `acm done ...` for any code, config, schema, or executable behavior change.
5. Use `acm done ...` to close the task; include changed files for file-backed work when practical, or let ACM derive the delta.

Trivial single-file fixes can skip the ACM ceremony.

If the task changes rules, tags, tests, workflows, or tool-surface behavior, run direct CLI `acm sync --mode working_tree --insert-new-candidates` and `acm health --include-details` before `acm done`.

If you need historical discovery after compaction, use direct CLI `acm history` then `acm fetch`.
If you need runtime or setup diagnostics, use direct CLI `acm status`.

## Memory (AMM)

AMM is available via MCP tools and CLI (`amm`). Query it early and often — see `AGENTS.md` § Memory for the full contract.

- **At session start**, run `amm recall --mode ambient` or `amm_recall` to load relevant prior context.
- **Before decisions or when uncertain**, query AMM — don't guess when it might already know.
- **After stable decisions or lessons learned**, commit them with `amm remember` or `amm_remember`.
- Use `amm expand` / `amm_expand` to expand thin recall items when you need more detail.

## amm-Specific Notes

- amm is Go, API-first, CLI/MCP-driven. All business logic flows through `internal/core/service.go`.
- `refined-spec.md` and `technical-blueprint.md` are the design authority. Flag divergence.
- Canonical tables are truth; derived tables (FTS5, embeddings) are rebuildable.
- Schema changes go through `internal/adapters/sqlite/migrations.go` — no ad-hoc DDL.
- CLI (`cmd/amm`) and MCP (`cmd/amm-mcp`) must expose the same commands.
- Contract changes must update `internal/contracts/v1` and `spec/v1` together.
- Go behavior changes need test coverage. Prefer targeted package tests before the full suite.

## Ruleset Maintenance

When `.acm/acm-rules.yaml`, `.acm/acm-tags.yaml`, `.acm/acm-tests.yaml`, or `.acm/acm-workflows.yaml` changes, refresh broker state with `acm sync` or `acm health --apply`, then run `acm health`.
