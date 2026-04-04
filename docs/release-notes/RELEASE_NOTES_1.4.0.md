# [1.4.0] Release Notes - 2026-04-04

## Release Summary

amm 1.4.0 is a quality-focused release that significantly tightens the memory extraction pipeline, adds knowledge graph enrichment, and expands both the Hermes and OpenClaw integration surfaces. The main themes are: extraction prompts are overhauled for fewer, higher-quality memories with better scope inference and confidence calibration; a new `build_entity_briefs` enrichment job synthesizes per-entity briefings from linked memories; `Expand` supports recursive multi-level traversal via `max_depth`; the compression pipeline replaces its 24h time-based cooldown with a responsive event-count threshold; Hermes gains a first-class memory-provider example; and both Hermes and OpenClaw plugins gain curated memory mirroring and two-tier memory system prompt guidance.

## Added

- **Recursive multi-level Expand with `max_depth` parameter.** `Expand()` now accepts `max_depth` (0–5) to recursively expand child summaries. At `max_depth=0` (default), behavior is unchanged. At `max_depth=N`, each child summary is itself expanded with `max_depth=N-1`, populating a new `expanded_children` field on `ExpandResult`. Wired through CLI (`--max-depth`), MCP (`max_depth`), and HTTP (`max_depth` query param). Enables single-call traversal from topic summaries through session summaries down to leaf summaries and raw events.

- **Entity synthesis briefings as enrichment job.** New `build_entity_briefs` job generates per-entity synthesis summaries for entities with 3+ linked memories. Gathers all linked memories, produces a coherent briefing via LLM (current state, key decisions, relationships, open questions), and stores as a summary with `Kind: "entity_brief"`. Incremental: skips entities whose brief is already up to date (no new linked memories since the brief was last generated). Tracks extraction metadata (`extraction_method`, `extraction_quality`, `fallback_count`) so heuristic fallback briefs are retried when LLM becomes available. Entity recall now surfaces brief descriptions for richer context. New `ListMemoriesByEntityID` repository method added to both SQLite and Postgres adapters.

- **Hermes external memory-provider example.** New `examples/hermes-agent/memory/amm/` implements the Hermes `memory.provider` interface with ambient recall injection, per-turn event sync, and curated-memory mirroring. Recommended for newer Hermes builds that support `memory.provider: amm` in `config.yaml`.

- **Optional Hermes curated-memory parity in the legacy hook plugin.** `examples/hermes-agent/amm-legacy` (renamed from `amm-memory`) can now mirror successful Hermes `memory` tool writes into AMM durable memories via `post_tool_call`, with env-driven scope/type configuration plus a local AMM-ID map and retry queue for update/delete targeting. Retained as fallback for older Hermes builds.

- **Optional OpenClaw curated-memory mirroring.** The OpenClaw native plugin can now mirror MEMORY.md/USER.md writes to AMM durable memories via `agent_end` diffing. Snapshots curated files at session start, diffs after each turn, and mirrors adds/removes/replacements with in-place PATCH for replacements. Configurable via `syncCuratedMemory`, `curatedProjectId`, scope/type settings, and `stateDir`.

- **Two-tier memory system prompt for Hermes provider and OpenClaw.** The Hermes memory-provider example and OpenClaw plugin now inject system prompt guidance teaching the agent to use built-in memory (MEMORY.md/USER.md) as a lean scratchpad and AMM (via MCP tools or CLI) as unlimited long-term storage, with `amm_expand` / `amm expand` with `max_depth` for deeper context on thin recall items. The Hermes legacy hook plugin does not inject system prompt guidance (use the memory-provider example for this feature).

## Fixed

- **Add 60s rate-limit cooldown to CompressHistory.** Prevents expensive plan-building on every cron tick when events constantly accumulate above the minimum threshold. Uses `lastCompletedJobTime` to skip if a compress job completed within the last 60 seconds.

- **Scope latestLeafSummaryBody by project.** The prior leaf summary passed as context during compression is now scoped by project, preventing cross-project context bleed when multiple projects have leaf summaries.

- **Reduce false-positive open loop archival.** `openLoopResolutionKeys` now requires a minimum normalized text length of 12 characters, preventing overly broad matches on short common subjects like "database" or "config."

- **Log warnings for invalid resolved_loops IDs.** `archiveResolvedOpenLoops` now logs `slog.Warn` when the LLM returns a non-existent memory ID or a memory that is not an active open_loop, aiding diagnosis of LLM hallucination.

- **Cache summaryNeedsLLMRetry fallback count.** `currentSummaryFallbackCount` is now called once instead of twice, avoiding redundant metadata parsing.

- **Cap confidence at 0.90 for narrative-sourced memories.** Memories extracted via the two-hop pipeline (events → narrative → extraction) now have confidence capped at 0.90, acknowledging inherent information loss in the summarization step.

- **Wire heuristic ConsolidateNarrative through escalation.** `HeuristicIntelligenceProvider.ConsolidateNarrative` now uses `summarizeWithEscalation` (normal → aggressive → deterministic truncation) instead of raw `Summarize` (plain truncation), improving fallback quality when no LLM is available.

- **Add structured importance signals to narrative output.** `NarrativeResult.KeyDecisions` and `Unresolved` are now structured types (`NarrativeDecision`, `NarrativeUnresolved`) with `importance` (high/medium/low), `source` (for decisions), and `blocking` (for unresolved items) fields. The narrative prompt requests structured objects and `buildExtractionInput` formats the importance/source/blocking metadata for the extraction LLM. Backward compatible: the custom `UnmarshalJSON` accepts both plain strings and structured objects from the LLM.

- **Extend intent classification for open loops and decisions.** `classifyRecallIntent` now routes "what's pending/open/unresolved" queries to facts mode and "why did we decide/what was decided" queries to episodes mode, in addition to the existing contradiction and entity routing.

- **Clamp compress_min_events against compress_max_events.** Prevents misconfiguration where `compress_min_events > compress_max_events` permanently stalls compression by ensuring the minimum never exceeds the query limit.

- **Add scope field to AnalyzeEvents example JSON.** The `buildAnalyzeEventsPrompt` example now includes the `scope` field so LLMs following the example literally will emit scope hints on the primary analysis path, not just the batch extraction path.

## Changed

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

- **Remove `form_episodes` from default maintenance pipeline.** `form_episodes` is no longer included in Phase 4 of `run-workers.sh` or the Helm CronJob. Narrative episodes from `ConsolidateSessions` are higher quality (as noted in 1.2.0). The job kind still exists for custom pipelines.

- **Legacy Hermes plugin renamed.** `examples/hermes-agent/amm-memory/` renamed to `examples/hermes-agent/amm-legacy/`. The legacy hook plugin is retained as fallback for older Hermes builds that don't support the external memory-provider architecture.

## Admin/Operations

- New `build_entity_briefs` job available via `amm jobs-run build_entity_briefs` (CLI), MCP, and HTTP. Runs incrementally — safe to schedule alongside existing maintenance jobs. Already wired into `run-workers.sh` (Phase 4) and the Helm CronJob template.
- `compress_min_events` is a new configuration option (config.json or `AMM_COMPRESS_MIN_EVENTS` env var, default: `compress_chunk_size * 5`). If you previously relied on the 24h cooldown, compression will now trigger more responsively based on pending event count.
- Extraction prompts have been significantly tightened. Expect fewer memories per session but with higher quality, better scope inference, and more accurate confidence scores. No configuration changes needed.
- Hermes users on newer builds should migrate from the legacy hook plugin (`amm-legacy/`) to the memory-provider example (`memory/amm/`). Set `memory.provider: amm` in your Hermes `config.yaml`. The legacy plugin continues to work but is no longer the recommended path.
- OpenClaw users can enable curated-memory mirroring by setting `syncCuratedMemory: true` in plugin config or `AMM_OPENCLAW_SYNC_CURATED_MEMORY=true`. Scopes and memory types are configurable via `memoryScope`, `userScope`, `memoryType`, `userType`. Both Hermes and OpenClaw plugins now automatically inject two-tier memory guidance into the system prompt.

## Deployment and Distribution

- Release binaries: `amm`, `amm-mcp`, `amm-http`
- Docker image: `ghcr.io/bonztm/agent-memory-manager`
- Helm chart path: `deploy/helm/amm`

```bash
docker pull ghcr.io/bonztm/agent-memory-manager:1.4.0
helm upgrade --install amm ./deploy/helm/amm --set image.tag=1.4.0
```

## Breaking Changes

None.

## Known Issues

None.

## Compatibility and Migration

No manual migration required. No new database migrations in this release. All changes are backward compatible:
- `max_depth` defaults to 0 (no recursion), preserving existing `Expand` behavior.
- `NarrativeDecision` and `NarrativeUnresolved` accept both plain strings and structured objects via custom `UnmarshalJSON`.
- `MemoryCandidate.Scope` is optional — extraction works identically when the LLM omits it.
- `build_entity_briefs` is additive — it creates new summaries without modifying existing data.

## Full Changelog

- Release tag: https://github.com/bonztm/agent-memory-manager/releases/tag/1.4.0
- Full changelog: https://github.com/bonztm/agent-memory-manager/blob/main/CHANGELOG.md
**Full Changelog**: https://github.com/BonzTM/agent-memory-manager/compare/1.3.2...1.4.0
