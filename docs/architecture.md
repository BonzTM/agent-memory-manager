# AMM Architecture

Detailed architecture reference for the Agent Memory Manager.

## System Overview

AMM is a Go binary that provides persistent, typed, temporal memory for agents. It is:

- **API-first**: a single `Service` interface defines all business logic; CLI, MCP, and HTTP are thin adapters
- **SQLite-backed**: one local database file, no external services
- **Single binary**: `go build` produces a self-contained executable
- **Minimal dependencies**: Go standard library plus `mattn/go-sqlite3` for the storage engine

The service layer (`internal/core/Service`) is the only entry point for business logic. No adapter bypasses it. This makes behavior consistent regardless of whether a command arrives via CLI, MCP JSON-RPC, or HTTP.

## Memory Architecture Layers

AMM organizes information into five layers, each with a distinct role.

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

AMM uses two orthogonal axes to control where a memory belongs and who can see it.

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

AMM processes information through four stages:

```
Retain --> Reflect --> Compress --> Index
```

1. **Retain**: Raw events and transcripts are appended to the history layer. Writes are synchronous and cheap.
2. **Reflect**: Background workers read new events and extract candidate durable memories (preferences, decisions, facts, open loops, contradiction candidates, episode candidates). Heuristic-first with optional LLM assist.
3. **Compress**: Background workers build leaf summaries over event spans, then session/topic summaries and episodes over leaf summaries. Every summary links back to its source span.
4. **Index**: FTS5 tables are updated via triggers on insert/update/delete. Embeddings and cache are updated by maintenance jobs.

## Retrieval Architecture

### Find, Describe, Expand

AMM retrieval follows a three-step flow:

1. **Find** (`recall`): Returns a thin list of scored `RecallItem` records -- just ID, kind, type, scope, score, and tight description. Low token cost.
2. **Describe** (`describe`): Returns slightly richer metadata for one or more items without the full body.
3. **Expand** (`expand`): Returns the full record plus provenance: a memory expands to its claims and source links; a summary expands to its source span, children, and optionally raw events; an episode expands to linked summaries, outcomes, and unresolved items.

This layered approach keeps ambient recall cheap (thin items only) while allowing drill-down when the agent needs full detail.

### Scoring Formula

Recall uses a 10-signal scoring formula:

```
score =
    0.25 * lexical            -- FTS/BM25 match strength
  + 0.18 * semantic           -- embedding cosine similarity (if enabled)
  + 0.18 * entity_overlap     -- overlap between query entities and memory entities
  + 0.10 * scope_fit          -- how well the memory's scope matches the query context
  + 0.08 * recency            -- how recently the memory was created or confirmed
  + 0.07 * importance         -- stored importance score
  + 0.05 * temporal_validity  -- whether the memory is within its valid_from/valid_to range
  + 0.05 * structural_proximity -- source/provenance overlap with query context
  + 0.04 * freshness          -- computed at query time from temporal fields (not stored)
  - 0.10 * repetition_penalty -- penalty for items recently shown in this session
```

When semantic similarity is disabled (the default in v0), weights are renormalized across the remaining signals.

### Ambient Recall

Ambient recall is the primary retrieval mode. On every turn, the agent runtime:

1. Sends the latest inbound message to AMM
2. AMM extracts entities/topics (exact match, capitalized token heuristics, recent topic cache)
3. Queries across memories, summaries, episodes, and history
4. Merges candidates, scores with the 10-signal formula, ranks
5. Returns a thin packet of 3-7 `RecallItem` records

The result is a compact halo of relevant memory injected into the agent's context at low token cost.

### Repetition Suppression

The `recall_history` table tracks which items have been shown in which sessions. Items recently surfaced receive a repetition penalty during scoring, preventing the same memories from dominating every turn. Recall history rows older than 7 days (configurable) are cleaned up by the `cleanup_recall_history` job.

## Data Flow

```
1. Event ingested
   --> stored in events table (canonical)
   --> FTS trigger populates events_fts (derived)

2. Reflect worker runs
   --> reads unprocessed events
   --> creates candidate memories in memories table
   --> FTS trigger populates memories_fts

3. Compress worker runs
   --> groups events into spans
   --> creates summaries in summaries table
   --> links via summary_edges
   --> FTS trigger populates summaries_fts

4. Recall query arrives
   --> FTS search across memories_fts, summaries_fts, episodes_fts, events_fts
   --> score candidates with 10-signal formula
   --> apply repetition suppression from recall_history
   --> rank and return thin RecallItem list
   --> log shown items to recall_history
```

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
