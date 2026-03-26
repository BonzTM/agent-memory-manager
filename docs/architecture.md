# amm Architecture

Detailed architecture reference for the Agent Memory Manager.

## System Overview

amm is a Go binary that provides persistent, typed, temporal memory for agents. It is:

- **API-first**: a single `Service` interface defines all business logic; CLI, MCP, and HTTP are thin adapters
- **SQLite-backed**: one local database file, no external services
- **Single binary**: `go build` produces a self-contained executable
- **Minimal dependencies**: Go standard library plus `mattn/go-sqlite3` for the storage engine

The service layer (`internal/core/Service`) is the only entry point for business logic. No adapter bypasses it. This makes behavior consistent regardless of whether a command arrives via CLI, MCP JSON-RPC, or HTTP.

## Memory Architecture Layers

amm organizes information into five layers, each with a distinct role.

### Layer A: Working Memory

- **Ephemeral, runtime-only**
- Holds current turn state: the active query, recent topic cache, entity extraction results
- Not persisted to the database by default
- Exists only for the duration of a single request or session
- Feeds into ambient recall as context for scoring and entity matching

### Layer B: History Layer

- **Append-only raw events and transcripts**
- The complete, unmodified archive of every interaction
- Stored in the `events` table
- Each event carries: kind, source system, session/project/agent IDs, actor info, privacy level, content, and timestamps
- FTS-indexed for search, but the raw records are canonical truth
- Not injected into prompts directly -- too large, too noisy
- Source material for reflection and compression workers

### Layer C: Compression Layer

- **Summaries linked to source spans**
- Built by compression workers from raw history
- Summary kinds: `leaf`, `session`, `topic`, `episode`, `condensed`
- Every summary records its `source_span` (the event IDs and/or child summary IDs it was built from)
- Hierarchy is tracked via `summary_edges`, enabling drill-down from high-level summaries to leaf summaries to raw events
- Supports the describe/expand retrieval flow: a thin description leads to a full expansion that can trace back to source history

### Layer D: Canonical Memory Layer

- **Typed durable memory records -- the authoritative memory substrate**
- Stored in the `memories` table
- Each memory has: type, scope, subject, body, tight description, confidence, importance, status, temporal validity, provenance links, and tags

The 16 memory types:

| Type | Purpose |
|------|---------|
| `identity` | Who someone or something is |
| `preference` | How someone prefers things done |
| `fact` | A durable factual assertion |
| `decision` | A deliberate choice that was made |
| `episode` | A narrative unit of activity |
| `todo` | An outstanding action item |
| `relationship` | A link between entities |
| `procedure` | How to do something |
| `constraint` | A rule or limitation that must be respected |
| `incident` | Something that went wrong |
| `artifact` | A reference to a non-message source material |
| `summary` | A compression of other content |
| `active_context` | Currently relevant state that should surface |
| `open_loop` | An unresolved question or pending item |
| `assumption` | A belief that has not been confirmed |
| `contradiction` | A detected conflict between memories or claims |

Memory lifecycle states: `active`, `superseded`, `archived`, `retracted`.

Supersession is explicit: a memory can record what it `supersedes` and what `superseded_by` it, with a timestamp. This models truth change over time.

### Layer E: Derived Index Layer

- **Disposable, rebuildable retrieval aids**
- FTS5 virtual tables: `memories_fts`, `summaries_fts`, `episodes_fts`, `events_fts`
- Optional `embeddings` table for semantic similarity
- `retrieval_cache` for query result caching
- `recall_history` for repetition suppression
- Kept in sync via SQLite triggers (FTS) or maintenance jobs (embeddings, cache)
- Can always be rebuilt from canonical tables via `amm repair --fix indexes` or the `rebuild_indexes` job

**Core invariant: canonical records and source history are authoritative. Derived indexes are disposable.**

## Scope Model

amm uses two orthogonal axes to control where a memory belongs and who can see it.

### Scope (structural)

Answers: *where does this memory belong?*

| Scope | Meaning | Required fields |
|-------|---------|-----------------|
| `global` | Applies across all projects and sessions | -- |
| `project` | Tied to a specific project or repo | `project_id` |
| `session` | Exists for continuity within a single session | `session_id` |

### Privacy level (access control)

Answers: *who is allowed to see it?*

| Level | Meaning |
|-------|---------|
| `private` | Only visible to the owning user/agent (default) |
| `shared` | Safe to surface in team or multi-agent contexts |
| `public_safe` | No secrets, credentials, or personal information |

These axes are independent. A memory can be `global` scope + `private` privacy (personal preferences across all projects) or `project` scope + `shared` privacy (a team decision about a specific repo).

### Agent memory

`agent_id` is an optional field on any scope. It indicates agent-specific operational memory without creating a separate scope axis.

## Processing Pipeline

amm runs a staged intelligence pipeline to move information from raw history to structured memory:

```
Ingest → Reflect → Index → Compress → Consolidate → Dedup/Enrich → Lifecycle → Index
```

1. **Ingest**: Raw events and transcripts are appended to the history layer (`events`). Writes are synchronous and lightweight.
2. **Reflect**: Reflection processes triaged events in batches and extracts candidate memories. Extraction no longer embeds items individually to save overhead. Processing metadata is recorded in a ledger.
3. **Index (Phase 1)**: `rebuild_indexes` runs after reflect to batch-embed new memories and update FTS5. It uses incremental queries (`ListUnembeddedMemories`) to avoid full table scans.
4. **Compress**: Background workers build leaf/session summaries over event spans using batched compression (`CompressEventBatches`).
5. **Consolidate**: Higher-level narrative units (episodes, topic summaries) are built from leaf summaries and events using `SummarizeTopicBatches`.
6. **Dedup/Enrich**: `merge_duplicates` uses semantic similarity to consolidate overlapping memories. `enrich_memories` links entities and `rebuild_entity_graph` projects the relationship graph.
7. **Lifecycle**: Review cycles promote, decay, or archive memories. `cross_project_transfer` moves high-confidence project memories to global scope.
8. **Index (Phase 2)**: A final indexing pass catches summaries and episodes created in previous phases.

---

## Embedding Strategy

amm uses a "batch-first" embedding strategy to maximize throughput and minimize API overhead:

- **No per-item embedding**: Individual worker jobs (reflect, compress, consolidate) do not generate embeddings for the items they create.
- **Incremental rebuilds**: The `rebuild_indexes` job performs batched embedding generation. It uses `ListUnembeddedMemories` and `ListUnembeddedSummaries` to find only items missing vectors for the current model.
- **Optimized queries**: Queries for unembedded items are optimized to handle large datasets (up to 50k items) without scanning the entire database.
- **Full rebuilds**: The `rebuild_indexes_full` variant allows force-rebuilding all vectors if the model or provider changes.

---

## Retrieval Architecture

### Find, Describe, Expand

amm retrieval follows a three-step flow:

1. **Find** (`recall`): Returns a thin list of scored `RecallItem` records — just ID, kind, type, scope, score, and tight description.
2. **Describe** (`describe`): Returns slightly richer metadata for one or more items without the full body.
3. **Expand** (`expand`): Returns the full record plus provenance.

### Scoring Formula

Recall uses a weighted multi-signal formula with dynamic renormalization:

```
score =
    w_lexical * lexical
  + w_extraction_quality * extraction_quality
  + w_semantic * semantic
  + w_entity_overlap * entity_overlap
  + w_scope_fit * scope_fit
  + w_recency * recency
  + w_importance * importance
  + w_temporal_validity * temporal_validity
  + w_structural_proximity * structural_proximity
  + w_freshness * freshness
  - w_repetition_penalty * repetition_penalty
```

Key scoring signals:

- **Anti-hub dampening**: High-degree entities (hubs) receive IDF-like dampening to prevent common terms from skewing results.
- **Extraction quality**: provisional memories are downweighted vs verified or upgraded records.
- **Graph-aware overlap**: entity overlap uses query entities expanded by aliases and related entities from the projected graph.
- **Learned weights**: Bayesian updates from relevance feedback (`expanded` actions) tune weights over time.

---

## Entity Graph

Entity modeling uses four components:

1. **Entities**: Canonical named nodes with types and aliases.
2. **Memory Links**: joins between memories and entities.
3. **Relationships**: Explicit edges like `uses` or `depends-on`.
4. **Projection**: Derived 1-2 hop related-entity table with hop-weighted scores.

Query entities expand via aliases and graph neighbors. This allows memories linked to related entities to surface even if exact terms are missing. Batch query methods (`GetMemoryEntitiesBatch`, `LinkMemoryEntitiesBatch`) ensure low-latency graph operations.

---

## Maintenance and Upgrades

- **`reset_derived`**: Use this command to purge all disposable data (FTS5, embeddings, caches, projections). This is the standard path for model upgrades or index corruption.
- **Reprocess**: The reprocessing job is retrofitted with the endgame pipeline, using the processing ledger and entity linking for consistent backfills.


## Intelligence Provider

`core.IntelligenceProvider` is the intelligence abstraction used by reflection and lifecycle logic:

- `AnalyzeEvents`
- `TriageEvents`
- `ReviewMemories`
- `ConsolidateNarrative`

amm runs in dual mode:

- **LLM intelligence** (`LLMIntelligenceProvider`): uses chat-completion prompts for triage, extraction, review, and narrative consolidation.
- **Heuristic intelligence** (`HeuristicIntelligenceProvider`): local fallback for no-LLM or LLM failure cases.

LLM provider methods are fail-soft: if a call or parse fails, they fall back to heuristic behavior (or empty review results where appropriate). This preserves forward progress while improving quality when external models are available.

## Schema Overview

### Canonical Tables (authoritative)

| Table | Purpose |
|-------|---------|
| `events` | Append-only raw interaction history |
| `summaries` | Compression layer objects over history |
| `summary_edges` | Parent/child links for summary hierarchy and expansion |
| `memories` | Typed durable memory records |
| `claims` | Structured atomic assertions linked to memories |
| `entities` | Canonical entity records (people, systems, concepts) |
| `memory_entities` | Join table linking memories to entities |
| `relationships` | Explicit entity-to-entity relationship edges |
| `projects` | Project registry for project-scoped metadata |
| `episodes` | Narrative memory units spanning events and summaries |
| `artifacts` | Ingested non-message source material |
| `jobs` | Queue and history for maintenance workers |
| `ingestion_policies` | Policy rules controlling read/write behavior per source pattern |

### Derived Tables (rebuildable)

| Table | Purpose |
|-------|---------|
| `memories_fts` | FTS5 full-text index over memories |
| `summaries_fts` | FTS5 full-text index over summaries |
| `episodes_fts` | FTS5 full-text index over episodes |
| `events_fts` | FTS5 full-text index over events |
| `embeddings` | Optional vector embeddings for semantic similarity |
| `retrieval_cache` | Cached query results with TTL |
| `recall_history` | Per-session record of shown items for repetition suppression |
| `relevance_feedback` | Implicit relevance actions (e.g., `expanded`) used for learned ranking |
| `entity_graph_projection` | Derived 1-2 hop entity-neighbor projection used in graph-aware recall |

### Freshness

Freshness is **computed at query time**, not stored as a column. It uses an exponential decay function over the most recent of `last_confirmed_at`, `updated_at`, `observed_at`, or `created_at`, with a configurable half-life (default: 14 days). This avoids stale metadata about staleness.

## Key Invariants

1. **Service layer is the only entry point.** CLI, MCP, and HTTP adapters call through `core.Service`. No adapter executes business logic directly.
2. **Canonical tables are truth. Derived tables are rebuildable.** If FTS, embeddings, or cache become corrupted, they can be rebuilt from canonical data without data loss.
3. **Schema changes go through migrations only.** All DDL lives in `internal/adapters/sqlite/migrations.go`. No ad-hoc schema modification.
4. **History is append-only.** Events are never modified or deleted. They are the source-of-truth archive.
5. **Summaries are always linked to source spans.** Every summary records which events or child summaries it was built from, enabling full expansion back to raw history.
6. **CLI and MCP expose the same commands.** Both adapters wrap the same service interface, ensuring consistent behavior.
7. **Contract changes update both sides.** Changes to typed payloads must update `internal/contracts/v1` and `spec/v1` together.
