# [1.1.0] Release Notes - 2026-03-31

## Release Summary

amm 1.1.0 is a minor release focused on recall intelligence, scoring accuracy, memory creation precision, and runtime integration quality. It adds automatic query intent routing that steers hybrid recall to specialized modes, temporal staleness detection that penalizes memories with stale relative-time references, and contradiction surfacing that annotates recalled items with conflicting memory IDs. The OpenClaw integration has been overhauled from a repo-local hook bundle to a native OpenClaw 2026.03.31 plugin with automatic ambient recall injection and dual transport support. It also fixes an inverted renormalization bug, collapses two redundant time-decay signals into one, expands Bayesian learned ranking to 9 of 10 signals, and adds recall-side deduplication and intake quality gates.

## Fixed

- `ResetDerived` FK ordering: claims are now deleted before memories in both PostgreSQL and SQLite adapters, fixing a foreign key constraint violation (`claims_memory_id_fkey`) that prevented derived data purges.
- Renormalization inversion: enabling semantic embeddings no longer penalizes lexical, entity, and other scoring signals. Missing semantic weight is now correctly redistributed upward to present signals.
- PostgreSQL array scanning for repository reads (nil slice handling).
- PostgreSQL nil slice writes causing query errors.
- Hermes plugin metadata serialization: metadata is now sent as `map[string]string` with JSON string serialization.
- ANN search now falls back to brute-force when results are empty (not just on error).
- Embedding upsert syncs the `embedding_vec` column when present, with logged warnings on failure instead of silent drops.
- Test assertion ordering: source lookup assertions now run before memory supersede to prevent flaky test results.

## Added

- Query intent routing: hybrid recall auto-routes to specialized modes (contradictions, entity) when query intent is clear. Ambient mode is never re-routed. `RecallMeta.RoutedFrom` reports the original mode when routing occurs.
- Temporal staleness scoring: memories containing relative-time language ("currently", "today", etc.) receive a 0–0.3 scoring penalty that ramps over 180 days after a 14-day freshness threshold.
- Contradiction surfacing in recall: recalled memories referenced in active contradictions include a `conflicts_with` field listing conflicting memory IDs. Visibility-gated per caller.
- Intake quality gates: configurable minimum confidence and importance thresholds via `AMM_MIN_CONFIDENCE_FOR_CREATION` and `AMM_MIN_IMPORTANCE_FOR_CREATION`.
- EventQuality classification wiring: the reflect pipeline now consumes LLM event quality assessments (durable/ephemeral/noise) to filter candidates sourced entirely from low-quality events.
- Built-in ONNX embedding provider: a `builtin_embeddings` build tag enables a local embedding provider without requiring an external API endpoint.
- PostgreSQL ANN vector search: migration 3 enables a vector extension for approximate nearest neighbor search.
- Recall-side deduplication: near-duplicate results collapsed using cosine similarity (threshold 0.85) with Jaccard text fallback.
- Configurable entity hub dampening threshold via `AMM_ENTITY_HUB_THRESHOLD` (default 10).
- Known tech entity extraction: 60+ lowercase technology names recognized without capitalization.
- Hermes AMM plugin example for Hermes-Agent integration.
- Durability check in LLM extraction prompt: candidates assessed for 30-day relevance before creation.
- Native OpenClaw plugin (`examples/openclaw/`) for OpenClaw 2026.03.31+. Uses `definePluginEntry()` with `openclaw.plugin.json` manifest, replacing the previous hook-bundle approach.
- Automatic ambient recall injection for OpenClaw via `before_prompt_build` hook. Relevant memories are prepended to every LLM prompt without the agent calling any tool, matching Hermes plugin behavior.
- Dual transport in the OpenClaw plugin: local `amm` binary (default) or HTTP API via `AMM_API_URL`, with MCP-over-HTTP sidecar support for remote deployments.
- Plugin-level `configSchema` for OpenClaw plugin configuration alongside environment variables.

## Changed

- Built-in embedding provider now uses real GloVe word vectors (50d, 100K vocab + tech terms) instead of a hash-based stub. Pure Go, no CGo, no external API. Binary size increases ~5MB with `builtin_embeddings` tag.
- Collapsed recency and freshness signals into a single recency signal. Former freshness weight (4%) redistributed: +2% to entity overlap (18%→20%) and +2% to recency (6%→8%).
- Expanded Bayesian learned ranking to 9 of 10 positive signals (was 6). Lexical, entity overlap, scope fit, and source trust weights now learnable from user feedback.
- Scoring weights no longer use a pre-normalization factor. Raw weights sum to 1.0 directly with dynamic renormalization at score time.
- Heuristic extraction now requires 2+ phrase cue group matches (was 1) and assigns confidence 0.45 (was 0.6).
- Heuristic fallback floor: when no LLM is configured and no explicit confidence gate is set, minimum confidence lowers to 0.40.
- Session trace archive TTL reduced from 7 days to 3 days.
- Minimum hybrid history score threshold adjusted from 0.55 to 0.48.
- Integration documentation updated to recommend disabling tool_call and tool_result event ingestion.
- OpenClaw integration overhauled from repo-local hook bundle to native OpenClaw plugin with `openclaw.plugin.json` manifest and `definePluginEntry()` entry point.
- OpenClaw event capture consolidated from standalone `hooks/` directories into plugin-registered hooks.
- OpenClaw integration documentation (`docs/openclaw-integration.md`) rewritten for the native plugin architecture.

## Removed

- OpenClaw `hooks/amm-memory-capture/` and `hooks/amm-session-maintenance/` standalone hook directories. Event capture is now handled by the native plugin. Session maintenance is external (host cron or systemd).
- OpenClaw `cron.add.reflect.json` cron artifact. The plugin is hot-path only; maintenance scheduling stays external via `examples/scripts/run-workers.sh`.

## Admin/Operations

| Environment Variable | Default | Description |
|---|---|---|
| `AMM_MIN_CONFIDENCE_FOR_CREATION` | `0.50` | Minimum confidence for memory creation (0.0-1.0) |
| `AMM_MIN_IMPORTANCE_FOR_CREATION` | `0.30` | Minimum importance for memory creation (0.0-1.0) |
| `AMM_ENTITY_HUB_THRESHOLD` | `10` | Entity link count before hub dampening activates |
| `AMM_OPENCLAW_RECALL_LIMIT` | `5` | Max ambient recall items injected per OpenClaw turn |

## Deployment and Distribution

- Release binaries: `amm`, `amm-mcp`, `amm-http`
- Docker image: `ghcr.io/bonztm/agent-memory-manager`
- Helm chart: published at `https://bonztm.github.io/agent-memory-manager`

```bash
docker pull ghcr.io/bonztm/agent-memory-manager:1.1.0
helm repo add amm https://bonztm.github.io/agent-memory-manager
helm upgrade --install amm amm/amm --set image.tag=1.1.0
```

## Breaking Changes

- The `freshness` signal has been removed from the scoring breakdown. Clients that parse the `explain` response should no longer expect a `freshness` key in the signal map. The signal's contribution has been folded into `recency`.
- Default scoring weights have changed. Any persisted learned ranking weights (from `update_ranking_weights` jobs) will be normalized against the new weight distribution on next load.

## Known Issues

- The built-in embedding provider uses GloVe word-vector averaging (~60-70% quality of transformer models). It captures genuine semantic similarity but doesn't understand word order or sentence structure. For highest quality, use an external embedding API endpoint (e.g., OpenAI `text-embedding-3-small` via OpenRouter).
- PostgreSQL ANN vector search requires manual operator setup (column and HNSW index creation) after the extension is enabled by migration 3.

## Compatibility and Migration

This release is backwards-compatible with 1.0.0 data. SQLite and PostgreSQL migrations run automatically on startup. No manual migration steps are required.

Operators using the `explain` recall mode should update any downstream parsing to account for the removed `freshness` signal key.

## Full Changelog

- Release tag: https://github.com/bonztm/agent-memory-manager/releases/tag/1.1.0
- Full changelog: https://github.com/bonztm/agent-memory-manager/blob/main/CHANGELOG.md
