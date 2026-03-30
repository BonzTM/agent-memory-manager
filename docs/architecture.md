# amm Architecture

Detailed architecture reference for the Agent Memory Manager.

## System Overview

AMM is a Go-based system that provides persistent, typed, temporal memory for agents. It follows these core principles:

- **API-First**: A single `Service` interface defines all business logic. CLI, MCP, and HTTP are thin adapters.
- **Pluggable Storage**: Supports SQLite (local, single file) and PostgreSQL (networked, high-concurrency).
- **Single Binary Distribution**: `go build` produces self-contained executables for the CLI (`amm`), MCP (`amm-mcp`), and HTTP server (`amm-http`).
- **Standard Library First**: Minimal dependencies, relying on Go standard library and mature drivers (`modernc.org/sqlite`, `lib/pq`).

The service layer (`internal/core/Service`) is the only entry point for business logic. This ensures consistent behavior regardless of the interface used.

## Memory Architecture Layers

AMM organizes information into five layers, from ephemeral state to durable truth.

### Layer A: Working Memory
- **Ephemeral, runtime-only**
- Holds current turn state: active query, recent topic cache, entity extraction results.
- Feeds into ambient recall for scoring and entity matching.

### Layer B: History Layer
- **Append-only raw events and transcripts**
- The complete, unmodified archive of every interaction stored in the `events` table.
- Source material for reflection and compression workers.

### Layer C: Compression Layer
- **Summaries linked to source spans**
- Built by compression workers from raw history.
- Hierarchy is tracked via `summary_edges`, enabling drill-down from high-level summaries to raw events.
- Summaries carry a `depth` field (0 = leaf/session, 1 = topic/condensed, 2+ = higher condensed) enabling efficient DAG queries by level.
- All body summarization uses three-level escalation (normal → aggressive → deterministic truncate) guaranteeing convergence — the output is always shorter than the input.

### Layer D: Canonical Memory Layer
- **Typed durable memory records**
- The authoritative memory substrate stored in the `memories` table.
- Records facts, preferences, decisions, procedures, and more (16 types total).

### Layer E: Derived Index Layer
- **Disposable, rebuildable retrieval aids**
- FTS5 virtual tables for full-text search.
- Optional vector embeddings for semantic similarity.
- Retrieval caches and entity graph projections.
- **Grouped Search (Grep)**: Provides pattern-based retrieval with results grouped by canonical item, bypassing semantic scoring for high-precision exact matches.

---

## Adapter Layer

AMM exposes its service layer through three primary adapters:

1. **CLI (`amm`)**: For interactive use, shell scripts, and local administration.
2. **MCP (`amm-mcp`)**: Implementation of the Model Context Protocol (stdio) for direct integration with agent runtimes.
3. **HTTP (`amm-http`)**: A dual-purpose RESTful API and MCP-over-HTTP server.
   - **REST API**: Standard endpoints for all service methods.
   - **MCP-over-HTTP**: Streamable HTTP transport (using `mcp-go`) mounted at `/v1/mcp`.
   - **Documentation**: OpenAPI 3.0 spec at `/openapi.json` and Swagger UI at `/swagger/`.

---

## Storage Backends

AMM supports two primary storage engines, selectable via the `AMM_STORAGE_BACKEND` environment variable.

### SQLite (Default)
- Ideal for local use, single-user agents, and sidecars.
- Zero-configuration; stored in a single `.db` file.
- Uses FTS5 for high-performance text search.

### PostgreSQL
- Recommended for multi-agent systems, shared memory, and high-concurrency environments.
- Supports robust transactions and concurrent writers.
- Requires `pgroonga` or `pg_trgm` for advanced text search parity with FTS5.

---

## Processing Pipeline

AMM runs a staged intelligence pipeline to extract knowledge from raw history:

1. **Ingest**: Raw events are appended to the history layer. Ingestion policies filter events before storage — events matching an `ignore` policy are dropped entirely, while `read_only` events are stored but skip extraction. It is **strongly recommended** to configure `ignore` policies for `tool_call` and `tool_result` event kinds to prevent tool invocation JSON from polluting extracted memories.
2. **Reflect**: Processes events in batches to extract candidate memories (facts, preferences, etc.).
3. **Index**: Generates text indexes and optional vector embeddings.
4. **Compress**: Builds hierarchical summaries over event spans. Uses three-level escalation (normal → aggressive → deterministic truncate) to guarantee convergence at every level (leaf, topic, session).
5. **Consolidate**: Groups related activity into narrative episodes.
6. **Enrich**: Links memories to canonical entities and builds the relationship graph.
7. **Review**: LLM-powered batch review for decay, promote, and contradiction detection. Lifecycle reviews now persist contradictions as typed memories.

### Compression Convergence Guarantee

Every summarization call in the compression pipeline uses `summarizeWithEscalation`, which enforces:

| Level | Strategy | Condition to use |
|-------|----------|-----------------|
| 1 | LLM summarize, target `maxChars` | Output non-empty and shorter than input |
| 2 | LLM summarize, target `maxChars/2` | Output non-empty and shorter than input |
| 3 | Deterministic truncate to `min(len, maxChars, escalation_deterministic_max_chars)` + `[Truncated from N chars]` | Always — no LLM call |

This means compaction can never produce output longer than input, regardless of LLM behaviour.

---

### Context Window Assembly

The Context Window Assembly service provides a unified view of the most important recent information for an agent. It combines:
1. **Topic Summaries**: High-level recaps for earlier activity.
2. **Fresh Events**: The last N raw events from the current session.

The window is assembled chronologically: summaries first, then fresh events.

---

## Retrieval & Scoring

### Scoring Formula
Recall uses a weighted multi-signal formula to rank results:
- **Lexical**: FTS5 text match score.
- **Semantic**: Vector similarity (if enabled).
- **Entity Overlap**: Graph-aware overlap with query entities.
- **Recency/Freshness**: Time-based decay of older information.
- **Importance**: Manual or LLM-assigned importance weight.
- **Source Trust**: Multiplier based on the reliability of the originating system.
- **Anti-Hub Dampening**: Penalizes overly common entities to reduce noise.

### Find, Describe, Expand
1. **Find** (`recall`): Returns a thin list of scored IDs and summaries.
2. **Describe** (`describe`): Returns metadata for one or more items.
3. **Expand** (`expand`): Returns the full record, claims, and provenance links.

---

## Key Invariants

1. **Service layer is the only entry point.** No adapter bypasses the core logic.
2. **Canonical tables are truth.** Derived indexes (FTS, embeddings) can always be rebuilt.
3. **History is append-only.** Raw events are never modified, ensuring a perfect audit trail.
4. **Schema changes are forward-only.** All modifications use the built-in migration system.
5. **Compression always converges.** The three-level escalation guarantee means summarization can never produce output longer than its input.
