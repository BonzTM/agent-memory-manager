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

### Layer D: Canonical Memory Layer
- **Typed durable memory records**
- The authoritative memory substrate stored in the `memories` table.
- Records facts, preferences, decisions, procedures, and more (16 types total).

### Layer E: Derived Index Layer
- **Disposable, rebuildable retrieval aids**
- FTS5 virtual tables for full-text search.
- Optional vector embeddings for semantic similarity.
- Retrieval caches and entity graph projections.

---

## Adapter Layer

AMM exposes its service layer through three primary adapters:

1. **CLI (`amm`)**: For interactive use, shell scripts, and local administration.
2. **MCP (`amm-mcp`)**: Implementation of the Model Context Protocol for direct integration with agent runtimes like Claude Code.
3. **HTTP (`amm-http`)**: A RESTful API server for network-based integration and multi-agent setups.

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

1. **Ingest**: Raw events are appended to the history layer.
2. **Reflect**: Processes events in batches to extract candidate memories (facts, preferences, etc.).
3. **Index**: Generates text indexes and optional vector embeddings.
4. **Compress**: Builds hierarchical summaries over event spans.
5. **Consolidate**: Groups related activity into narrative episodes.
6. **Enrich**: Links memories to canonical entities and builds the relationship graph.

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
