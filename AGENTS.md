# AGENTS.md

Operating contract for **amm (Agent Memory Manager)** — a Go, API-first, CLI/MCP-driven persistent memory substrate for agents.

## Source Of Truth

- Follow this file first.
- Design intent lives in `refined-spec.md` (what amm is) and `technical-blueprint.md` (how to build it).
- Keep canonical rules in `.acm/acm-rules.yaml`.
- Keep canonical tags in `.acm/acm-tags.yaml` and executable checks in `.acm/acm-tests.yaml`.
- Keep canonical completion workflow gates in `.acm/acm-workflows.yaml`.
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

## Required Task Loop

1. Read this file and the human task.
2. Run `acm context` before opening or editing project files.
3. Follow all hard rules returned in the receipt.
4. Use `fetch` only for the pointers, plans, and task keys needed for the current step.
5. When a task spans multiple steps, multiple files, or a likely handoff, create or update `work`.
6. If code, config, schema, or other executable behavior changes, run `verify` before `done`.
7. If `.acm/acm-workflows.yaml` requires review task keys, satisfy them before `done`.
8. End every task with `done`, including every changed file for file-backed work.
9. If you learn a reusable decision, gotcha, or preference, record it with `memory`.

When the task changes rules, tags, tests, workflows, or tool-surface behavior, refresh with `acm sync --mode working_tree --insert-new-candidates` and then run `acm health --include-details` before `done`.

If you need to resume after compaction, use `acm history` for discovery then `acm fetch` the returned keys.
If you need to debug project setup, use `acm status`.

## Working Rules

- Do not silently expand governed file scope. Refresh context first.
- Prefer small, reviewable changes over broad cleanup.
- Do not invent product requirements or architectural decisions — surface the gap and wait.
- If verification fails, fix the issue or report clearly. Do not claim the task is complete.
- Keep work state current when you pause, hand off, or hit a blocker.
- Implementation must stay aligned with `refined-spec.md` and `technical-blueprint.md`. Flag divergence.
- Go behavior changes need test coverage or explicit exemption.
- Schema changes must go through the migration system in `internal/adapters/sqlite/migrations.go`.

## When To Use work

Use `work` when any of the following are true:
- the task will take more than one material step
- more than one file or subsystem is involved
- the task includes explicit planning, verification, or handoff
- you need durable task state that should survive compaction or session reset

For code changes, include a `verify:tests` task.

## Staged Plan Contract

Governed multi-step work in this repo must use the staged plan contract:

- Create a root plan with `kind=feature|maintenance|governance`, explicit scope metadata (`objective`, `in_scope`, `out_of_scope`, `constraints`, `references`), and `plan.stages.spec_outline` / `refined_spec` / `implementation_plan`.
- Create top-level `stage:*` tasks with child tasks linked through `parent_task_key`.
- Leaf tasks are the atomic execution units and must include `acceptance_criteria` (at least 2: one output, one proof) and `references` (1-3 exact repo paths).
- Use `kind=feature_stream` plus `parent_plan_key` for parallel execution streams under a feature root.
- The schema is enforced through `scripts/acm-feature-plan-validate.py` via `acm verify`.

### Leaf task rules
- Each leaf must describe one deliverable with bounded scope.
- Do not create catch-all tasks: `misc`, `polish`, `remaining`, `cleanup`, `wire the rest`.
- Use real `depends_on` edges between tasks.
- Gate tasks (`verify:tests`, `review:*`) are exempt from acceptance criteria requirements.

### Orchestrator ownership
- One orchestrator agent owns every multi-step plan: root plan, stage transitions, scope declarations, verification, review, and done.
- Leaf tasks are execution units, not planning documents.
- When the tool supports sub-agents, delegate bounded leaf tasks to keep the orchestrator's context narrow.

### Thin plan exemption
- Plans with a single non-gate task and no stages are exempt from the full staged plan schema. This covers simple bugfixes and single-step tasks.

## Ruleset Maintenance

1. Edit the canonical rules, tags, tests, or workflow files.
2. Run `acm sync --mode working_tree --insert-new-candidates` or `acm health --apply`.
3. Run `acm health --include-details` and resolve blocking findings.

## Tool-Specific Companions

`CLAUDE.md` and other tool-specific files should stay thin and map their workflow back to this file.
If they disagree with this file, this file is authoritative.
