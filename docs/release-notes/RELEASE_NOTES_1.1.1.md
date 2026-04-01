# [1.1.1] Release Notes - 2026-04-01

## Release Summary

amm 1.1.1 is a patch release focused on memory extraction quality. The extraction prompt has been restructured to produce fewer, richer memories — especially when running on non-frontier models like Qwen, DeepSeek, or Gemini Flash. Events ingested through global hooks now automatically pick up project scope when the working directory matches a registered project. The Helm chart ships a maintenance CronJob out of the box, and the maintenance script now produces structured logs suitable for cron capture.

## What's New

### Smarter project scoping from global hooks

Events ingested without an explicit `project_id` now get one automatically. When an event carries a `cwd` field in its metadata, AMM checks it against registered project paths and assigns the matching project. This means global ingestion hooks (like Claude Code or Codex session hooks) no longer produce globally-scoped memories for project-specific work — memories land in the right project without any hook changes.

### Better memory extraction, especially on smaller models

The extraction prompt has been rewritten from the ground up to work well with non-frontier LLMs:

- **Fewer memories.** The prompt now frames extraction as "evaluate whether anything is worth keeping" rather than "extract memories." The default expectation is an empty result. Selectivity is reinforced at both the beginning and end of the prompt.
- **Richer bodies.** Memory bodies that just restate the short description are now explicitly flagged as defects. The prompt asks for context, reasoning, and "why it matters" in every body.
- **Better confidence scores.** Calibration anchors (0.95 for explicit statements down to 0.5 for speculation) replace the vague "0.0 to 1.0" guidance that led to uniform 0.85 scores.
- **Guidance for all 10 memory types.** Every type now has a one-liner explaining when to use it and what the body should contain. Previously only decision, preference, and fact had any guidance.
- **User questions are filtered.** The prompt now steers extraction toward answers and conclusions rather than the questions that prompted them.
- **Clearer structure for weaker models.** Rules are organized into labeled sections (FILTERING, BODY QUALITY, TYPE REFERENCE) instead of a flat list, improving instruction adherence on models that struggle with long prompts.

### Helm chart maintenance CronJob

The chart now includes a CronJob that runs the full 7-phase maintenance pipeline every 30 minutes. It uses a lightweight busybox container to call the AMM HTTP API, matching the pattern used in production deployments. Enabled by default — configure or disable via `maintenance.cronjob.*` values.

### Better maintenance script logging

`examples/scripts/run-workers.sh` now produces structured, timestamped output:

```
2026-04-01T12:30:00Z [amm-maintenance] Starting full maintenance pipeline
2026-04-01T12:30:00Z [amm-maintenance] ── Phase 1: Extract memories from events ──
2026-04-01T12:30:03Z [amm-maintenance] OK   reflect (3s)
...
2026-04-01T12:30:45Z [amm-maintenance] Done: 23 jobs (23 passed, 0 failed) in 45s
```

Every line has a UTC timestamp and the job name, duration, and status. The end-of-run summary reports total pass/fail counts. Works well both interactively and when capturing cron logs.

### Version bump script improvements

The bump script now covers two previously-missed version surfaces:
- `examples/openclaw/package.json`
- `examples/hermes-agent/amm-memory/plugin.yaml`

## Deployment and Distribution

- Release binaries: `amm`, `amm-mcp`, `amm-http`
- Docker image: `ghcr.io/bonztm/agent-memory-manager`
- Helm chart: published at `https://bonztm.github.io/agent-memory-manager`

```bash
docker pull ghcr.io/bonztm/agent-memory-manager:1.1.1
helm repo add amm https://bonztm.github.io/agent-memory-manager
helm upgrade --install amm amm/amm --set image.tag=1.1.1
```

## Breaking Changes

None. This is a backwards-compatible patch release.

## Compatibility and Migration

No migration required. The extraction prompt changes take effect on the next reflect run. Existing memories are not affected — only newly extracted memories will reflect the improved quality.

To take advantage of project scoping, register your projects first:

```bash
amm project add --name my-project --path /home/you/git/my-project
```

Future events with a matching `cwd` will automatically receive the project scope.

## Full Changelog

- Release tag: https://github.com/bonztm/agent-memory-manager/releases/tag/1.1.1
- Full changelog: https://github.com/bonztm/agent-memory-manager/blob/main/CHANGELOG.md
