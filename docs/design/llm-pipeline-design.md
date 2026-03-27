# LLM Injection Points in the AMM Pipeline

**Status**: Option B (Single-Point Model) implemented as first step  
**Created**: 2026-03-25  
**Updated**: 2026-03-26  
**Context**: The pipeline now uses LLM involvement at the extraction point with improved prompt quality, noise filter relaxation, and LLM-generated summary descriptions. Further injection points (triage, quality review) are deferred pending evaluation of Option B's impact.

## The Core Question

Where in the AMM pipeline does LLM involvement produce meaningfully better outcomes than heuristics alone, and can we consolidate those points to minimize API cost?

## Current Pipeline Stages (Heuristic-Only)

### 1. Ingestion Noise Filtering
**What it does**: Conservatively downgrades "noisy" events to `read_only`, causing reflect/reprocess to skip them entirely.

**Problem**: Heuristics can't distinguish "useless verbose output" from "verbose output containing a lesson." Examples of potentially valuable content that gets dropped:
- Grep results revealing unexpected code patterns
- Test failures exposing constraints or gotchas
- Build errors revealing dependency issues
- Tool output showing system behavior

**Current risk**: Useful signal silently dropped before LLM extraction ever sees it.

### 2. Memory Extraction (reflect / reprocess)
**What it does**: Extracts durable memories from events. Has both heuristic and LLM paths — LLM path produces much better results but costs per-event.

**Problem**: When LLM is available, extraction quality is good. When it's not, heuristic extraction is crude (keyword matching). The bigger issue is that extraction runs on events that *survive* noise filtering — so the LLM never sees filtered events.

### 3. Decay (`decay_stale_memory`)
**What it does**: Reduces importance of old, unaccessed memories. Heuristic-only (age + access frequency).

**Problem**: A memory's value isn't just recency. "We tried X and it failed catastrophically" is permanently valuable regardless of age. Heuristic decay can't assess semantic durability.

### 4. Summary Generation (compress_history / consolidate_sessions)
**What it does**: Builds summaries over event spans and sessions. Already has LLM path via Summarizer interface.

**Status**: This one already works well with LLM when available.

## Potential Consolidated Injection Points

### Option A: Two-Point Model (Recommended starting point)

**Point 1: Smart Triage at Ingestion**  
Instead of binary "full / read_only / ignore", add an LLM triage step for events that heuristics flag as borderline. The LLM answers: "Does this event contain durable knowledge worth extracting? If yes, what kind?"

This collapses noise filtering + extraction guidance into one LLM call. Events clearly noisy by heuristics skip the LLM entirely. Events clearly valuable skip it too. Only the "maybe" bucket gets triaged.

**Point 2: Periodic Quality Review**  
A batch job that reviews N oldest/least-accessed memories and assesses: "Is this still valuable? Should it be archived or merged with another memory?" This collapses decay + quality assessment into one periodic LLM call.

**Cost model**: Point 1 fires per-borderline-event (amortized by heuristic pre-filter). Point 2 fires as a batch job on a schedule. Neither is per-event for all events.

### Option B: Single-Point Model (Cheapest)

**One LLM call during extraction only.** Enhance the extraction prompt to also output:
- Noise assessment ("is this event worth remembering?")
- Quality tags ("durable", "ephemeral", "context-dependent")
- Suggested importance/confidence overrides

Then downstream jobs (decay) use these LLM-assigned tags instead of heuristics. Cost: one LLM call per event during reflect/reprocess, but no additional LLM calls elsewhere.

**Downside**: No LLM involvement in lifecycle management. Tags assigned at extraction time become stale.

### Option C: Three-Point Model (Most Thorough)

1. Ingestion triage (as Option A Point 1)
2. Extraction (already exists)
3. Periodic lifecycle review (as Option A Point 2)

**Downside**: Highest cost. Probably overkill unless the memory store is very large.

## Design Constraints

- **Cost sensitivity**: LLM calls cost money. Every injection point needs to justify its value.
- **Offline-capable**: AMM must work without LLM (heuristic fallback). LLM enrichment is additive, not required.
- **Existing interface**: `core.Summarizer` already abstracts LLM/heuristic choice. New injection points should follow the same pattern.
- **Batch-friendly**: Prefer batched LLM calls over per-event calls where possible.

## Open Questions

1. What's the actual false-negative rate of the current noise filter? How many events are being downgraded that contain useful signal? (Could sample and audit.)
2. Is the decay job actually harming recall quality, or is it working fine with heuristics? (Need usage data.)
3. What's the cost budget for LLM calls in the pipeline? This determines whether Option A or B is viable.
4. Should the LLM triage at ingestion be synchronous (blocking ingest) or async (tag for later processing)?
5. Can we reuse the existing `Summarizer` interface or do we need a new `Triager` / `Assessor` interface?

## Implemented (Option B — Single-Point Model)

### Extraction Prompt Improvements
- Added percentage-based quantity bar: "at most 10-15% of events should yield a memory"
- Tool output distillation: extract the lesson, not the raw output
- Anti-duplication: skip information already in project docs (README, AGENTS.md)
- Body quality: require self-contained, "why and so what" content
- Better tight_description guidance: natural-language retrieval phrases, no paths/timestamps

### Noise Filter Relaxation
- When LLM summarizer is configured (`hasLLMSummarizer` flag), `read_only` events are passed through to reflect/reprocess instead of being skipped
- The LLM decides what's worth extracting from tool output, build logs, etc.
- When no LLM is configured, conservative heuristic filter is preserved (no regression)

### Summary Tight Descriptions
- CompressHistory and ConsolidateSessions now generate tight_descriptions via the summarizer
- Falls back to timestamp/snippet format when summarizer returns empty or errors
- One additional summarizer call per summary (minimal cost overhead)

## Next Steps (If Option B Is Insufficient)

If the extraction improvements don't sufficiently reduce over-production or improve recall quality:

1. **Option A Point 1: Smart Triage at Ingestion** — LLM triage for borderline events before extraction
2. **Option A Point 2: Periodic Quality Review** — Batch job reviewing oldest/least-accessed memories for archival or promotion

## Related Work

- Ingestion noise filtering: implemented (heuristic + LLM bypass when configured)
- Decay/archive automation: `archive_session_traces` job (heuristic-only, potential future LLM enrichment)
- LLM extraction: `LLMSummarizer` in `internal/service/llm_summarizer.go` (prompt improved)
- Memory dedup: pre-insert dedup in Remember, Reflect, and scaled MergeDuplicates
- Embeddings: API-based provider implemented, separate config from summarizer
