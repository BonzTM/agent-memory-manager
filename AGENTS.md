# AGENTS.md

Operating contract for **amm (Agent Memory Manager)** — a Go, API-first, CLI/MCP-driven persistent memory substrate for agents.

## Quick Start

1. Read this file for repo rules, architecture, and change routing.
2. If your agent runtime provides ACM, see [.acm/AGENTS-ACM.md](.acm/AGENTS-ACM.md) for the enhanced workflow.  If you are unaware or unsure of what ACM is, do not read the file.
3. If using Claude, also read [CLAUDE.md](CLAUDE.md).

## Source Of Truth

- Follow this file first.
- Design intent lives in `refined-spec.md` (what amm is) and `technical-blueprint.md` (how to build it).
- If tool-specific instructions conflict with this file, this file wins unless a human explicitly says otherwise.

## Project Architecture

amm is:
- **Go** — single binary, minimal dependencies, standard library where possible
- **API-first** — core service interface in `internal/core/service.go` is the single entry point for all logic
- **CLI/MCP-driven** — `cmd/amm` (convenience CLI), `cmd/amm-mcp` (MCP adapter), both call the service
- **SQLite-backed** — canonical store in SQLite, derived indexes (FTS5, embeddings) are rebuildable
- **Framework-agnostic** — designed for integration with Claude Code, Codex, OpenClaw, Hermes, or any agent runtime

### Module Layout
```
cmd/amm/         CLI entrypoint
cmd/amm-mcp/     MCP adapter
internal/
  core/          Service + repository interfaces, errors
  service/       Business logic implementation
  adapters/
    cli/         JSON envelope runner
    mcp/         MCP tool invocation
    sqlite/      SQLite repository + migrations
  contracts/v1/  Typed payloads, validation, command catalog
  commands/      Command dispatch
  runtime/       Config, service factory, logger
```

### Key Invariants
- **Service layer is the only entry point.** CLI, MCP, and HTTP are adapters. They must not contain business logic or direct SQL.
- **Canonical tables are truth.** Events, summaries, memories, claims, entities, episodes, artifacts, jobs. Derived tables (FTS5, embeddings, caches) are disposable and rebuildable.
- **Contracts and schema stay in lockstep.** Changes to payloads or commands must update `internal/contracts/v1`, `spec/v1` schemas, and tests together.
- **CLI and MCP expose the same commands.** Parity is mandatory.

## Prerequisites

- **Go 1.21+** (module requires 1.26.1; any recent Go toolchain handles this)
- **CGO enabled** — SQLite is accessed via `github.com/mattn/go-sqlite3`, which requires a C compiler (`gcc` or `cc`) and SQLite headers
- **Python 3** — needed for validation scripts in `scripts/`
- **`.env` file** — copy `.env.example` to `.env` and populate `ACM_*` variables if using ACM workflows

## Build & Test

```bash
# Build both binaries (CGO + FTS5 required)
CGO_ENABLED=1 go build -tags fts5 ./cmd/amm ./cmd/amm-mcp

# Run targeted package tests (prefer this first)
CGO_ENABLED=1 go test -tags fts5 ./internal/core/... ./internal/runtime/... -count=1

# Run full test suite
CGO_ENABLED=1 go test -tags fts5 ./... -count=1

# Initialize a fresh database
AMM_DB_PATH=~/.amm/amm.db ./amm init
```

The `-tags fts5` flag is mandatory — FTS5 support is compile-time enforced.

## Go Conventions

This project follows the conventions in [../coding-handbook/golang](../coding-handbook/golang/). Key points:

- **Thin `main`** — `cmd/*/main.go` handles config, wiring, and shutdown only. No business logic.
- **Errors** — wrap with `%w`, inspect with `errors.Is`/`errors.As`, log once at the acting boundary.
- **Context** — first parameter for I/O and long-running work, never stored in structs.
- **Testing** — real integration tests against SQLite, not mocked repositories. Prefer targeted package tests before the full suite.
- **Dependencies** — stdlib first. Every non-trivial dependency needs explicit rationale.

See `golang/AGENTS.md` in the coding-handbook for the full fast-path contract.

## Working Rules

- Prefer small, reviewable changes over broad cleanup.
- Do not invent product requirements or architectural decisions — surface the gap and wait.
- If verification fails, fix the issue or report clearly. Do not claim the task is complete.
- Implementation must stay aligned with `refined-spec.md` and `technical-blueprint.md`. Flag divergence.
- Go behavior changes need test coverage or explicit exemption.
- Schema changes must go through the migration system in `internal/adapters/sqlite/migrations.go`.

## Protected Areas

Changes to these areas require extra care — verify thoroughly and flag in PR descriptions:

- **`internal/adapters/sqlite/migrations.go`** — migration sequence is append-only and forward-only. Never reorder, edit, or remove existing migrations.
- **`internal/contracts/v1` + `spec/v1`** — backward compatibility constraints apply. Changes must update both in lockstep with tests.
- **`.acm/` config files** — edits require `acm sync` and `acm health` before closing. See [.acm/AGENTS-ACM.md](.acm/AGENTS-ACM.md).
- **`refined-spec.md` / `technical-blueprint.md`** — design authority documents. Update only when implementation intentionally diverges.

## Git Conventions

- **Commit messages** — short imperative summary (e.g., "add recall depth parameter"). Body for non-obvious reasoning.
- **Branch naming** — `<type>/<short-description>` (e.g., `feat/recall-depth`, `fix/fts5-rebuild`).
- **PR scope** — one logical change per PR. Prefer small, reviewable PRs over bundled changes.
- **Tests with code** — behavior-changing PRs must include test updates in the same change.

## Troubleshooting

| Problem | Likely cause | Fix |
|---------|-------------|-----|
| `cgo: C compiler not found` | CGO_ENABLED=1 but no `gcc`/`cc` | Install `build-essential` (Debian) or `gcc` |
| `undefined: sqlite3` or link errors | Missing CGO or SQLite headers | Ensure `CGO_ENABLED=1` and `libsqlite3-dev` installed |
| `unknown build flag -tags fts5` | Old Go version | Upgrade to Go 1.21+ |
| FTS5 queries return empty results | Binary built without `-tags fts5` | Rebuild with the tag |
| Tests fail with "database is locked" | Concurrent test processes sharing a DB file | Use `-count=1` to disable test caching; ensure tests use isolated temp DBs |
| `acm` commands fail | Missing `.env` or ACM not installed | Check `.env` exists with `ACM_*` vars; run `acm status` to debug |
