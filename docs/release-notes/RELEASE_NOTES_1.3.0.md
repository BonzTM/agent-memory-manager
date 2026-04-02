# [1.3.0] Release Notes - 2026-04-02

## Release Summary

amm 1.3.0 adds session recall and temporal search — the ability to find and surface past conversations by time and content. When an agent or user says "last week we worked on X" or "what did we discuss yesterday," amm can now return the relevant session narratives. This release also decouples maintenance processing from hooks, adds reasoning model support for LLM calls, and improves session summary quality for better discoverability.

## What's New

### Session recall mode

New `--mode sessions` for recall that lists and searches session summaries specifically.

```bash
# List recent sessions
amm recall --mode sessions --limit 20

# Search sessions by content
amm recall "consolidation pipeline" --mode sessions

# Sessions from last week
amm recall --mode sessions --after 2026-03-26

# Combined: search within a date range
amm recall "terraform" --mode sessions --after 2026-03-01 --before 2026-04-01
```

Unlike hybrid recall which mixes memories, summaries, events, and episodes, sessions mode returns only session-level narratives — what was worked on, what was decided, what's unresolved. Results are reverse-chronological.

### Temporal search across all recall modes

All recall modes now support `--after` and `--before` flags (RFC3339 timestamps) for hard date-range filtering.

```bash
amm recall "errors" --mode history --after 2026-03-01 --before 2026-04-01
amm recall "terraform" --after 2026-03-26T00:00:00Z
```

Natural-language temporal references in queries are automatically extracted:

```bash
amm recall "terraform yesterday"        # extracts: after=yesterday, query="terraform"
amm recall "work from last week"         # extracts: after=Mon, before=Sun of last week
amm recall "in September 2025"           # extracts: Sep 1 - Sep 30 2025
amm recall "recently"                    # extracts: last 3 days
```

The parser handles: today, yesterday, earlier/previously/recently, last week, this week, N days/weeks ago, last/this month, named months with optional year, quarters (Q1-Q4), and last year. Explicit `--after`/`--before` flags override extraction when both are present.

### Hooks are now capture-only

All integration hooks (Claude Code, Codex, Hermes, OpenCode) no longer run maintenance jobs at session end. This means:

- **Session stop is instant** — no 30-60s wait for LLM-backed reflect/consolidate/compress
- **Processing is batched** — maintenance runs on a schedule, processing all sessions that accumulated since the last run
- **Failures are retried** — a failed cron job retries next interval instead of silently dying in a hook

Hooks now only: ingest events (user messages, assistant messages, session markers) and return ambient recall hints.

**Maintenance should run externally** via cron, systemd timer, or the Helm CronJob (already included in the chart at `maintenance.cronjob`):

```bash
# Simplest cron entry (every 15 minutes)
*/15 * * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run reflect && \
  /usr/local/bin/amm jobs run consolidate_sessions && \
  /usr/local/bin/amm jobs run compress_history && \
  /usr/local/bin/amm jobs run rebuild_indexes >/dev/null 2>&1

# Or use the full phased worker script
*/15 * * * * /path/to/examples/scripts/run-workers.sh >/dev/null 2>&1
```

### Reasoning model support

New configuration for reasoning-capable models (OpenAI o1, o3, o4-mini, etc.):

```bash
# Environment variables — effort level (sends reasoning: {"effort": "..."})
AMM_SUMMARIZER_REASONING_EFFORT=medium    # for narrative compression
AMM_REVIEW_REASONING_EFFORT=high          # for extraction/reasoning

# Or simple toggle (sends reasoning: {"enabled": true})
AMM_SUMMARIZER_REASONING=enabled
AMM_REVIEW_REASONING=enabled

# Or in config.json
"summarizer": {
  "reasoning": "enabled",
  "reasoning_effort": "medium",
  "review_reasoning": "enabled",
  "review_reasoning_effort": "high"
}
```

The `reasoning` and `reasoning_effort` tunables are independent — set one, both, or neither depending on what the model supports. When both are set, `reasoning_effort` takes precedence. API requests send the standard `reasoning` object format (e.g., `"reasoning": {"effort": "medium"}`). Valid effort values: `low`, `medium`, `high`.

### Better session summaries

The consolidation prompt now produces two distinct fields:

- **`title`** — Human-readable session headline (under 80 chars). Names the project and primary activity. Purpose: scanning a list of sessions to decide which to expand.
- **`tight_description`** — Retrieval-optimized search keywords (under 120 chars). Purpose: FTS and embedding matching so `amm recall "consolidation pipeline"` finds the right session.

Run `amm reset-derived` followed by `amm jobs run consolidate_sessions` to regenerate summaries for existing sessions.

## Fixed

- Empty query allowed for `mode=sessions` (list all sessions without text search)
- MCP adapter forwards `after`/`before` fields through recall validation
- Session summary temporal filtering uses event occurrence time, not consolidation time
- SQLite date comparison normalized to UTC for consistent text ordering
- `ListEvents` SQL uses inclusive `>=`/`<=` for boundary events
- OpenCode plugin idle-event handler no longer throws after maintenance removal
- Hermes session-end hook exports shell variables for Python subprocess
- TOML config parser handles reasoning effort and reasoning toggle keys

## Admin/Operations

- `AMM_TEMPORAL_ATTENUATION` (float64, 0.0-1.0, default 0.3): score multiplier for items outside the temporal window in scored recall modes
- `AMM_SUMMARIZER_REASONING` (string, "enabled" or empty): simple reasoning toggle — sends `reasoning: {"enabled": true}`
- `AMM_SUMMARIZER_REASONING_EFFORT` (string, low/medium/high): reasoning effort — sends `reasoning: {"effort": "..."}`
- `AMM_REVIEW_REASONING` (string, "enabled" or empty): reasoning toggle for review model
- `AMM_REVIEW_REASONING_EFFORT` (string, low/medium/high): reasoning effort for review model
- `AMM_SUMMARIZER_TIMEOUT_SECONDS` (int, default 300): HTTP client timeout for LLM calls (was hardcoded at 30s)
- `AMM_EMBEDDING_TIMEOUT_SECONDS` (int, default 30): HTTP client timeout for embedding calls
- `AMM_HTTP_READ_TIMEOUT_SECONDS` (int, default 30): HTTP server read timeout
- `AMM_HTTP_WRITE_TIMEOUT_SECONDS` (int, default 60): HTTP server write timeout
- `AMM_HTTP_IDLE_TIMEOUT_SECONDS` (int, default 120): HTTP server idle timeout
- Helm chart: `retrieval.temporalAttenuation`, `summarizer.reasoningEffort`, `review.reasoningEffort` added to values.yaml and configmap

## Deployment and Distribution

- Release binaries: `amm`, `amm-mcp`, `amm-http`
- Docker image: `ghcr.io/bonztm/agent-memory-manager`
- Helm chart path: `deploy/helm/amm`

```bash
docker pull ghcr.io/bonztm/agent-memory-manager:1.3.0
```

## Upgrade Notes

1. **Update hooks.** If you installed hook scripts from the `examples/` directory, re-copy them to remove maintenance job calls from stop/session-end hooks.
2. **Set up a maintenance scheduler.** Hooks no longer run maintenance. Add a cron entry, systemd timer, or enable the Helm CronJob to run `amm jobs run` periodically (recommended: every 15-30 minutes).
3. **Regenerate session summaries.** Run `amm reset-derived` then `amm jobs run consolidate_sessions` to get improved titles and search keywords on existing session summaries.
4. **Optional: configure reasoning.** If using a reasoning-capable model, set `AMM_SUMMARIZER_REASONING_EFFORT` and/or `AMM_REVIEW_REASONING_EFFORT` for potentially improved extraction quality.
