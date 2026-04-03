# Changelog

All notable changes to amm are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- **Add 60s rate-limit cooldown to CompressHistory.** Prevents expensive plan-building on every cron tick when events constantly accumulate above the minimum threshold. Uses `lastCompletedJobTime` to skip if a compress job completed within the last 60 seconds.
- **Scope latestLeafSummaryBody by project.** The prior leaf summary passed as context during compression is now scoped by project, preventing cross-project context bleed when multiple projects have leaf summaries.
- **Reduce false-positive open loop archival.** `openLoopResolutionKeys` now requires a minimum normalized text length of 12 characters, preventing overly broad matches on short common subjects like "database" or "config."
- **Log warnings for invalid resolved_loops IDs.** `archiveResolvedOpenLoops` now logs `slog.Warn` when the LLM returns a non-existent memory ID or a memory that is not an active open_loop, aiding diagnosis of LLM hallucination.
- **Cache summaryNeedsLLMRetry fallback count.** `currentSummaryFallbackCount` is now called once instead of twice, avoiding redundant metadata parsing.
- **Clamp compress_min_events against compress_max_events.** Prevents misconfiguration where `compress_min_events > compress_max_events` permanently stalls compression by ensuring the minimum never exceeds the query limit.
- **Add scope field to AnalyzeEvents example JSON.** The `buildAnalyzeEventsPrompt` example now includes the `scope` field so LLMs following the example literally will emit scope hints on the primary analysis path, not just the batch extraction path.

### Changed

- **Enforce atomic open_loop extraction.** The memory extraction prompt now requires each `open_loop` to be a single atomic item. Multi-item open loops (numbered lists, bullet lists, or "also"/"additionally" joining unrelated topics) are explicitly called out as defects. Prevents the extraction LLM from aggregating all unresolved items into a single junk-drawer memory.
- **Pass full open_loop context to extraction input.** `buildExtractionInput` now includes the memory ID and full body (capped at 500 chars) for each active open loop from prior sessions, not just the tight description. Gives the extraction LLM enough context to close, update, or avoid re-creating existing open loops.
- **Add LLM scope hints for extracted memories.** The extraction prompt now asks the LLM to suggest `project` or `global` scope for each memory. When the LLM suggests `global` for inherently cross-project types (preference, identity, constraint, procedure), the candidate pipeline promotes from project to global scope. Addresses memories being incorrectly scoped when source events lack `project_id`.
- **Strengthen generic knowledge filter.** The extraction prompt now explicitly skips general programming practices, widely-known tool usage patterns, and standard workflow conventions. Only project-specific twists, gotchas, or non-obvious applications of general practices are extracted.
- **Improve confidence calibration in extraction.** Replaced abstract calibration buckets with concrete examples (user explicitly states → 0.95, assistant concludes → 0.85, one-off choice → 0.75, tool inference → 0.60). Flags batches where every item is 0.9+ as a red flag.
- **Add role-aware extraction weighting.** For preference, constraint, and procedure types, user statements are weighted as stronger evidence than assistant observations. Decision and fact types treat both sources equally.
- **Make topic summary prompt more aggressively abstract.** `buildSummarizeTopicBatchesPrompt` now instructs the LLM to keep only stable facts, active decisions, and unresolved items. Per-session details, timestamps, tool outputs, and intermediate steps are explicitly dropped. Guided by the question: "What would I need to know cold about this topic?"
- **Pass prior leaf summary as context in CompressHistory.** `CompressHistory` now fetches the most recent existing leaf summary and passes it as `PreviousContext` on the first chunk of the first batch. The compression prompt instructs the LLM to not repeat information already captured. Reduces redundancy in consecutive leaf summaries.
- **Add retention tier guidance to lifecycle review.** `buildReviewMemoriesPrompt` now classifies memory types into three retention tiers — durable (preference, constraint, identity, relationship), standard (decision, fact, procedure), and ephemeral (open_loop, assumption, incident) — guiding the LLM to bias promote/decay/archive decisions by tier. Assumptions are now archived when confirmed or refuted.
- **Replace compress_history 24h cooldown with event-count threshold.** `CompressHistory` no longer uses a time-based cooldown gate. Instead it skips when fewer than `compress_min_events` events are pending past the frontier (default: `compress_chunk_size * 5`, i.e. 50 events). Configurable via `compress_min_events` in config.json or `AMM_COMPRESS_MIN_EVENTS` env var.

## [1.3.2] - 2026-04-02

### Added

- **Default tool event ignore policy.** Fresh installs now seed an ingestion policy (`pol_default_tool_events_ignore`) that ignores `tool_*` events by default (kind glob, priority 100). Previously operators had to manually add this policy after installation. Applies to both SQLite (migration 10) and PostgreSQL (migration 4).
- **Embedding-based candidate deduplication fallback.** When text-based duplicate detection finds no matches, `processMemoryCandidates` now falls back to `findDuplicatesByEmbedding` using cosine similarity before inserting a new memory. Prevents near-duplicate memories when surface text differs but semantic content overlaps.
- **Enrichment relationship creation from analysis.** The `Enrich` pipeline now creates relationships from `AnalyzeEvents` relationship candidates (via `createRelationshipsFromAnalysis`), not just entities. Previously only entity linking was wired through the enrichment path.
- **Open loop lifecycle archival.** `LifecycleReview` now deterministically archives open loops that are stale (no access in 30 days) or resolved (a matching decision memory exists with a more recent timestamp). Runs as part of the normal lifecycle review batch alongside LLM-driven promote/decay/merge/archive decisions.
- **Heuristic fallback retry pipeline.** When an LLM-backed pipeline falls back to heuristic extraction, the output is marked as retryable. Subsequent reflect/consolidate passes re-attempt LLM extraction on those events (up to 3 attempts). Tracked via `reflect_fallback_count` event metadata and `fallback_count` memory metadata.
- **LLM fallback WARN logging.** All LLM intelligence operations (`AnalyzeEvents`, `ExtractMemoryCandidateBatch`, `ConsolidateNarrative`, `ReviewMemories`, `CompressEventBatches`, `SummarizeTopicBatches`, `TriageEvents`) now log `slog.Warn` with operation name, fallback type, model, and error when falling back to heuristic processing.

### Fixed

- **Stale metadata cleared on reset-derived.** `ResetDerived` now strips processing metadata (`extraction_method`, `extraction_model`, `fallback_count`, `entities_extracted`, `entities_extraction_method`, `lifecycle_reviewed_at`, `lifecycle_reviewed_model`, `narrative_included`) from manually-created memories that are preserved during the reset. Previously these fields survived the purge and caused stale state on re-extraction.
- **Lower open_loop dedup threshold.** Embedding-based duplicate detection for `open_loop` type memories now uses a lower cosine similarity threshold (0.82 vs 0.85 default), reducing false negatives where semantically similar open loops were created as separate memories.

### Changed

- **Summary-level heuristic retry tracking.** Session summaries produced by heuristic fallback now track their own `fallback_count` in metadata. `summaryNeedsLLMRetry` checks whether a summary was heuristic-produced and under the retry cap, allowing `ConsolidateSessions` to re-attempt LLM narrative generation on subsequent passes.

## [1.3.1] - 2026-04-02

### Fixed

- **LLM reasoning request format.** The `reasoning` parameter is now sent as the correct object format (`{"effort": "high"}` or `{"enabled": true}`) instead of a bare boolean. The previous `"reasoning": true` format caused API errors on OpenRouter and OpenAI endpoints.
- **LLM timeout too short for large context.** The HTTP client timeout for summarizer and review model calls was hardcoded at 30 seconds, causing timeouts on large-context summarization calls (e.g., 900k token sessions). The heuristic fallback produced raw tool output as summaries instead of meaningful narratives.

### Added

- **Configurable LLM timeouts.** `AMM_SUMMARIZER_TIMEOUT_SECONDS` (default 300s/5min) and `AMM_EMBEDDING_TIMEOUT_SECONDS` (default 30s) control HTTP client deadlines for LLM and embedding API calls. Configurable via env vars, JSON, and TOML.
- **Configurable HTTP server timeouts.** `AMM_HTTP_READ_TIMEOUT_SECONDS` (default 30), `AMM_HTTP_WRITE_TIMEOUT_SECONDS` (default 60), `AMM_HTTP_IDLE_TIMEOUT_SECONDS` (default 120) for the amm-http server.
- **Independent reasoning tunables.** `AMM_SUMMARIZER_REASONING` and `AMM_REVIEW_REASONING` (`enabled` or empty) control the `reasoning: {"enabled": true}` toggle independently from `AMM_SUMMARIZER_REASONING_EFFORT` / `AMM_REVIEW_REASONING_EFFORT` (`low`/`medium`/`high`) which sends `reasoning: {"effort": "..."}`. Different models support different subsets; setting an unsupported parameter no longer breaks the request.

### Changed

- **Reasoning effort takes precedence.** When both `reasoning` and `reasoning_effort` are set, only `reasoning_effort` is sent (as `{"effort": "..."}`) since it's more specific than the simple toggle.

## [1.3.0] - 2026-04-02

### Added

- **Session recall mode.** New `--mode sessions` lists and searches session summaries with date filtering. Supports empty-query listing (show recent sessions) and FTS text search scoped to session summaries only.
- **Temporal search.** All recall modes support `--after` and `--before` (RFC3339) for date-range filtering. Natural-language temporal references in queries ("last week", "yesterday", "in March 2025") are automatically extracted and applied when explicit flags are not set.
- **Deterministic temporal parser.** Covers: today, yesterday, earlier/previously/recently, last week, this week, N days/weeks ago, last month, this month, named months with optional year, quarters (Q1-Q4), last year.
- **`SearchScopedSummaries` repository method.** Combines FTS text search with kind/project/session/date filters in SQL before the LIMIT, preventing valid sessions from being crowded out by non-session summaries.
- **Reasoning effort support for LLM calls.** New `AMM_SUMMARIZER_REASONING_EFFORT` and `AMM_REVIEW_REASONING_EFFORT` env vars (low/medium/high). When set, sends `reasoning: {"effort": "..."}` in OpenAI-compatible API requests. Supports reasoning-capable models (o1, o3, o4-mini).
- **`AMM_TEMPORAL_ATTENUATION` env var.** Configurable score multiplier (0.0-1.0, default 0.3) for recall items outside the active temporal window.

### Changed

- **Hooks are capture-only.** All integration hooks (Claude Code, Codex, Hermes, OpenCode) no longer run maintenance jobs on session end. Hooks capture events only. Maintenance (reflect, consolidate_sessions, compress_history, etc.) should run on a schedule via cron/systemd timer or the Helm CronJob.
- **Session summary `CreatedAt` reflects event time.** Session summaries now set `CreatedAt` to the earliest source event timestamp, not the consolidation time. Temporal recall filters sessions by when they happened.
- **Improved consolidation prompt.** `buildConsolidateNarrativePrompt` now requests a human-readable `title` (under 80 chars) and retrieval-optimized `tight_description` (search keywords, under 120 chars) with explicit guidance on their distinct purposes.
- **`NarrativeResult` gains `Title` field.** LLM-generated session titles used for summary `Title` instead of generic "Session \<uuid\>".
- **Hard temporal filtering in scoring.** `scoreAndConvert` hard-filters candidates outside the temporal window before scoring. All scored recall modes (hybrid, facts, ambient, etc.) now exclude out-of-window items rather than just attenuating their scores.
- **Temporal filtering uses occurrence time.** New `occurrenceTimestamp()` function prefers `ObservedAt`/`CreatedAt` over `UpdatedAt` for temporal window filtering. Reprocessed or updated items are filtered by when they originally happened.
- **Inclusive boundary comparisons.** `ListEvents` SQL changed from strict `>` / `<` to inclusive `>=` / `<=` in both SQLite and Postgres, so boundary events (e.g., exactly at midnight) are included.
- **Temporal-only queries route to timeline.** Queries like "yesterday" or "last week" (empty after temporal stripping) route to timeline mode for all FTS-dependent modes, not just hybrid.
- **Over-fetch for temporal recall.** Hybrid (10x) and history (20x) modes over-fetch from FTS when temporal bounds are active to ensure in-window candidates survive past the per-source LIMIT.

### Fixed

- `ValidateRecall` allows empty query for `mode=sessions`.
- MCP adapter forwards `after`/`before` fields in recall validation.
- `latestSessionSummary` selects by `UpdatedAt` (consolidation time) for stable incremental ordering regardless of event timestamps.
- SQLite `normalizeRFC3339ToUTC` ensures consistent text comparison for date-filtered summary queries.
- OpenCode plugin `maintenanceBySession` reference error after maintenance removal (replaced with `lastIdleBySession` idle-event throttle).
- Hermes `on-session-end.sh` exports `SESSION_ID`/`PROJECT_ID` so Python subprocess can read them.
- TOML config parser handles `summarizer.reasoning_effort` and `summarizer.review_reasoning_effort`.

## [1.2.1] - 2026-04-01

### Added

- OpenClaw plugin published as `@bonztm/amm` on npm. Install via `openclaw plugins install @bonztm/amm`.
- `install.sh` one-command local installer for the OpenClaw plugin. Automatically configures: plugin entry, MCP server (local `amm-mcp` or MCP-over-HTTP), and `plugins.allow`. Supports `--api-url`, `--project-id`, `--api-key`, `--recall-limit` options.
- Transport split: npm package uses HTTP-only transport (passes OpenClaw security scanner), `install.sh` provides full dual-transport (local binary + HTTP).
- OpenCode plugin registers native `memory_search`/`memory_get` tools via the `tool` hook.
- npm publish step in release workflow with trusted publishers (OIDC provenance).
- OpenClaw MCP server configuration: `install.sh` configures `mcp.servers.amm` in `openclaw.json` — local `amm-mcp` stdio for binary installs, MCP-over-HTTP (`streamable-http`) for `--api-url` installs.

### Changed

- OpenClaw plugin reverted from memory slot claim to hooks-based integration. The memory slot contract (`MemoryPluginRuntime`) requires plugins that own the full memory lifecycle (storage, embeddings, search managers, flush plans). AMM's Go binary architecture doesn't match — hooks-based integration (`before_prompt_build` + `registerHook`) is stable and fully functional.
- OpenClaw plugin config resolution guards against undefined values.
- OpenClaw `install.sh` handles JSONC trailing commas in `openclaw.json`.

### Fixed

- Postgres `ClaimUnreflectedEvents` test updated for sessionless-only filtering.
- OpenClaw `install.sh` expands `~` to full home path in `AMM_DB_PATH` for MCP env vars.
- OpenClaw MCP server config placed under `mcp.servers` (was incorrectly at top-level `mcp`).

## [1.2.0] - 2026-04-01

### Added

- **Session-first memory extraction pipeline.** ConsolidateSessions is now the primary extraction path for conversation events. Two-pass LLM: narrative summarization (summarizer model) then structured extraction (review model) on the full session narrative. Produces fewer, richer memories with full conversation context instead of per-event fragments.
- Shared `processMemoryCandidates()` function used by both Reflect and ConsolidateSessions for consistent validation, dedup, insertion, and entity linking.
- Idle-timeout trigger for session consolidation. Sessions consolidate after configurable inactivity period (default 15 min, `AMM_SESSION_IDLE_TIMEOUT_MINUTES`). Sessions with an explicit `session_stop` event bypass the idle gate and consolidate immediately.
- Incremental consolidation for resumed sessions. Each idle gap defines an activity burst; prior summary is prepended for context continuity. Multiple consolidation passes per session are supported.
- Map-reduce chunking for large sessions exceeding the summarizer's context window (`AMM_SUMMARIZER_CONTEXT_WINDOW`, default 128k). Sessions are split into overlapping chunks, each summarized independently, then consolidated into a final narrative.
- Entity and relationship extraction from session narratives via `AnalyzeEvents` on the narrative summary.
- Active open loops from prior sessions are passed to the extraction LLM to avoid re-creating resolved loops.
- `source_system` metadata on all memories: `remember` (manual), `reflect`, `consolidate_sessions`, `reprocess`.
- `ResetDerived` preserves manually-created memories (`source_system = "remember"`).
- `amm status` now reports `llm_configured` and `extraction_active` fields.
- `DeleteSummary` and `DeleteEpisode` methods on the Repository interface (SQLite + Postgres).
- `SessionID` field on `ListEpisodesOptions` for session-scoped episode queries.
- CompressHistory 24hr cooldown: skips if last successful compress job was within the configured period.
- Hermes agent `on-session-start.sh` hook for session bookending.
- Full event capture hooks for api-mode claude-code (user, assistant, tool events over HTTP).
- `session_start` event emission in api-mode hooks for claude-code, codex, and opencode.
- **OpenClaw plugin published as `@bonztm/amm` on npm.** Install via `openclaw plugins install @bonztm/amm`. Dual transport: HTTP API for npm installs, local binary for `install.sh` installs.
- **OpenCode plugin registers native `memory_search`/`memory_get` tools** via the `tool` hook, providing direct memory access without requiring the MCP sidecar.

### Changed

- **Reflect narrowed to sessionless events only.** Events with a `session_id` are handled by ConsolidateSessions. `ClaimUnreflectedEvents` (SQLite + Postgres) filters `session_id IS NULL OR session_id = ''` at the SQL level.
- **Model routing corrected.** `ConsolidateNarrative`, `CompressEventBatches`, `SummarizeTopicBatches` now use the summarizer model (large context, cheap). `ExtractMemoryCandidateBatch`, `AnalyzeEvents`, `TriageEvents`, `ReviewMemories` now use the review model (strong instruction following). `LLMIntelligenceProvider` overrides `ExtractMemoryCandidateBatch` to route through the review model.
- Reprocess now handles session events: clears `reflected_at`, deletes stale session summaries and episodes, triggers `ConsolidateSessions` for immediate rebuild.
- `FormEpisodes` removed from default maintenance pipeline (`run-workers.sh`) and session-end hooks. Narrative episodes from ConsolidateSessions are higher quality. Code retained for future cross-session episode detection.
- Claude Code `on-user-message.sh` rewritten to parse stdin JSON instead of `$1` argument. Removes date-based `CLAUDE_SESSION_ID` fallback.
- All claude-code hooks now include `cwd` in event metadata for project_id inference.
- Hermes agent hooks: date-based session ID fallback replaced with stable UUID (`uuidgen` with python3 fallback). `cwd` added to all hook metadata.
- Api-mode codex and opencode examples updated with `cwd` and `project_id` in metadata.

### Removed

- `insertNarrativeMemories()` and `insertNarrativeMemoryIfNotDuplicate()` — replaced by full extraction pipeline on narrative summaries.
- `narrativeMemorySearchQuery()` helper (dead code after removal).

## [1.1.1] - 2026-04-01

### Added

- Automatic project_id inference from `cwd` event metadata at ingestion. When an event carries `cwd` in metadata and no explicit `project_id`, AMM matches against registered project paths and sets the project scope automatically. Enables correct project-scoped memories from global ingestion hooks.
- Helm chart CronJob template (`maintenance.cronjob`) for the full 7-phase maintenance pipeline. Enabled by default on `*/30 * * * *` schedule. Hits the AMM HTTP API from a lightweight busybox container using the chart's own service URL and API key secret. Configurable via `maintenance.cronjob.*` values.

### Changed

- Memory extraction prompt overhauled for higher quality output and better non-frontier model compliance:
  - Prompt restructured into clear sections (FILTERING, BODY QUALITY, TYPE REFERENCE) with filtering rules before extraction guidance, improving instruction adherence on weaker models.
  - Task framing changed from "extract memories" to "evaluate events, return [] unless genuinely durable" — default expectation is now an empty array.
  - Selectivity reinforced at both the top and bottom of the prompt to reduce over-extraction.
  - Hard percentages removed from selectivity guidance for batch-size independence.
  - Body field now requires context and reasoning beyond tight_description; bodies that merely restate tight_description are flagged as defects.
  - Confidence calibration anchors added (0.95 explicit, 0.85 implied, 0.7 inferred, 0.5 speculative) to reduce uniform scoring.
  - All 10 memory types now have concise extraction guidance with body expectations: preference, decision, open_loop, constraint, procedure, incident, assumption, fact, identity, relationship.
  - Decision guidance simplified from rigid template (Decision/Why/Tradeoff) to freeform with reasoning.
  - Open loop guidance now requires describing what is unresolved, why it matters, and what would close the loop.
- `examples/scripts/run-workers.sh` rewritten with structured logging: UTC timestamps on every line, per-job duration and exit status, end-of-run summary with pass/fail counts and total duration. Suitable for both interactive use and cron log capture.

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
- Native OpenClaw plugin (`examples/openclaw/`) for OpenClaw 2026.03.31+. Uses `definePluginEntry()` with `openclaw.plugin.json` manifest. Replaces the previous hook-bundle approach.
- Automatic ambient recall injection for OpenClaw via `before_prompt_build` hook. Relevant memories are prepended to every LLM prompt without the agent needing to call any tool, matching the Hermes plugin's `pre_llm_call` behavior.
- Dual transport support in the OpenClaw plugin: local `amm` binary (default) or HTTP API via `AMM_API_URL`, with MCP-over-HTTP sidecar support for remote deployments.
- Plugin-level `configSchema` for the OpenClaw plugin, allowing configuration through OpenClaw's native plugin config in addition to environment variables.

### Changed

- OpenClaw integration overhauled from a repo-local hook bundle to a native OpenClaw plugin with `openclaw.plugin.json` manifest and `definePluginEntry()` entry point.
- OpenClaw event capture hooks consolidated from standalone `hooks/` directories into plugin-registered hooks within the plugin's `register()` function.
- OpenClaw integration documentation (`docs/openclaw-integration.md`) rewritten for the native plugin architecture.
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

### Removed

- OpenClaw `hooks/amm-memory-capture/` and `hooks/amm-session-maintenance/` standalone hook directories. Event capture is now handled by the native plugin. Session maintenance is external (host cron or systemd).
- OpenClaw `cron.add.reflect.json` cron artifact. The plugin is hot-path only; maintenance scheduling stays external via `examples/scripts/run-workers.sh`.

## [1.0.0] - 2026-03-30

### Added

- Initial 1.0.0 stable release of amm, a Go-based durable memory substrate for AI agents with one service layer exposed consistently through CLI (`amm`), MCP (`amm-mcp`), and HTTP (`amm-http`).
- Dual storage backend support with SQLite as the default local backend and PostgreSQL as the shared high-concurrency backend.
- Durable-memory workflow covering event ingestion, explicit memory writes, multi-mode recall, expand/describe/history queries, projects, relationships, privacy controls, policies, and integrity repair.
- HTTP API and MCP-over-HTTP support for remote, sidecar, and containerized deployments, plus OpenAPI and Swagger documentation.
- Runtime integration guidance and shipped examples for Claude Code, Codex, OpenCode, OpenClaw, Hermes-Agent, API-mode HTTP clients, and Kubernetes sidecar deployments.
- Background maintenance pipeline with reflect, compression, indexing, contradiction detection, graph rebuild, lifecycle review, and related worker jobs.
- Helm chart and sidecar deployment artifacts for Kubernetes-based installations.

[unreleased]: https://github.com/bonztm/agent-memory-manager/compare/1.3.2...HEAD
[1.3.2]: https://github.com/bonztm/agent-memory-manager/releases/tag/1.3.2
[1.3.1]: https://github.com/bonztm/agent-memory-manager/releases/tag/1.3.1
[1.3.0]: https://github.com/bonztm/agent-memory-manager/releases/tag/1.3.0
[1.2.1]: https://github.com/bonztm/agent-memory-manager/releases/tag/1.2.1
[1.2.0]: https://github.com/bonztm/agent-memory-manager/releases/tag/1.2.0
[1.1.1]: https://github.com/bonztm/agent-memory-manager/releases/tag/1.1.1
[1.1.0]: https://github.com/bonztm/agent-memory-manager/releases/tag/1.1.0
[1.0.0]: https://github.com/bonztm/agent-memory-manager/releases/tag/1.0.0
