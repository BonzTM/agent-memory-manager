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

## Working Rules

- Prefer small, reviewable changes over broad cleanup.
- Do not invent product requirements or architectural decisions — surface the gap and wait.
- If verification fails, fix the issue or report clearly. Do not claim the task is complete.
- Implementation must stay aligned with `refined-spec.md` and `technical-blueprint.md`. Flag divergence.
- Go behavior changes need test coverage or explicit exemption.
- Schema changes must go through the migration system in `internal/adapters/sqlite/migrations.go`.
