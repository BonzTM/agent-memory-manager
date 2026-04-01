# Changelog

All notable changes to amm are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.1.0] - 2026-03-31

### Added

- Query intent routing: hybrid recall mode now auto-routes to specialized modes (contradictions, entity) when query intent is clear. Ambient mode is never re-routed. Routing is heuristic-based with no LLM call overhead. `RecallMeta.RoutedFrom` reports the original mode when routing occurs.
- Temporal staleness scoring: memories containing relative-time language ("currently", "today", "this sprint", etc.) now receive a scoring penalty that ramps from 0 to 0.3 over 180 days after a 14-day freshness threshold. Applied as a modifier to the existing TemporalValidity signal.
- Contradiction surfacing in recall: recalled memories that are referenced in active contradiction memories now include a `conflicts_with` field listing the IDs of conflicting memories. Works across all recall modes. Visibility-gated: conflicting IDs are only exposed when the caller can see the referenced memory.
- Intake quality gates: configurable minimum confidence and importance thresholds for memory creation, preventing low-quality candidates from becoming durable memories. Tunable via `AMM_MIN_CONFIDENCE_FOR_CREATION` and `AMM_MIN_IMPORTANCE_FOR_CREATION` environment variables.
- EventQuality classification wiring: the reflect pipeline now consumes LLM event quality assessments (durable/ephemeral/noise) to filter candidates sourced entirely from low-quality events.
- Built-in ONNX embedding provider: a `builtin_embeddings` build tag enables a local embedding provider that auto-enables embeddings without requiring an external API endpoint. The standard binary is unaffected.
- PostgreSQL ANN vector search: migration 3 attempts to enable a vector extension (vectors/vectorchord/vchord) for approximate nearest neighbor search. Column and index creation is deferred to operator setup.
- Recall-side deduplication: near-duplicate results are now collapsed at recall time using cosine similarity (threshold 0.85) with a Jaccard text fallback when embeddings are unavailable.
- Configurable entity hub dampening threshold via `AMM_ENTITY_HUB_THRESHOLD` environment variable (default 10). Controls when hub dampening kicks in as the knowledge graph grows.
- Known tech entity extraction: 60+ lowercase technology names (redis, kubernetes, postgres, docker, etc.) are now recognized by the heuristic entity extractor without requiring capitalization.
- Hermes AMM plugin example for Hermes-Agent integration.
- Durability check in LLM extraction prompt: candidates are now assessed for 30-day relevance before creation.


### Changed

- Built-in embedding provider now uses real GloVe word vectors (50d, 100K vocab + tech terms) instead of a hash-based stub. Pure Go, no CGo, no external API. Binary size increases ~5MB when built with `builtin_embeddings` tag.
- Collapsed recency and freshness signals into a single recency signal. The former freshness weight (4%) was redistributed: +2% to entity overlap (18% to 20%) and +2% to recency (6% to 8%).
- Fixed renormalization inversion: when semantic embeddings are available, other signals are no longer penalized. When embeddings are absent, their weight is now correctly redistributed upward to present signals.
- Expanded Bayesian learned ranking to cover 9 of 10 positive signals (was 6). Lexical, entity overlap, scope fit, and source trust weights are now learnable from user feedback. Only semantic remains hardcoded.
- Scoring weights no longer use a pre-normalization factor. Raw weights now sum to 1.0 directly, with dynamic renormalization handling the optional semantic signal at score time.
- Heuristic extraction now requires 2+ phrase cue group matches (was 1) and assigns confidence 0.45 (was 0.6) to reduce false positives.
- Heuristic fallback floor: when no LLM summarizer is configured and the operator hasn't explicitly set a confidence gate, the minimum confidence for creation is automatically lowered to 0.40 so heuristic-extracted memories (confidence 0.45) can still be created.
- Session trace archive TTL reduced from 7 days to 3 days.
- Minimum hybrid history score threshold adjusted from 0.55 to 0.48 to account for the collapsed signal weight distribution.
- Integration documentation updated to recommend disabling tool_call and tool_result event ingestion across all integration points.

### Fixed

- `ResetDerived` FK ordering: claims are now deleted before memories in both PostgreSQL and SQLite adapters, fixing a foreign key constraint violation (`claims_memory_id_fkey`) that prevented derived data purges.
- PostgreSQL array scanning for repository reads (nil slice handling).
- PostgreSQL nil slice writes causing query errors.
- Hermes plugin metadata serialization: metadata is now sent as `map[string]string` with JSON string serialization.
- ANN search now falls back to brute-force when results are empty (not just on error).
- Embedding upsert syncs the `embedding_vec` column when present, with logged warnings on failure instead of silent drops.
- Test assertion ordering: source lookup assertions now run before memory supersede to prevent flaky test results.

## [1.0.0] - 2026-03-30

### Added

- Initial 1.0.0 stable release of amm, a Go-based durable memory substrate for AI agents with one service layer exposed consistently through CLI (`amm`), MCP (`amm-mcp`), and HTTP (`amm-http`).
- Dual storage backend support with SQLite as the default local backend and PostgreSQL as the shared high-concurrency backend.
- Durable-memory workflow covering event ingestion, explicit memory writes, multi-mode recall, expand/describe/history queries, projects, relationships, privacy controls, policies, and integrity repair.
- HTTP API and MCP-over-HTTP support for remote, sidecar, and containerized deployments, plus OpenAPI and Swagger documentation.
- Runtime integration guidance and shipped examples for Claude Code, Codex, OpenCode, OpenClaw, Hermes-Agent, API-mode HTTP clients, and Kubernetes sidecar deployments.
- Background maintenance pipeline with reflect, compression, indexing, contradiction detection, graph rebuild, lifecycle review, and related worker jobs.
- Helm chart and sidecar deployment artifacts for Kubernetes-based installations.

[unreleased]: https://github.com/bonztm/agent-memory-manager/compare/1.1.0...HEAD
[1.1.0]: https://github.com/bonztm/agent-memory-manager/releases/tag/1.1.0
[1.0.0]: https://github.com/bonztm/agent-memory-manager/releases/tag/1.0.0
