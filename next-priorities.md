# amm Next Priorities

Remaining gaps and improvement areas, ordered by impact.

## High Priority

### Scoring weight dead code (41% of retrieval signal)
The retrieval scoring formula allocates 0.18 weight to semantic similarity (embeddings) and 0.18 to entity overlap. Neither signal produces meaningful output today:
- **Semantic similarity**: The `embeddings` table exists but is never populated. No embedding model is integrated. When enabled, the scoring formula should renormalize weights — currently it just adds zero.
- **Entity overlap**: Entities are now auto-created during reflection, but the entity-overlap scoring signal in `scoring.go` still uses a basic name-match. Once entity volume grows, consider TF-IDF or frequency-dampened overlap to avoid hub entities dominating recall.

**Next step**: Add an `EmbeddingProvider` interface similar to the `Summarizer` pattern — local model (e.g., `all-MiniLM-L6-v2` via ONNX) or API-backed. Populate embeddings on memory/summary insert. Use cosine similarity in the scoring pipeline.

### Supersession workflows (partially done)
The `reprocess` and `reprocess_all` jobs now use the supersession fields (`supersedes`, `superseded_by`, `superseded_at`) to mark old memories when new LLM extractions replace them. What's still missing:
- `Reflect` should detect when a new memory conflicts with an existing one (same subject, same type, different body) and supersede the old one in real-time, not just during reprocessing.
- `UpdateMemory` should record the supersession chain when a memory's body changes substantively.
- `DetectContradictions` should propose supersession candidates, not just flag conflicts.

### LLM-backed summarization for compress/consolidate
The `LLMSummarizer` is wired into `Reflect` and `CompressHistory` via `Summarize()`. `ConsolidateSessions` still concatenates event content rather than synthesizing session-level summaries. Wire `s.summarizer.Summarize()` into `ConsolidateSessions` for real cross-event synthesis.

## Medium Priority

### JSON Schema validation (`spec/v1/`)
The spec calls for `spec/v1` JSON schemas that stay in lockstep with `internal/contracts/v1`. This directory doesn't exist.

**What's needed**:
- JSON Schema files for each command's request/response payloads.
- A validation script (Python or Go) that checks contract types match schemas.
- CI step to enforce lockstep.

### `projects` and `relationships` tables
The spec (section 30.1) lists `projects` and `relationships` as canonical tables. Neither exists in migrations. Project scoping works via bare `project_id` strings, which means:
- No project registry or metadata.
- No way to list known projects.
- No relationship tracking between entities.

**Next step**: Add migration v3 with `projects` table (id, name, path, metadata, created_at) and `relationships` table (from_entity_id, to_entity_id, relationship_type, metadata). Add repository + service methods. Wire into CLI/MCP.

### End-to-end integration tests
No tests exercise the full CLI binary or MCP JSON-RPC protocol. Current tests cover service and repository layers but not the adapter glue.

**What's needed**:
- Build the binary, run CLI commands against a temp DB, assert JSON output.
- Send JSON-RPC messages to the MCP server, assert responses.
- Cover happy paths and error paths for at least: init, remember, recall, ingest, jobs run.

### Ingestion policy matching improvements
Policy matching currently only supports exact match on pattern_type + pattern. The spec describes richer matching:
- Glob/regex patterns for session IDs.
- Wildcard source system matching.
- Priority ordering when multiple policies match.

### Ingestion noise filtering
The heuristic reflect job still creates garbage memories from tool output, JSON blobs, and build logs. Even with LLM extraction, these events consume LLM tokens without producing useful memories. Adding ingestion-time classification (or an ingestion policy that auto-tags noisy event kinds) would reduce both cost and memory pollution.

## Lower Priority

### Graph-assisted retrieval
The spec (section 23.2, 30.2) mentions optional `graph_projection` tables and graph-assisted retrieval. This is explicitly post-v0 but worth tracking.

### Contradiction resolution workflow
`DetectContradictions` currently flags conflicts via keyword overlap heuristic. A real resolution workflow would:
- Present contradictions to the user or agent.
- Allow explicit resolution (pick winner, merge, or archive both).
- Record resolution provenance.

### Temporal validity enforcement
The `valid_from` and `valid_to` fields exist on memories but are never checked during recall. Queries should filter out temporally invalid memories by default and support `as_of` queries for historical truth.

### Privacy-aware filtering
The `privacy_level` field exists but recall doesn't filter by caller context. Surface-aware filtering (spec section 32.2) needs a caller identity model.

### Ambient recall entity/topic extraction improvements
Current entity extraction uses capitalized-word heuristics. Improvements:
- NER via a lightweight model.
- Topic extraction from recent conversation context.
- Entity alias resolution against the canonical entity store.

### Decay and promotion automation
`decay_stale_memory` works but `promote_high_value_memories` and `archive_low_salience_session_traces` (spec section 26.2) are not implemented.
