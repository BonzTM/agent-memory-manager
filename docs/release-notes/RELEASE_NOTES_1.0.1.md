# amm 1.0.1 Release Notes - 2026-03-31

## Release Summary

amm 1.0.1 is a patch release focused on recall quality, scoring accuracy, and memory creation precision. It fixes an inverted renormalization bug that penalized other signals when embeddings were active, collapses two redundant time-decay signals into one, expands the Bayesian learned ranking to cover 9 of 10 positive signals, and adds recall-side deduplication to prevent near-duplicate results from flooding context windows. A new heuristic fallback floor ensures memory creation continues working when no LLM is configured.

## Highlights

- **Fixed renormalization inversion**: enabling semantic embeddings no longer penalizes lexical, entity, and other scoring signals. Missing semantic weight is now correctly redistributed upward to present signals.
- **Collapsed recency and freshness into one signal**: these used identical decay formulas on near-identical timestamps. The freed weight was redistributed to entity overlap and recency.
- **Expanded learned ranking**: the Bayesian learner now touches 9 of 10 positive signals (up from 6), covering lexical, entity overlap, scope fit, and source trust in addition to the original metadata signals.
- **Recall-side deduplication**: near-identical results are collapsed using cosine similarity (threshold 0.85) with a Jaccard text fallback, preventing duplicate memories from consuming recall slots.
- **Intake quality gates**: configurable minimum confidence and importance thresholds for memory creation, with EventQuality classification wiring to filter candidates sourced from ephemeral or noise events.
- **Heuristic fallback floor**: when no LLM is available, the confidence gate automatically lowers to 0.40 so heuristic-extracted memories (confidence 0.45) can still be created, preventing total memory blackout.
- **Configurable entity hub dampening**: the threshold for hub dampening is now tunable via `AMM_ENTITY_HUB_THRESHOLD`, allowing operators to adjust as their knowledge graph grows.
- **Fixed `ResetDerived` FK violation**: claims are now deleted before memories in both PostgreSQL and SQLite adapters, fixing a foreign key constraint violation (`claims_memory_id_fkey`) that prevented derived data purges.

## Deployment and Distribution

- Release binaries: `amm`, `amm-mcp`, `amm-http`
- Docker image: `ghcr.io/bonztm/agent-memory-manager`
- Helm chart path: `deploy/helm/amm`

```bash
docker pull ghcr.io/bonztm/agent-memory-manager:1.0.1
helm upgrade --install amm ./deploy/helm/amm --set image.tag=1.0.1
```

## Breaking Changes

- The `freshness` signal has been removed from the scoring breakdown. Clients that parse the `explain` response should no longer expect a `freshness` key in the signal map. The signal's contribution has been folded into `recency`.
- Default scoring weights have changed. Any persisted learned ranking weights (from `update_ranking_weights` jobs) will be normalized against the new weight distribution on next load.

## New Configuration

| Environment Variable | Default | Description |
|---|---|---|
| `AMM_MIN_CONFIDENCE_FOR_CREATION` | `0.50` | Minimum confidence for memory creation (0.0-1.0) |
| `AMM_MIN_IMPORTANCE_FOR_CREATION` | `0.30` | Minimum importance for memory creation (0.0-1.0) |
| `AMM_ENTITY_HUB_THRESHOLD` | `10` | Entity link count before hub dampening activates |

## Known Issues

- The built-in ONNX embedding provider still uses a hash-based stub for vector generation. It produces consistent vectors for identical strings but does not capture semantic meaning. Production deployments should use an external embedding API endpoint until real ONNX inference is integrated.
- PostgreSQL ANN vector search requires manual operator setup (column and HNSW index creation) after the extension is enabled by migration 3.

## Compatibility and Migration

This release is backwards-compatible with 1.0.0 data. SQLite and PostgreSQL migrations run automatically on startup. No manual migration steps are required.

Operators using the `explain` recall mode should update any downstream parsing to account for the removed `freshness` signal key.

## Full Changelog

See [CHANGELOG.md](../../CHANGELOG.md) for the complete list of changes.
