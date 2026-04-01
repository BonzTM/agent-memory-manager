# [1.2.0] Release Notes - 2026-04-01

## Release Summary

amm 1.2.0 is a minor release that fundamentally changes how memories are extracted from conversations. The new session-first extraction pipeline replaces per-event batch extraction with a two-pass approach: full session narratives are summarized first, then memories are extracted from the narrative with the full conversation arc as context. This produces fewer, richer memories — decisions include the reasoning, open loops include what would close them, and thin "proceed with fixes" fragments are gone.

The release also corrects the model routing so summarization tasks use the cheap large-context model and extraction/reasoning tasks use the strong instruction-following model. The OpenClaw plugin now claims the memory slot, and the OpenCode plugin registers native tools.

## What's New

### Session-first memory extraction

The core pipeline change: conversation events (user messages, assistant messages, tool calls) are no longer extracted individually by Reflect. Instead, ConsolidateSessions groups events by session, generates a narrative summary, then runs the full extraction pipeline on that narrative.

**Why this matters:** Per-event extraction saw fragments without context. "Chose SQLite for local deployment" was just a string — no reasoning, no alternatives considered, no constraints. Session-first extraction sees the full conversation arc: the question, the investigation, the tradeoffs, and the conclusion. The extraction LLM produces memories that include the "why" because it has the full story.

**How it works:**
1. Events are stored as they arrive (no change to hooks or ingestion)
2. ConsolidateSessions runs on the maintenance cycle (every 30 min) or on session-end hook
3. Sessions idle for 15+ minutes are consolidated (configurable via `AMM_SESSION_IDLE_TIMEOUT_MINUTES`)
4. LLM call 1: `ConsolidateNarrative()` produces a session summary (summarizer model — large context, cheap)
5. LLM call 2: `AnalyzeEvents()` or `ExtractMemoryCandidateBatch()` extracts structured memories, entities, and relationships from the narrative (review model — strong instruction following)
6. Results go through the same `processMemoryCandidates()` validation/dedup/insert pipeline as before

Reflect still handles sessionless events (webhooks, API calls, scripts without session context).

### Incremental consolidation for resumed sessions

Walk away from a session for hours, come back — the new pipeline handles it. Each idle gap defines an "activity burst" that gets its own consolidation pass. The prior summary is prepended as context so the extraction LLM maintains narrative continuity without re-processing the entire session history.

### Map-reduce chunking for large sessions

Claude Code sessions can hit 1M tokens. When a session burst exceeds the summarizer's context window, it's split into overlapping chunks, each summarized independently, then consolidated into a final narrative. No data is truncated or lost.

### Model routing correction

The summarizer/review model assignments were backwards. Fixed:

| Task | Model | Why |
|---|---|---|
| ConsolidateNarrative, CompressEventBatches, SummarizeTopicBatches | Summarizer | Compression — large context, cheap |
| ExtractMemoryCandidateBatch, AnalyzeEvents, TriageEvents, ReviewMemories | Review | Reasoning — strong instruction following |

**Breaking change for split-model users:** If you have separate `AMM_SUMMARIZER_*` and `AMM_REVIEW_*` configs, methods have been reassigned to match their names. Single-model deployments are unaffected.

### OpenClaw plugin on npm

The OpenClaw plugin is now published as `@bonztm/amm` on npm. Install via `openclaw plugins install @bonztm/amm`. The npm package uses HTTP transport (requires `amm-http` running). For local binary mode, use `install.sh`. Dual transport: npm installs are HTTP-only (OpenClaw security scanner restriction), local installs support both binary and HTTP.

### OpenCode native tools

The OpenCode plugin now registers `memory_search` and `memory_get` as native tools via the `tool` hook. Agents can search and retrieve memories directly without the MCP sidecar. The MCP sidecar (`amm-mcp`) is still supported for the full 30+ tool suite.

### Manual memory protection

`amm remember` now tags memories with `source_system: "remember"`. `ResetDerived` preserves these memories — only LLM-extracted memories are purged. This means `reset-derived` after an upgrade rebuilds extracted memories from session narratives without losing anything you explicitly told AMM to remember.

### Integration hook fixes

- **Claude Code:** `on-user-message.sh` rewritten to parse stdin JSON (was incorrectly reading `$1`). All hooks now include `cwd` in metadata for automatic project_id inference.
- **Hermes:** Date-based session ID fallback replaced with stable UUID. `cwd` added to all hooks. New `on-session-start.sh` for session bookending.
- **Api-mode:** Full event capture hooks added for claude-code HTTP mode. Session start events added for all api-mode integrations.

## Configuration

| Environment Variable | Default | Description |
|---|---|---|
| `AMM_SESSION_IDLE_TIMEOUT_MINUTES` | `15` | Minutes of inactivity before session consolidation triggers. Set to `0` for immediate processing. |
| `AMM_SUMMARIZER_CONTEXT_WINDOW` | `128000` | Token budget for the summarizer model. Sessions exceeding this are chunked automatically. |

Existing env vars (`AMM_SUMMARIZER_*`, `AMM_REVIEW_*`, etc.) continue to work unchanged.

## Deployment and Distribution

- Release binaries: `amm`, `amm-mcp`, `amm-http`
- Docker image: `ghcr.io/bonztm/agent-memory-manager`
- Helm chart: published at `https://bonztm.github.io/agent-memory-manager`

```bash
docker pull ghcr.io/bonztm/agent-memory-manager:1.2.0
helm repo add amm https://bonztm.github.io/agent-memory-manager
helm upgrade --install amm amm/amm --set image.tag=1.2.0
```

## Breaking Changes

- **Model routing reassignment.** Users with separate `AMM_SUMMARIZER_*` and `AMM_REVIEW_*` configurations will see methods reassigned. `ConsolidateNarrative` now uses the summarizer (was review). `ExtractMemoryCandidateBatch` now uses the review model (was summarizer). Single-model deployments are unaffected.
- **`FormEpisodes` removed from default maintenance pipeline.** Narrative episodes from ConsolidateSessions are higher quality. The code is retained but no longer runs by default. Operators who explicitly depend on `form_episodes` in custom pipelines should verify their setup.
- **Claude Code `on-user-message.sh` no longer reads `$1`.** It now parses stdin JSON like all other hooks. Update any custom wiring that passes the prompt as an argument.

## Compatibility and Migration

**Upgrade path:** Replace the binary. The new pipeline activates immediately for new sessions. Existing unreflected session events will be picked up by ConsolidateSessions on the next maintenance run.

**To rebuild existing memories with the new pipeline quality:**

```bash
amm reset-derived
```

This clears all LLM-extracted memories, summaries, episodes, and jobs, then resets `reflected_at` on all events. The next maintenance run re-processes everything through the session-first pipeline. Manually-created memories (`amm remember`) are preserved.

**Pre-upgrade manual memories:** If you have `amm remember` memories created before 1.2.0, they lack the new `source_system` tag and will be deleted by `reset-derived`. To preserve them, re-create them with `amm remember` after upgrading (which sets the tag), or skip `reset-derived` and let only new sessions use the improved pipeline.

**No schema migrations required.** The changes are service-layer only.

## Full Changelog

- Release tag: https://github.com/bonztm/agent-memory-manager/releases/tag/1.2.0
- Full changelog: https://github.com/bonztm/agent-memory-manager/blob/main/CHANGELOG.md
