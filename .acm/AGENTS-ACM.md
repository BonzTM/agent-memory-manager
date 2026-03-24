# ACM Agent Workflow — amm

This extends [AGENTS.md](../AGENTS.md) with the ACM-managed workflow for agents that have `acm` available. All invariants and working rules from AGENTS.md still apply.

See [acm-work-loop.md](acm-work-loop.md) for the full command reference.

## Source Of Truth

- Canonical rules: `.acm/acm-rules.yaml`
- Canonical tags: `.acm/acm-tags.yaml`
- Canonical verification: `.acm/acm-tests.yaml`
- Canonical workflow gates: `.acm/acm-workflows.yaml`
- ACM work storage is the source of truth for active and historical plan state.

## Task Loop

For non-trivial work (multi-step, multi-file, or governed changes), follow this loop. Trivial single-file fixes can skip the ACM ceremony.

1. Read `AGENTS.md` (and `CLAUDE.md` if using Claude) and the human task.
2. Run `acm context --task-text "<current task>" --phase <plan|execute|review>`.
3. Read the returned hard rules and fetch only the keys needed for the current step.
4. If the task spans multiple steps, multiple files, or likely handoff, create or update ACM work with `acm work ...`.
5. For code, config, schema, or behavior changes, run `acm verify ...` before completion.
6. If `.acm/acm-workflows.yaml` requires a review task such as `review:cross-llm`, satisfy it with `acm review --run --receipt-id <receipt-id>` when the task defines a `run` block; otherwise use manual review fields or `acm work`.
7. Close the task with `acm done ...`. Changed files must stay within the active receipt scope.

## When To Use `work`

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

## ACM-Specific Working Norms

- Do not silently expand governed file scope. Refresh context first.
- Keep work state current when you pause, hand off, or hit a blocker.

## Ruleset Maintenance

1. Edit the canonical rules, tags, tests, or workflow files.
2. Run `acm sync --mode working_tree --insert-new-candidates` or `acm health --apply`.
3. Run `acm health --include-details` and resolve blocking findings.
