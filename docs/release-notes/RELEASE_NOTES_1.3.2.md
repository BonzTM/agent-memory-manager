# [1.3.2] Release Notes - 2026-04-02

## Release Summary

amm 1.3.2 improves extraction reliability and memory quality. The main themes are: heuristic fallback outputs are now retried with the LLM on subsequent maintenance passes instead of being permanent; duplicate detection gains an embedding-based fallback for near-duplicates that differ in surface text; and stale open loops are automatically archived when resolved by a decision or untouched for 30 days. Fresh installs also ship with a default ingestion policy that ignores tool events out of the box.

## Fixed

- **Stale metadata cleared on reset-derived.** `ResetDerived` now strips processing metadata (`extraction_method`, `extraction_model`, `fallback_count`, `entities_extracted`, `lifecycle_reviewed_at`, etc.) from manually-created memories that survive the purge. Previously these stale fields caused incorrect retry/enrichment behavior after a derived data reset.

- **Lower open_loop dedup threshold.** Embedding-based duplicate detection for `open_loop` memories uses a lower cosine similarity threshold (0.82 vs the default 0.85), reducing cases where semantically equivalent open loops were stored as separate memories.

## Added

- **Default tool event ignore policy.** Fresh installs now seed a `tool_*` ignore policy (kind glob, priority 100) during migration. Operators no longer need to manually run `amm policy-add` to suppress tool event noise. Existing databases are unaffected — the migration only inserts when the policy does not already exist.

- **Embedding-based candidate deduplication fallback.** `processMemoryCandidates` falls back to cosine-similarity dedup (`findDuplicatesByEmbedding`) when text-based matching finds no duplicates. Catches near-duplicates where the wording differs but the semantic content is the same.

- **Enrichment relationship creation.** The `Enrich` pipeline now wires `AnalyzeEvents` relationship candidates through to `createRelationshipsFromAnalysis`, building knowledge-graph edges from LLM analysis during enrichment — not just entity nodes.

- **Open loop lifecycle archival.** `LifecycleReview` deterministically archives open loops that are:
  - **Stale:** no recall access in 30 days, or
  - **Resolved:** a matching active decision memory exists with a newer timestamp.

  These deterministic archives are merged with the LLM-driven review results and processed in the same batch.

- **Heuristic fallback retry pipeline.** When an LLM call fails and the pipeline falls back to heuristic extraction, the output is marked as retryable rather than permanent. On subsequent reflect/consolidate passes, the LLM is re-attempted (up to 3 tries). Tracking is via `reflect_fallback_count` on events and `fallback_count` on memories/summaries.

- **LLM fallback WARN logging.** All LLM intelligence operations now emit `slog.Warn` when falling back to heuristic processing, including the operation name, fallback type, configured model, and original error. Makes it straightforward to identify LLM connectivity or configuration issues from logs.

## Changed

- **Summary-level retry tracking.** Heuristic-produced session summaries now carry a `fallback_count` in metadata. `ConsolidateSessions` checks `summaryNeedsLLMRetry` to re-attempt LLM narrative generation on subsequent passes, matching the event-level retry behavior.

## Admin/Operations

- Fresh installs automatically get a `tool_*` ignore ingestion policy. Existing installations that already have this policy (or a custom equivalent) are unaffected.
- LLM fallback events are now visible at WARN level in structured logs. If you see repeated fallback warnings, check your `AMM_SUMMARIZER_MODEL` / `AMM_REVIEW_MODEL` configuration and API key.
- The retry pipeline is transparent — no new configuration required. Retries happen automatically during the next scheduled reflect or consolidate_sessions maintenance pass.

## Deployment and Distribution

- Release binaries: `amm`, `amm-mcp`, `amm-http`
- Docker image: `ghcr.io/bonztm/agent-memory-manager`
- Helm chart path: `deploy/helm/amm`

```bash
docker pull ghcr.io/bonztm/agent-memory-manager:1.3.2
helm upgrade --install amm ./deploy/helm/amm --set image.tag=1.3.2
```

## Breaking Changes

None.

## Known Issues

None.

## Compatibility and Migration

No manual migration required. Database migrations run automatically on startup:
- **SQLite:** migration 10 seeds the default tool event ignore policy.
- **PostgreSQL:** migration 4 seeds the default tool event ignore policy.

Both migrations use `INSERT OR IGNORE` / `ON CONFLICT DO NOTHING`, so existing policies are preserved.

## Full Changelog

- Release tag: https://github.com/bonztm/agent-memory-manager/releases/tag/1.3.2
- Full changelog: https://github.com/bonztm/agent-memory-manager/blob/main/CHANGELOG.md
