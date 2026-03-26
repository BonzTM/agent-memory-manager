# AMM End-Game Plan: From Substrate to Brain

## The "Substrate vs Brain" Question

The original spec says AMM is "a substrate, not a brain." Here's why that boundary should move:

**Reasons the substrate-only framing made sense at v0:**
1. Avoid scope creep before the foundation was solid
2. Keep AMM framework-agnostic (brains are opinionated, substrates aren't)
3. Minimize external dependencies (LLMs cost money, add latency, break offline)
4. Make the "no API key" experience work at all

**Reasons to become a brain now:**
1. **The substrate is solid.** 16 types, 9 recall modes, 10-signal scoring, FTS5+embeddings, full provenance chain, maintenance jobs, repair tools. The foundation is built.
2. **The heuristic gap is too wide.** As documented in our analysis, heuristic-extracted memories actively pollute the recall space. The "works offline" guarantee matters less than "works well when online."
3. **Intelligence is the differentiator.** Any project can wrap SQLite with FTS5. What makes AMM valuable is the pipeline that turns raw history into *understanding*. That pipeline needs LLM intelligence at more points than just extraction.
4. **The architecture already supports it.** The `Summarizer` interface pattern, the embedding provider abstraction, the `extraction_method` metadata tag — AMM was *designed* to have intelligence plugged in. The substrate-only framing was a phasing decision, not an architectural one.
5. **Agents need a smart memory system, not a dumb filing cabinet.** The users of AMM are agents. Agents don't want to do memory management themselves — they want a memory system that *thinks* about what to remember, how to organize it, and what's relevant.

**The constraint to preserve:** AMM must always *function* without LLM (offline/free mode). But the *default recommended path* should be LLM-enhanced, and the system should make clear that heuristic mode is degraded.

---

## Design Principles

### Consolidated LLM Calls

Every new LLM integration must justify itself against the cost budget. The strategy:

1. **Batch ruthlessly.** Never make per-item LLM calls when batching is possible.
2. **Piggyback on existing calls.** If we're already calling the LLM for extraction, have it also output entity types, relationship hints, and quality tags — one prompt, multiple outputs.
3. **Tier the work.** Hot-path (recall-time) gets zero new LLM calls. Warm-path (ingestion-time) gets at most 1 call per batch. Cold-path (maintenance jobs) gets batched periodic calls.
4. **Cache aggressively.** LLM outputs should be stored as metadata so the same analysis never runs twice.

### Configurable Batch Sizes (No Hardcoded Counts)

All batch sizes must have sane defaults but be user-configurable via environment variables or config. Users with generous API budgets can lower batch sizes for faster processing; cost-conscious users can raise them.

Defaults should optimize for the common case: frequent small reflects (1-5 events from OpenCode idle hooks) and periodic larger reprocesses.

| Parameter | Default | Env Var | Notes |
|-----------|---------|---------|-------|
| Reflect batch claim | 100 | `AMM_REFLECT_BATCH_SIZE` | Max events claimed per reflect run |
| Reflect LLM sub-batch | 20 | `AMM_REFLECT_LLM_BATCH_SIZE` | Events sent per LLM call within reflect |
| Reprocess LLM batch | 20 | `AMM_REPROCESS_BATCH_SIZE` | Already configurable via `SetReprocessBatchSize` |
| Lifecycle review batch | 50 | `AMM_LIFECYCLE_REVIEW_BATCH_SIZE` | Memories per LLM review call |
| Cross-project transfer batch | 30 | `AMM_CROSS_PROJECT_BATCH_SIZE` | Memories per transfer assessment call |

### Model Routing (Optional, User-Controlled)

Not everything needs the same model. The system should support configurable model routing:

| Task Class | Default Model Config | Reasoning Needed |
|-----------|---------------------|-----------------|
| Extraction (reflect/reprocess) | `AMM_SUMMARIZER_MODEL` (existing) | Low — pattern matching, classification |
| Summarization (compress/consolidate) | Same as extraction (default) | Low — text compression |
| Lifecycle review | Same as extraction (default) | Medium — judgment calls on memory value |
| Ingestion triage | Same as extraction (default) | Low — binary classification |

By default, all LLM work goes through the single configured `AMM_SUMMARIZER_*` endpoint/model. Users who want can optionally set `AMM_REVIEW_MODEL` / `AMM_REVIEW_ENDPOINT` to use a stronger (or cheaper) model for lifecycle review specifically. But this is NOT required — single-model works fine for everything.

### Processing Ledger (Track Everything)

**Every processing step applied to a memory must be tracked in metadata.** This prevents re-processing, enables targeted upgrades, and provides a complete processing history.

Current state: only `extraction_method` ("llm" or "heuristic") and `reflected_at` on events. That's not enough.

**New metadata fields on memories (stored in existing `metadata_json`, no migration):**

| Field | Set When | Values | Purpose |
|-------|----------|--------|---------|
| `extraction_method` | reflect/reprocess | `"llm"`, `"heuristic"` | Already exists |
| `extraction_quality` | reflect/reprocess/remember | `"verified"`, `"provisional"`, `"upgraded"` | Phase 1A (new) |
| `extracted_at` | reflect/reprocess | ISO 8601 timestamp | When extraction happened |
| `extracted_model` | reflect/reprocess (LLM only) | Model ID string | Which model extracted this |
| `embedded_at` | embedding upsert | ISO 8601 timestamp | When embedding was generated |
| `embedded_model` | embedding upsert | Model ID string | Which embedding model was used |
| `entities_extracted` | reflect/reprocess | `"true"` | Whether entity extraction ran for this memory |
| `entities_extracted_method` | reflect/reprocess | `"llm"`, `"heuristic"` | How entities were extracted |
| `claims_extracted` | extract_claims job | `"true"` | Whether claims were extracted |
| `lifecycle_reviewed_at` | lifecycle_review job | ISO 8601 timestamp | Last lifecycle review |
| `lifecycle_reviewed_model` | lifecycle_review job (LLM) | Model ID string | Which model reviewed |
| `narrative_included` | consolidate_sessions | `"true"` | Whether this memory's source events were included in a session narrative |

**Decision rule for any processing step:** Before running LLM on a memory/event, check metadata: "Has this exact step already been done by LLM (or at all)?" If yes, skip. If done by heuristic but LLM is now available, eligible for upgrade via reprocess.

**Decision rule for heuristic fallback:** When LLM fails or is unconfigured, always set `*_method: "heuristic"` so reprocess knows to revisit later.

### Remember Stays Manual, Gets Post-Processing

Explicit `amm remember` calls remain as-is — when an agent or user explicitly wants to store a memory, that intent is sacred and should not be gated by LLM triage or extraction. However, explicitly remembered memories benefit from **asynchronous LLM post-processing**:

1. Memory is immediately stored with `extraction_quality: "verified"` (agent-authored = high trust)
2. A lightweight background enrichment pass runs when the next maintenance cycle fires:
   - **Entity extraction**: identify and link entities mentioned in the memory
   - **Claim extraction**: extract structured claims from the memory body
   - **Relationship inference**: if the memory mentions known entities, create relationship edges
   - **Embedding generation**: generate and store embedding (already happens via best-effort upsert)
3. This enrichment is tracked: `entities_extracted: "true"`, `claims_extracted: "true"`, etc.
4. The memory is never re-extracted or modified — only enriched with linked data

This means explicitly remembered memories skip the event → reflect → memory pipeline but still get the graph/claim/entity benefits of LLM processing.

### Current LLM Call Map (what exists today)
| When | Call | Count |
|------|------|-------|
| `reflect` | `ExtractMemoryCandidate` per event | 1 per event |
| `reprocess` | `ExtractMemoryCandidateBatch` per batch of 20 | 1 per 20 events |
| `compress_history` | `Summarize` per chunk of 10 events | 2 per chunk (body + tight) |
| `consolidate_sessions` | `Summarize` per session | 2 per session (body + tight) |
| `remember` (explicit) | None | 0 |
| `recall` | None (embedding only) | 0 (1 embed call) |
| Embedding upserts | `Embed` per memory/summary | 1 per item |

### Target LLM Call Map (after this plan)
| When | Call | Count | What's New |
|------|------|-------|------------|
| `reflect` | `AnalyzeEventBatch` (replaces per-event extract) | **1 per batch** (configurable, default 20 events/call) | Batched extraction + entity extraction + relationship hints + quality tags. **Currently N calls → 1 call per sub-batch.** |
| `reprocess` | Same as above | 1 per batch | Unchanged approach, richer output |
| `compress_history` | `Summarize` + `NarrateEpisode` | 2 per chunk | Episode narrative consolidated with compression |
| `consolidate_sessions` | `ConsolidateSession` (one call does summary + episode + narrative) | **1 per session** | Currently 2 calls → 1 call with richer output |
| `lifecycle_review` (NEW) | `ReviewMemoryBatch` | **1 per batch** (configurable, default 50 memories/call) | Replaces heuristic decay + promote + contradiction detection |
| `enrich_memories` (NEW) | Piggybacked on reflect or standalone | **0 extra** if piggybacked; 1 per batch if standalone | Entity/claim/relationship extraction for memories that bypassed reflect (explicit remember) |
| `recall` | None | 0 | Still zero LLM at recall time |
| Embedding upserts | `Embed` (batched) | 1 per batch | Already batched in rebuild |

### Real-World Reflect Pattern

**Important context:** In OpenCode integration, reflect triggers on `session.idle` (with a 60-second throttle). This means reflect frequently runs with just 1-5 unreflected events, not batches of 100. The current per-event `ExtractMemoryCandidate` call means 1-5 LLM calls per idle cycle — which is actually not catastrophic in count, but wasteful because each call has the full prompt overhead.

With batched extraction, a typical reflect cycle with 3 events becomes: **1 LLM call** (the batch of 3) instead of 3 separate calls. The savings scale — during a long session with many tool runs, events accumulate between idle hooks, and a reflect with 15 events goes from 15 calls → 1 call.

**Net change in LLM calls per typical cycle:**
- Reflect: **-N+1 calls** per sub-batch (N events → 1 call)
- Compress: +0 calls (same count, richer output)
- Consolidate: **-1 call** (2 → 1 per session)
- Lifecycle review: **+1 call** per batch of 50 memories (new, but replaces 3 separate heuristic jobs)
- Enrich: **+0 extra** when piggybacked on reflect; occasional standalone batch for explicit-remember memories
- **Total: Significant reduction in call count, significant increase in intelligence per call**

---

## Phase 1: Foundation Hardening (Immediate)

### 1A. Heuristic Memory Provisional Status ✅ COMPLETE

**Goal:** Memories extracted by heuristics are explicitly marked as lower-quality and downweighted in recall.

**Changes:**

1. **New metadata field**: `extraction_quality` with values `"verified"` (LLM-extracted or explicit `remember`), `"provisional"` (heuristic-extracted), `"upgraded"` (was heuristic, re-extracted by LLM)

2. **Scoring adjustment in `scoring.go`**: Add an `extractionQuality` signal that penalizes provisional memories:
   - `verified` or absent → 1.0
   - `upgraded` → 0.9
   - `provisional` → 0.5
   Weight: 0.08 (take from lexical's current over-allocation)

3. **Remember path**: Explicit `amm remember` calls get `extraction_quality: "verified"` automatically (the agent decided this was worth remembering — that's a quality signal)

4. **Reflect path**: Heuristic-extracted memories get `extraction_quality: "provisional"`. LLM-extracted get `extraction_quality: "verified"`.

5. **Reprocess path**: When reprocess upgrades a heuristic memory, set `extraction_quality: "upgraded"`.

**Files changed:**
- `internal/service/scoring.go` — new signal + weight rebalance
- `internal/service/reflect.go` — set extraction_quality
- `internal/service/reprocess.go` — set extraction_quality on upgrade
- `internal/service/service.go` (Remember) — set extraction_quality: verified
- `internal/service/memory_candidates.go` — ScoringCandidate gets ExtractionQuality field

**No schema migration needed** — extraction_quality stores in existing `metadata_json`.

**QA:**
- `CGO_ENABLED=1 go test -tags fts5 ./internal/service/... -count=1` — all existing tests pass
- New test: `TestRemember_SetsVerifiedExtractionQuality` — explicit remember → verify metadata `extraction_quality` = "verified"
- New test: `TestReflect_SetsProvisionalWhenHeuristic` — reflect without LLM → verify metadata `extraction_quality` = "provisional"
- New test: `TestReflect_SetsVerifiedWhenLLM` — reflect with LLM → verify metadata `extraction_quality` = "verified"
- New test: `TestReprocess_SetsUpgradedQuality` — heuristic memory re-extracted by LLM → verify `extraction_quality` = "upgraded"
- New test: `TestScoring_PenalizesProvisionalMemories` — two identical memories, one verified one provisional, verify verified scores higher
- CLI: `amm remember --type fact --body "test" --tight "test" && amm recall "test" --explain` → verify extraction_quality signal appears in explain output

### 1B. Batched Reflect ✅ COMPLETE

**Goal:** Reflect should use `ExtractMemoryCandidateBatch` instead of per-event `ExtractMemoryCandidate`.

**Current problem:** `reflect.go` line 44 calls `ExtractMemoryCandidate` **per event** in a loop. When the LLM is configured, this is 1 API call per event. The batch interface already exists and `reprocess` already uses it.

**Real-world context:** In OpenCode, reflect triggers on `session.idle` with a 60-second throttle (see `examples/opencode/plugins/amm.js:180-198`). Typical reflect runs process 1-5 events (not 100). With per-event extraction, that's 1-5 LLM calls per idle cycle. With batching, it's always 1 call regardless of event count. During longer sessions or cron-triggered runs, batches can be larger (10-50 events), where savings are more dramatic.

**Changes:**

1. **`reflect.go`**: Refactor the event loop to collect events into sub-batches and call `ExtractMemoryCandidateBatch`. Sub-batch size is configurable via `AMM_REFLECT_LLM_BATCH_SIZE` (default: 20). The outer claim size (`reflectBatchSize=100`) stays as the max events claimed per reflect run, also configurable via `AMM_REFLECT_BATCH_SIZE`.

2. **Source event tracking**: Use `SourceEventNums` from batch extraction to link candidates back to specific events (same pattern as `reprocess.go`, already implemented in `MemoryCandidate.SourceEventNums`).

3. **Small batch optimization**: When the batch contains only 1 event, the system can still use `ExtractMemoryCandidateBatch` with a single-item array — the LLM prompt handles this fine, and it keeps the code path uniform.

**Files changed:**
- `internal/service/reflect.go` — refactor to batch extraction
- `internal/service/service.go` — add configurable reflect LLM batch size
- `internal/runtime/config.go` — read `AMM_REFLECT_LLM_BATCH_SIZE` and `AMM_REFLECT_BATCH_SIZE`

**QA:**
- `CGO_ENABLED=1 go test -tags fts5 ./internal/service/... -count=1` — all existing reflect tests pass (TestReflect_ExtractsPreferences, TestReflect_SkipsDuplicates, etc.)
- New test: `TestReflect_UsesBatchExtraction` — insert 5 events, reflect, verify summarizer received one batch call (not 5 individual calls). Use a recording summarizer stub.
- New test: `TestReflect_BatchSourceEventTracking` — verify memories created from batch extraction have correct `source_event_ids` linking to the right events
- New test: `TestReflect_SingleEvent_StillBatched` — insert 1 event, reflect, verify single batch call (not per-event call)
- New test: `TestReflect_ConfigurableBatchSize` — set batch size to 3, insert 7 events, verify 3 batch calls (3+3+1)
- Manual: with LLM configured, ingest 20 events, run reflect, count LLM API calls in logs → should be 1 (not 20)

**Impact:** N events → ceil(N/batch_size) LLM calls. For typical OpenCode usage (3 events per idle): 3 calls → 1 call. For larger runs: dramatic reduction.

### 1C. Remember Post-Processing Enrichment ✅ COMPLETE

**Goal:** Explicit `amm remember` memories bypass the event→reflect pipeline but should still get entity/claim/relationship enrichment via asynchronous post-processing.

**Current state:** `remember` stores the memory immediately with embedding (best-effort). No entity extraction, no claim extraction, no relationship linking. The memory sits in the store as a rich text blob with no graph connections.

**Changes:**

1. **New job: `enrich_memories`** — finds memories where `entities_extracted` is absent in metadata, runs entity extraction (LLM if available, heuristic fallback), creates claims, links relationships.

2. **Enrichment scope**: Targets memories created via explicit `remember` (no `source_event_ids`) AND memories where entity/claim extraction hasn't run yet. Uses the processing ledger to avoid re-work.

3. **Enrichment is additive only**: Never modifies the memory body, type, subject, confidence, or importance. Only adds linked entities, claims, and relationships. The agent's original authored content is sacred.

4. **Integration with reflect**: When reflect runs, its `AnalyzeEvents` output already includes entities and relationships. These get linked to the memories created during reflect. The `enrich_memories` job only needs to handle memories that bypassed reflect (explicit remember, imported memories).

**Files changed:**
- `internal/service/enrich.go` — new file, EnrichMemories method
- `internal/service/service.go` — new job kind "enrich_memories"
- `examples/scripts/run-workers.sh` — add `enrich_memories` to the maintenance sequence

**QA:**
- New test: `TestEnrichMemories_ExtractsEntitiesForRememberedMemory` — remember a memory, run enrich, verify entities linked and `entities_extracted: "true"` in metadata
- New test: `TestEnrichMemories_SkipsAlreadyEnriched` — enrich a memory, run again, verify no re-processing (check metadata timestamps)
- New test: `TestEnrichMemories_DoesNotModifyBody` — verify body/subject/confidence unchanged after enrichment
- New test: `TestEnrichMemories_SkipsReflectCreatedMemories` — memory with source_event_ids already has entities from reflect → verify enrich skips it
- `CGO_ENABLED=1 go test -tags fts5 ./internal/service/... -count=1`

---

## Phase 2: Enriched LLM Pipeline (The "One Call, Many Outputs" Refactor)

### 2A. Expand the Summarizer Interface → Intelligence Provider ✅ COMPLETE

**Goal:** Replace the narrow `Summarizer` interface with a richer `IntelligenceProvider` that returns structured, multi-faceted analysis from a single LLM call.

**New interface** (in `internal/core/intelligence.go`):

```go
type IntelligenceProvider interface {
    // Summarizer compatibility (existing)
    Summarize(ctx context.Context, text string, maxLen int) (string, error)
    
    // Enriched extraction — replaces ExtractMemoryCandidate/Batch
    AnalyzeEvents(ctx context.Context, events []EventContent) (*AnalysisResult, error)
    
    // Lifecycle review — replaces heuristic decay/promote/contradict
    ReviewMemories(ctx context.Context, memories []MemoryReview) (*ReviewResult, error)
    
    // Narrative consolidation — replaces compress + consolidate
    ConsolidateNarrative(ctx context.Context, events []EventContent, existingMemories []MemorySummary) (*NarrativeResult, error)
}

type AnalysisResult struct {
    Memories     []MemoryCandidate
    Entities     []EntityCandidate      // typed entities with aliases
    Relationships []RelationshipCandidate // entity-to-entity relationships
    QualityTags  map[int][]string       // per-event quality tags
}

type EntityCandidate struct {
    CanonicalName string
    Type          string   // person, project, technology, concept, org, service
    Aliases       []string
    Description   string
}

type RelationshipCandidate struct {
    FromEntity    string // canonical name
    ToEntity      string
    Type          string // uses, depends-on, contradicts, authored-by, part-of
    Description   string
}

type ReviewResult struct {
    Promote  []string // memory IDs to boost importance
    Decay    []string // memory IDs to reduce importance
    Archive  []string // memory IDs to archive
    Merge    []MergeSuggestion
    Contradictions []ContradictionPair
}

type NarrativeResult struct {
    Summary      string
    TightDesc    string
    Episode      *EpisodeCandidate
    KeyDecisions []string
    Unresolved   []string
}
```

**Implementation:** `LLMIntelligenceProvider` makes ONE chat completion call for `AnalyzeEvents` that returns a JSON object with all of memories + entities + relationships + quality tags. The prompt is larger but the output is richer and it's ONE call instead of N.

**Model routing:** By default, all methods use the single configured `AMM_SUMMARIZER_*` endpoint/model. Optionally, users can set `AMM_REVIEW_ENDPOINT` / `AMM_REVIEW_MODEL` / `AMM_REVIEW_API_KEY` to route `ReviewMemories` (and potentially `ConsolidateNarrative`) to a different model. This is purely optional — single-model configuration works for everything. The `LLMIntelligenceProvider` accepts a `ChatCompleteFunc` per method group (extraction, review, summarize) and falls back to the default if not configured.

**Heuristic fallback:** `HeuristicIntelligenceProvider` implements the same interface using existing phrase-cue + capitalized-word logic. The interface is the boundary; implementations are swappable. All heuristic outputs are tagged with the appropriate `*_method: "heuristic"` metadata for future LLM upgrade.

**Backward compatibility:** Keep the existing `Summarizer` interface as a subset. `IntelligenceProvider` embeds/extends it. Existing code that only needs `Summarize` still works.

**Files changed:**
- `internal/core/intelligence.go` — new interface + types
- `internal/core/summarizer.go` — keep for backward compat, delegate to intelligence provider
- `internal/service/llm_intelligence.go` — LLM implementation with optional model routing
- `internal/service/heuristic_intelligence.go` — heuristic fallback
- `internal/service/service.go` — wire new provider
- `internal/runtime/config.go` — read optional `AMM_REVIEW_*` config
- `internal/runtime/factory.go` — build provider with model routing

**QA:**
- `CGO_ENABLED=1 go test -tags fts5 ./... -count=1` — full suite passes (interface change must not break any existing code)
- New test: `TestHeuristicIntelligence_ImplementsSummarizer` — verify heuristic provider satisfies Summarizer interface (backward compat)
- New test: `TestLLMIntelligence_AnalyzeEvents_ReturnsStructuredResult` — mock HTTP server returns JSON with memories + entities + relationships, verify parsing
- New test: `TestLLMIntelligence_FallsBackToHeuristic` — mock HTTP 500, verify heuristic results returned and `*_method: "heuristic"` set in metadata
- New test: `TestIntelligenceProvider_SingleCallExtraction` — verify AnalyzeEvents makes exactly 1 HTTP call for a batch (use httptest.Server with call counter)
- New test: `TestIntelligenceProvider_ModelRouting` — configure separate review endpoint, verify ReviewMemories goes to review endpoint while AnalyzeEvents goes to default

### 2B. Entity Extraction with Types and Aliases ✅ COMPLETE

**Goal:** Replace capitalized-word heuristic with LLM-powered entity extraction that understands types, aliases, and relationships.

**Current state:** `ExtractEntities()` in `entities.go` finds capitalized words. All entities are type `"topic"`. No alias detection. No relationship extraction.

**New state:** `AnalyzeEvents` returns `[]EntityCandidate` with:
- `Type`: one of person, project, technology, concept, org, service, artifact (matches spec section 15.2)
- `Aliases`: ["AMM", "agent-memory-manager", "amm", "the memory manager"]
- `Description`: concise one-liner

**Changes in entity storage:**
- `findOrCreateEntity` becomes smarter: checks aliases during matching, merges alias lists, updates type if upgrading from "topic" to a specific type
- New `relationships` table entries created from `RelationshipCandidate` output
- Entity overlap signal in scoring benefits immediately — typed entities with aliases produce better matches

**Heuristic fallback:** Existing capitalized-word extraction continues to work. Entities created by heuristics get type "topic" as before. LLM upgrades the type and adds aliases on next analysis.

**Files changed:**
- `internal/service/entities.go` — enhanced findOrCreateEntity with alias merging
- `internal/service/reflect.go` — use AnalysisResult.Entities instead of ExtractEntities()
- `internal/service/reprocess.go` — same
- `internal/service/recall.go` (buildScoringContext) — entity extraction at query time stays heuristic (no LLM on hot path), but now matches against richer entity records with aliases

**QA:**
- New test: `TestFindOrCreateEntity_MergesAliases` — create entity "AMM" type "topic", then findOrCreate with name "agent-memory-manager" and alias "AMM" → verify single entity with both aliases
- New test: `TestFindOrCreateEntity_UpgradesType` — entity exists as "topic", findOrCreate with type "technology" → verify type upgraded
- New test: `TestReflect_CreatesTypedEntities` — with LLM configured (mock), verify entities created with types other than "topic"
- New test: `TestEntityOverlap_MatchesAliases` — query "AMM", memory mentions "agent-memory-manager" → verify entity overlap signal fires via alias match
- `CGO_ENABLED=1 go test -tags fts5 ./internal/service/... -count=1`

### 2C. Consolidated Prompt Design ✅ COMPLETE

**Goal:** Design prompts that maximize information extraction per LLM call.

**`AnalyzeEvents` prompt structure:**

```
You are a memory analyst for an AI agent system. Analyze the following events 
and extract structured information.

For each batch, return a JSON object with:
1. "memories": durable facts, preferences, decisions, etc. (same rules as current extraction prompt)
2. "entities": named entities mentioned (people, projects, technologies, concepts, organizations, services)
3. "relationships": relationships between entities (uses, depends-on, part-of, etc.)
4. "event_quality": per-event quality assessment ("durable", "ephemeral", "noise", "context-dependent")

[Existing extraction rules from llm_summarizer.go buildMemoryExtractionPrompt]

Additional entity rules:
- For each entity, provide: canonical_name, type (person/project/technology/concept/org/service), aliases, brief description
- Merge entities that are clearly the same thing (e.g., "AMM" and "agent-memory-manager")
- Only extract entities that appear meaningfully in context, not passing mentions

Additional relationship rules:
- Only extract relationships that are explicitly stated or strongly implied
- Types: uses, depends-on, contradicts, authored-by, part-of, replaces, extends

Events:
[Event 1] ...
[Event 2] ...
```

**Token budget:** This prompt is ~40% larger than the current extraction-only prompt, but replaces what would be extraction + entity extraction + relationship extraction as separate calls. Net token savings.

### 2D. Processing Ledger Implementation ✅ COMPLETE

**Goal:** Implement the processing ledger described in Design Principles as a cross-cutting concern used by all phases.

**Changes:**

1. **Helper functions** in `internal/service/ledger.go`:
   - `setProcessingMeta(mem *core.Memory, key, value string)` — sets metadata key with null-safety
   - `hasProcessingStep(mem *core.Memory, key string) bool` — checks if step was done
   - `hasLLMProcessingStep(mem *core.Memory, key string) bool` — checks if step was done by LLM specifically
   - `needsLLMUpgrade(mem *core.Memory, key string) bool` — returns true if step was done by heuristic and LLM is available
   - `markExtracted(mem, method, model string)` — sets extraction_method, extraction_quality, extracted_at, extracted_model
   - `markEmbedded(mem, model string)` — sets embedded_at, embedded_model
   - `markEntitiesExtracted(mem, method string)` — sets entities_extracted, entities_extracted_method
   - `markClaimsExtracted(mem)` — sets claims_extracted
   - `markLifecycleReviewed(mem, model string)` — sets lifecycle_reviewed_at, lifecycle_reviewed_model

2. **All existing and new processing code uses these helpers** instead of raw `metadata["key"] = "value"`. This ensures consistency and makes it easy to add new ledger fields.

3. **Reprocess upgrade logic**: When any processing step has `*_method: "heuristic"` and LLM is now available, that step is eligible for re-processing. The `needsLLMUpgrade` helper centralizes this check.

**Files changed:**
- `internal/service/ledger.go` — new file with helper functions
- All service files that set metadata — use ledger helpers

**QA:**
- New test: `TestLedger_SetAndCheck` — verify set/has/needsUpgrade functions work correctly
- New test: `TestLedger_NullSafeMetadata` — verify helpers handle nil metadata map
- New test: `TestLedger_HeuristicMarkedForUpgrade` — set heuristic method, verify needsLLMUpgrade returns true
- `CGO_ENABLED=1 go test -tags fts5 ./internal/service/... -count=1`

---

## Phase 3: Semantic Entity Graph

### 3A. Graph Queries over Existing Schema ✅ COMPLETE

**Goal:** Enable graph-walk retrieval using the existing `relationships`, `entities`, `memory_entities`, and `claims` tables.

**Current state:** The schema supports all of this. `relationships` table has `from_entity_id`, `to_entity_id`, `relationship_type`. `memory_entities` links memories to entities with roles. `claims` link memories to subject/predicate/object structures. But there's no code that actually walks this graph.

**New capabilities:**

1. **Entity-walk recall**: Given an entity, find all related entities (1-hop, 2-hop), then find all memories linked to any of those entities. This is a SQL query pattern, not an LLM call.

2. **Claim-based recall**: For contradiction detection, find all claims with matching subject+predicate but different object values. Already partially implemented in `contradictions.go` but limited to exact string matching.

3. **Relationship-aware scoring**: In `scoring.go`, the entity overlap signal currently does substring matching. Enhance it to also check: "does the query mention an entity that has a *relationship* to an entity in this memory?" If querying about "SQLite" and a memory mentions "go-sqlite3", the "depends-on" relationship between them should boost the score.

**Files changed:**
- `internal/core/repository.go` — new `ListRelatedEntities(ctx, entityID, depth int)` method
- `internal/adapters/sqlite/repository.go` — implement with recursive CTE
- `internal/service/recall.go` — entity expansion in buildScoringContext
- `internal/service/scoring.go` — relationship-aware entity overlap

**No LLM calls added.** This is pure graph traversal over LLM-populated data.

### 3B. Graph Projection Table (Derived, Rebuildable) ✅ COMPLETE

**Goal:** Pre-compute entity neighborhoods for fast recall-time graph walks.

**New derived table:**
```sql
CREATE TABLE IF NOT EXISTS entity_graph_projection (
    entity_id TEXT NOT NULL,
    related_entity_id TEXT NOT NULL,
    hop_distance INTEGER NOT NULL,
    relationship_path TEXT, -- JSON array of relationship types
    score REAL NOT NULL DEFAULT 1.0,
    created_at TEXT NOT NULL,
    PRIMARY KEY (entity_id, related_entity_id)
);
```

**Rebuild job:** `rebuild_entity_graph` — walks relationships table and computes 1-2 hop neighborhoods. Runs after any job that creates entities or relationships. Derived/rebuildable.

**Files changed:**
- `internal/adapters/sqlite/migrations.go` — new migration (append-only)
- `internal/adapters/sqlite/repository.go` — CRUD for projection table
- `internal/service/service.go` — new job kind
- `internal/service/recall.go` — use projection for fast entity expansion

**QA (Phase 3A):**
- New test: `TestListRelatedEntities_OneHop` — create entities A→B→C, query A depth=1 → returns B only
- New test: `TestListRelatedEntities_TwoHop` — same setup, depth=2 → returns B and C
- New test: `TestRecall_EntityRelationshipBoost` — memory mentions "go-sqlite3", query "SQLite", relationship "go-sqlite3 depends-on SQLite" exists → verify memory scores higher than without relationship
- `CGO_ENABLED=1 go test -tags fts5 ./internal/service/... ./internal/adapters/sqlite/... -count=1`

**QA (Phase 3B):**
- New test: `TestRebuildEntityGraph_Projection` — create relationships, run rebuild job, verify projection table populated
- New test: `TestGraphProjection_Rebuildable` — delete projection, rebuild, verify identical results
- `amm jobs run rebuild_entity_graph` → verify exit 0 and status shows projection table populated

---

## Phase 4: Lifecycle Intelligence

### 4A. Consolidated Lifecycle Review Job ✅ COMPLETE

**Goal:** Replace three separate heuristic jobs (decay, promote, detect_contradictions) with one LLM-powered batch review.

**Current state:**
- `decay_stale_memory`: age-based exponential decay, archives when importance < 0.1
- `promote_high_value`: **stub — returns 0, nil** (completely unimplemented)
- `detect_contradictions`: claim-based string matching, auto-supersedes older memory

**New job: `lifecycle_review`**

1. Collect candidates: all active memories older than N days OR below importance threshold OR with low recall frequency
2. Batch them into an LLM call via `ReviewMemories` (batch size configurable via `AMM_LIFECYCLE_REVIEW_BATCH_SIZE`, default 50):

```
Review the following memories for an AI agent system. For each, assess:
1. Is this still valuable? (yes/archive/merge)
2. Should importance be adjusted? (up/down/unchanged)
3. Does this contradict any other memory in the batch?
4. Should this be merged with another memory? (provide merge target)
5. Is this information likely still true?

Return a JSON object with: promote (IDs), decay (IDs), archive (IDs), merge (pairs), contradictions (pairs with explanation)
```

3. Apply results: update importance, archive, merge, create contradiction memories
4. Tag reviewed memories with `last_lifecycle_review` timestamp in metadata (never review the same memory twice in the same cycle)

**Heuristic decay/archive jobs remain as fallback** when LLM is unconfigured. The existing logic is fine as a safety net.

**`promote_high_value` gets a real implementation** — the LLM identifies memories worth boosting, but also: memories with high recall frequency (from `recall_history`) but low importance get auto-promoted as a heuristic signal.

**Files changed:**
- `internal/service/lifecycle_review.go` — new file
- `internal/service/promote.go` — real implementation using recall_history
- `internal/core/intelligence.go` — ReviewMemories method
- `internal/service/llm_intelligence.go` — implement ReviewMemories prompt
- `internal/service/service.go` — new job kind

**QA:**
- New test: `TestLifecycleReview_PromotesHighRecallMemory` — create memory, record 10 recalls for it, run lifecycle_review → verify importance increased
- New test: `TestLifecycleReview_ArchivesStaleMemory` — create old low-importance memory, run review → verify archived
- New test: `TestLifecycleReview_DetectsContradiction` — create two memories with conflicting claims, run review → verify contradiction memory created
- New test: `TestLifecycleReview_TagsReviewedMemories` — verify `last_lifecycle_review` in metadata after review
- New test: `TestLifecycleReview_NoDoubleReview` — run twice in same cycle → verify already-reviewed memories skipped
- New test: `TestPromoteHighValue_UsesRecallHistory` — memory with high recall frequency but low importance → verify importance boosted
- `amm jobs run lifecycle_review` → verify exit 0, output shows promote/decay/archive counts

### 4B. Cross-Project Memory Transfer ✅ COMPLETE

**Goal:** Detect when a project-scoped memory should be promoted to global scope.

**Trigger conditions (heuristic + LLM):**

1. **Same memory appears in 2+ projects** — if `reflect` or `remember` creates very similar memories (high jaccard or embedding similarity) in different projects, that's a signal the knowledge is cross-project.

2. **Pattern detection in lifecycle_review** — the LLM batch review can identify "this is a general Go best practice, not project-specific" and suggest scope promotion.

3. **New job: `cross_project_transfer`** — runs after lifecycle_review, examines project-scoped memories with high importance and confidence, batches them for LLM assessment:

```
These memories are currently scoped to specific projects. For each, assess whether
the knowledge is genuinely project-specific or generalizable:
- If generalizable: recommend promotion to global scope
- If project-specific: leave as-is
- If partially generalizable: recommend creating a global version with project-specific detail removed
```

**Implementation:** When promoting, create a new global-scope memory and supersede the project-scoped one. Preserve provenance via `source_event_ids`.

**Files changed:**
- `internal/service/cross_project.go` — new file
- `internal/core/intelligence.go` — add to ReviewMemories or separate method
- `internal/service/service.go` — new job kind

**QA:**
- New test: `TestCrossProjectTransfer_PromotesSimilarMemories` — create same fact in project-A and project-B (high jaccard), run cross_project_transfer → verify global memory created, project-scoped ones superseded
- New test: `TestCrossProjectTransfer_KeepsProjectSpecific` — create project-specific memory (e.g., "this repo uses CircleCI"), run transfer → verify NOT promoted
- `amm jobs run cross_project_transfer` → verify exit 0

---

## Phase 5: Narrative Consolidation

### 5A. LLM-Powered Session Narratives ✅ COMPLETE

**Goal:** `consolidate_sessions` produces rich episode narratives, not just truncated event dumps.

**Current state:** `compress.go` concatenates event content and calls `Summarize()` (which is truncation in heuristic mode, passable summarization in LLM mode). No episode creation.

**New state:** `ConsolidateNarrative` returns:
- `Summary`: compressed session body
- `TightDesc`: retrieval-optimized description
- `Episode`: full narrative including participants, decisions, outcomes, unresolved items
- `KeyDecisions`: decisions made during the session → auto-create decision memories
- `Unresolved`: open questions → auto-create open_loop memories

**One LLM call per session** produces a summary, an episode, and potentially 1-3 auto-extracted memories. Currently this would be 2 summarize calls + 0 episode creation.

**Files changed:**
- `internal/service/compress.go` — ConsolidateSessions uses ConsolidateNarrative
- `internal/core/intelligence.go` — ConsolidateNarrative method
- `internal/service/llm_intelligence.go` — implement prompt
- `internal/service/service.go` — episode insertion from narrative result

**QA:**
- Existing test: `TestConsolidateSessions_UsesSummarizer` must still pass (backward compat)
- New test: `TestConsolidateSessions_CreatesEpisode` — with LLM configured, consolidate session → verify episode created with participants, decisions, unresolved items
- New test: `TestConsolidateSessions_AutoExtractsDecisions` — session events contain explicit decision → verify decision memory auto-created
- New test: `TestConsolidateSessions_AutoExtractsOpenLoops` — session events contain unresolved question → verify open_loop memory auto-created
- `amm jobs run consolidate_sessions` → verify episodes appear in `amm recall --mode episodes`

### 5B. Hierarchical Summary Narratives ✅ COMPLETE

**Goal:** Higher-level summaries over leaf summaries (the spec mentions "potentially hierarchical" in section 11.2).

**New job: `build_topic_summaries`** — groups leaf summaries by entity/topic overlap and creates parent summaries with narrative arcs.

This is a cold-path job (runs periodically, not on hot path). One LLM call per topic group.

**Files changed:**
- `internal/service/compress.go` — new BuildTopicSummaries method
- `internal/service/service.go` — new job kind

**QA:**
- New test: `TestBuildTopicSummaries_GroupsByEntity` — create leaf summaries linked to shared entities, run job → verify parent summary created grouping them
- New test: `TestBuildTopicSummaries_Idempotent` — run twice → verify no duplicate parent summaries
- `amm jobs run build_topic_summaries` → verify exit 0

---

## Phase 6: Learned Ranking

### 6A. Implicit Relevance Feedback ✅ COMPLETE

**Goal:** Learn which memories are actually useful from agent behavior.

**Signal:** When an agent calls `recall` and then calls `expand` on a specific item, that's implicit positive feedback — the agent chose to look deeper at this memory. When an agent recalls but never expands anything, the recall was probably low-quality.

**Current state:** `recall_history` already tracks what was shown (session_id, item_id, item_kind, shown_at). But `Expand()` in `internal/core/service.go:27` has signature `Expand(ctx, id, kind string)` — it doesn't know which session triggered the expansion. We need to thread session context through Expand to connect expansions back to recalls.

**API contract changes required:**

1. **Expand signature change**: `Expand(ctx, id, kind string)` → `Expand(ctx, id, kind string, opts ExpandOptions)` where `ExpandOptions` has optional `SessionID string`
2. **CLI adapter** (`internal/adapters/cli/runner.go`): Add `--session-id` flag to expand command
3. **MCP adapter** (`internal/adapters/mcp/server.go`): Add `session_id` to `amm_expand` tool schema
4. **Contracts** (`internal/contracts/v1/payloads.go`): Add `SessionID` to ExpandRequest

**New table (derived):**
```sql
CREATE TABLE IF NOT EXISTS relevance_feedback (
    session_id TEXT NOT NULL,
    item_id TEXT NOT NULL,
    item_kind TEXT NOT NULL,
    action TEXT NOT NULL, -- 'shown', 'expanded', 'used_in_remember'
    created_at TEXT NOT NULL,
    PRIMARY KEY (session_id, item_id, action)
);
```

**Track:**
- `shown` — item appeared in recall results (already tracked in `recall_history`, can be copied/referenced)
- `expanded` — item was expanded via `Expand()` with a session_id (new tracking in service.go Expand impl)
- `used_in_remember` — if a `remember` call's source_event_ids overlap with a recently recalled item's source events, that's a strong signal (checked in service.go Remember)

**QA:**
- New test: `TestExpand_RecordsFeedback` — recall with session_id, expand with same session_id, verify relevance_feedback row with action "expanded"
- New test: `TestExpand_NoSessionID_NoFeedback` — expand without session_id, verify no feedback recorded (backward compat)
- New test: `TestRemember_RecordsFeedbackForOverlappingEvents` — recall item, then remember with overlapping source_event_ids, verify "used_in_remember" feedback
- `CGO_ENABLED=1 go test -tags fts5 ./internal/service/... -count=1` — all existing Expand tests still pass

**Files changed:**
- `internal/core/service.go` — add ExpandOptions to Expand signature
- `internal/core/types.go` — add ExpandOptions struct
- `internal/contracts/v1/payloads.go` — add SessionID to ExpandRequest
- `internal/adapters/cli/runner.go` — --session-id on expand
- `internal/adapters/mcp/server.go` — session_id on amm_expand
- `internal/adapters/sqlite/migrations.go` — relevance_feedback table (new migration)
- `internal/adapters/sqlite/repository.go` — InsertRelevanceFeedback, ListRelevanceFeedback
- `internal/core/repository.go` — feedback repo methods
- `internal/service/service.go` — Expand records feedback when session_id present; Remember checks overlap

### 6B. Ranking Weight Adjustment ✅ COMPLETE

**Goal:** Use relevance feedback to adjust the static scoring weights.

**Approach:** Periodically (cold-path job), compute:
- For each signal (lexical, semantic, entity_overlap, etc.), what's the average signal value for expanded items vs shown-but-not-expanded items?
- Signals that are higher for expanded items should get more weight.
- Apply a Bayesian update to the base weights (with strong prior toward current weights, so learning is gradual)

**This is NOT an LLM call.** It's a statistics job over the feedback table. Pure SQL + Go math.

**Files changed:**
- `internal/adapters/sqlite/migrations.go` — relevance_feedback table (in 6A)
- `internal/service/scoring.go` — loadable weights (read from config/table instead of const)
- `internal/service/learned_ranking.go` — weight update job
- `internal/service/service.go` — new job kind

**QA:**
- New test: `TestLearnedRanking_BoostsExpandedSignals` — record feedback where high-semantic items are always expanded → run weight update → verify semantic weight increased
- New test: `TestLearnedRanking_StrongPrior` — with very little feedback data, verify weights stay close to defaults (Bayesian prior dominates)
- New test: `TestLearnedRanking_LoadableWeights` — verify scoring uses loaded weights, not hardcoded constants
- `amm jobs run update_ranking_weights` → verify exit 0, output shows new weights

---

## Phase 7: Multi-Agent Memory

### 7A. Agent Isolation ✅ COMPLETE

**Goal:** Memories can be scoped to specific agents. An agent's private operational knowledge doesn't leak to other agents.

**Current state:** The schema and Go types already support `agent_id` on memories (`internal/core/types.go:160` has `AgentID string`, the memories table in migrations already has `agent_id TEXT`). However, **the recall pipeline doesn't filter by agent** — `RecallOptions` in `internal/core/service.go:93-101` has no `AgentID` field, and no recall path checks `agent_id` or `privacy_level`.

**Changes:**

1. **Add `AgentID` to `RecallOptions`** in `internal/core/service.go` — new optional field
2. **Add `AgentID` to `RecallRequest`** in `internal/contracts/v1/payloads.go` — so CLI/MCP callers can pass it
3. **Recall filtering in `recall.go`**: When `AgentID` is provided in `RecallOptions`, filter candidates to show:
   - Memories with matching `agent_id` (agent's own)
   - Memories with empty `agent_id` (shared/unscoped)
   - Memories with `privacy_level = "shared"` regardless of `agent_id` (explicitly shared)
4. **Repository filtering**: Add `AgentID` to `ListMemoriesOptions` and `SearchMemories` — push filtering to SQL for efficiency
5. **Remember path**: If the caller provides `agent_id`, it's stored. Otherwise empty (shared). Already supported by the Go type.
6. **CLI/MCP adapters**: Thread `--agent-id` flag through CLI runner and MCP tool schemas

**QA:**
- `CGO_ENABLED=1 go test -tags fts5 ./internal/service/... -count=1` — all existing tests pass
- New test: `TestRecall_FiltersByAgentID` — create memories with agent_id "agent-a" and "agent-b", recall with agent_id "agent-a", verify only agent-a's + unscoped memories returned
- New test: `TestRecall_SharedPrivacyCrossesAgentBoundary` — create memory with agent_id "agent-b" + privacy "shared", recall as "agent-a", verify it appears
- CLI: `amm remember --agent-id agent-a --type fact --body "test" --tight "test"` then `amm recall --agent-id agent-b "test"` → should NOT find it. `amm recall --agent-id agent-a "test"` → should find it.

**Files changed:**
- `internal/core/service.go` — add AgentID to RecallOptions
- `internal/core/repository.go` — add AgentID to ListMemoriesOptions
- `internal/contracts/v1/payloads.go` — add agent_id to RecallRequest and RememberRequest
- `internal/adapters/sqlite/repository.go` — SQL WHERE clause for agent_id filtering
- `internal/service/recall.go` — threading agent_id into candidate filtering
- `internal/adapters/cli/runner.go` — --agent-id flag on recall and remember
- `internal/adapters/mcp/server.go` — agent_id in amm_recall and amm_remember tool schemas

### 7B. Selective Sharing ✅ COMPLETE

**Goal:** Agents can explicitly share specific memories with other agents or make them broadly available.

**Current state:** `privacy_level` field exists (private/shared/public_safe) in schema and Go types but isn't enforced in recall filtering (fixed in 7A above).

**Changes:**

1. **New service method: `ShareMemory(ctx, id string, privacy PrivacyLevel) (*Memory, error)`** in `internal/core/service.go` — updates a memory's privacy level
2. **New command: `share`** — allows an agent to change a memory's privacy level:
   ```bash
   amm share <memory_id> --privacy shared
   ```
3. **Cross-agent memory merging**: When Agent A creates a memory that duplicates Agent B's memory (detected by dedup), the system can merge them into a shared memory, preserving both agents' provenance.

**QA:**
- New test: `TestShareMemory_ChangesPrivacyLevel` — create private memory, share it, verify recall crosses agent boundary
- New test: `TestShareMemory_InvalidID` — verify error on nonexistent ID
- CLI: `amm share <id> --privacy shared` → verify JSON output shows updated privacy
- MCP: `amm_share` tool call → verify same behavior

**Files changed:**
- `internal/core/service.go` — ShareMemory method on Service interface
- `internal/service/service.go` — implement ShareMemory
- `internal/contracts/v1/commands.go` — CmdShare constant
- `internal/contracts/v1/payloads.go` — ShareRequest schema
- `internal/contracts/v1/validation.go` — ValidateShare
- `internal/adapters/cli/runner.go` — share subcommand
- `internal/adapters/mcp/server.go` — amm_share tool

---

## Phase 8: Ingestion Triage

### 8A. Smart Triage for Borderline Events ✅ COMPLETE

**Goal:** Before events enter the reflect pipeline, assess whether they're worth processing.

**Current state:** Heuristic noise filter downgrades events to `read_only` based on content patterns. When LLM is configured, `read_only` events are passed through. But there's no middle ground — either everything goes to the LLM, or heuristics decide what's noise.

**New state:** Three-tier triage:

1. **Clearly noise** (heuristic) → skip. Examples: empty messages, heartbeats, status pings.
2. **Clearly valuable** (heuristic) → pass to reflect. Examples: explicit decisions, preference statements.
3. **Borderline** (LLM triage) → quick LLM assessment: "Is this worth reflecting on?"

The LLM triage call is CHEAP — it's a classification task, not generation. Short prompt, short response. And it only fires for the borderline bucket.

**Implementation:**

1. **In `ShouldIngest`**: After heuristic classification, if result is "borderline" and LLM is configured, make a quick classification call.
2. **New method on IntelligenceProvider**: `TriageEvent(ctx, content string) (TriageDecision, error)` returning skip/reflect/high-priority.
3. **Batch triage**: During reflect, collect borderline events and triage them in one batched call before extraction.

**Files changed:**
- `internal/core/intelligence.go` — TriageEvent method
- `internal/service/llm_intelligence.go` — implement
- `internal/service/reflect.go` — integrate triage before extraction

**QA:**
- New test: `TestTriage_SkipsClearNoise` — heartbeat event → heuristic skips, no LLM call
- New test: `TestTriage_PassesClearSignal` — "We decided to use Postgres" → heuristic passes, no LLM call
- New test: `TestTriage_LLMTriagesBorderline` — grep output with embedded lesson → heuristic marks borderline → LLM triage call fires → verify decision (skip or reflect)
- New test: `TestTriage_FallsBackOnLLMFailure` — borderline event + LLM error → verify heuristic decision used
- `CGO_ENABLED=1 go test -tags fts5 ./internal/service/... -count=1`

**Cost:** 1 additional cheap LLM call per borderline event batch. Since these were previously either dropped (heuristic mode) or fully processed (LLM mode), this is actually cheaper than the current "process everything" LLM approach.

---

## Implementation Priority & Dependencies

```
Phase 1A (Provisional Status)     ←── No dependencies, immediate value
Phase 1B (Batched Reflect)        ←── No dependencies, biggest cost saving
Phase 1C (Remember Enrichment)    ←── Depends on 1A (uses processing ledger metadata)
    │
    ▼
Phase 2D (Processing Ledger)      ←── Foundation for all processing tracking
Phase 2A (Intelligence Provider)  ←── Foundation for enriched LLM pipeline
Phase 2B (Entity Extraction)      ←── Depends on 2A
Phase 2C (Consolidated Prompts)   ←── Depends on 2A
    │
    ▼
Phase 3A (Graph Queries)          ←── Depends on 2B (needs typed entities)
Phase 3B (Graph Projection)       ←── Depends on 3A
    │
    ▼
Phase 4A (Lifecycle Review)       ←── Depends on 2A + 2D
Phase 4B (Cross-Project Transfer) ←── Depends on 4A
    │
    ▼
Phase 5A (Session Narratives)     ←── Depends on 2A
Phase 5B (Hierarchical Summaries) ←── Depends on 5A
    │
    ▼
Phase 6A (Relevance Feedback)     ←── Independent, can start anytime after Phase 1
Phase 6B (Learned Ranking)        ←── Depends on 6A
    │
    ▼
Phase 7A (Agent Isolation)        ←── Independent, can start anytime
Phase 7B (Selective Sharing)      ←── Depends on 7A
    │
    ▼
Phase 8A (Ingestion Triage)       ←── Depends on 2A
```

### Parallelizable work streams:
- **Stream A**: Phase 1A+1B → Phase 2D+2A → Phase 2B+2C → Phase 3 → Phase 8 (extraction + graph pipeline)
- **Stream B**: Phase 1C → Phase 4 → Phase 5 (enrichment + lifecycle + narrative)
- **Stream C**: Phase 6 (learned ranking, independent after Phase 1)
- **Stream D**: Phase 7 (multi-agent, independent)

Streams C and D can proceed in parallel with A and B once Phase 1 is complete.
Phase 2D (Processing Ledger) should be implemented early in Phase 2 since all subsequent phases depend on it for tracking.

---

## What's Still Missing (Beyond This Plan)

1. **Postgres support** (spec Phase 3) — for multi-user/team deployments. Not critical for single-user.
2. **HTTP API adapter** — spec section 29.1 lists REST endpoints. Currently only CLI + MCP.
3. **Anti-hub logic** (spec section 21.2) — common entities dominating recall. Could be a scoring signal or a graph-based dampening factor. Natural extension of Phase 3 (entity graph).
4. **Source trust weighting** (spec section 32.3) — not all events are equally trustworthy. Could tie into the processing ledger.
5. **User-controlled forgetting** — `amm forget <id>` with proper cascading (supersede, not delete).
6. **Explain-recall improvements** — surface the signal breakdown in recall results (the `SignalBreakdown` struct exists but isn't returned to callers).
7. **Workspace scope** (spec section 7.7, deferred) — for multi-team deployments.
8. **Full transcript capture** — OpenCode plugin currently captures tool results and session events but NOT full user/assistant messages. This limits what reflect can extract from.

---

## Success Criteria

AMM has reached end-game when:

1. **Zero information loss from events to memories** — nothing worth remembering is silently dropped
2. **Entity graph is rich enough for graph-walk retrieval** — querying "SQLite" finds memories about "go-sqlite3", "CGO", "FTS5" via relationship traversal
3. **Contradictions are detected semantically, not just by string matching** — "we use JWT" and "we switched to session cookies" are recognized as contradictory
4. **Recall quality improves with usage** — learned ranking means the 1000th recall is better than the 1st
5. **Cross-project knowledge transfers automatically** — a lesson learned in project A surfaces in project B when relevant
6. **Session narratives tell coherent stories** — not truncated event dumps
7. **Multi-agent deployments work cleanly** — agents have their own memories but can share knowledge
8. **The heuristic-only mode still works** — degraded but functional, zero API calls, zero dollars
