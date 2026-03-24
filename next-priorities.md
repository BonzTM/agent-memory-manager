# amm Next Priorities

Remaining gaps and improvement areas beyond the current v0 implementation, ordered by impact.

## High Priority

### Scoring weight dead code (41% of retrieval signal)
The retrieval scoring formula allocates 0.18 weight to semantic similarity (embeddings) and 0.18 to entity overlap. Neither signal produces meaningful output today:
- **Semantic similarity**: The `embeddings` table exists but is never populated. No embedding model is integrated. When enabled, the scoring formula should renormalize weights — currently it just adds zero.
- **Entity overlap**: Entities are now auto-created during reflection (heuristic extraction), but the entity-overlap scoring signal in `scoring.go` still uses a basic name-match. Once entity volume grows, consider TF-IDF or frequency-dampened overlap to avoid hub entities dominating recall.

**Next step**: Add an `EmbeddingProvider` interface similar to the `Summarizer` pattern — local model (e.g., `all-MiniLM-L6-v2` via ONNX) or API-backed. Populate embeddings on memory/summary insert. Use cosine similarity in the scoring pipeline.

### Supersession workflows
The `supersedes`, `superseded_by`, and `superseded_at` fields exist on memories. The status filter works. But no code ever sets these fields — there is no supersession workflow.

**What's needed**:
- When `Reflect` creates a new memory that conflicts with an existing one (same subject, same type, different body), mark the old one superseded.
- When `UpdateMemory` changes a memory's body substantively, record the supersession chain.
- `DetectContradictions` should propose supersession candidates, not just flag them.

### LLM-backed summarization quality
The `Summarizer` interface is now in place with a heuristic fallback. The heuristic truncates content rather than summarizing it. Real summarization requires an LLM.

**Next step**: Implement an `LLMSummarizer` behind the same interface. Support OpenAI-compatible APIs via `OPENAI_API_KEY` env var. Use for:
- `Reflect`: Extract structured memory candidates from event batches instead of phrase-cue matching.
- `CompressHistory`: Produce actual summaries instead of truncated concatenations.
- `ConsolidateSessions`: Synthesize session-level and topic-level summaries.

Keep the heuristic as a zero-dependency fallback when no API key is configured.

## Medium Priority

### JSON Schema validation (`spec/v1/`)
The spec calls for `spec/v1` JSON schemas that stay in lockstep with `internal/contracts/v1`. This directory doesn't exist.

**What's needed**:
- JSON Schema files for each command's request/response payloads.
- A validation script (Python or Go) that checks contract types match schemas.
- CI step to enforce lockstep.

### `projects` and `relationships` tables
The spec (§30.1) lists `projects` and `relationships` as canonical tables. Neither exists in migrations. Project scoping works via bare `project_id` strings, which means:
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

## Lower Priority

### Graph-assisted retrieval
The spec (§23.2, §30.2) mentions optional `graph_projection` tables and graph-assisted retrieval. This is explicitly post-v0 but worth tracking.

### Contradiction resolution workflow
`DetectContradictions` currently flags conflicts via keyword overlap heuristic. A real resolution workflow would:
- Present contradictions to the user or agent.
- Allow explicit resolution (pick winner, merge, or archive both).
- Record resolution provenance.

### Temporal validity enforcement
The `valid_from` and `valid_to` fields exist on memories but are never checked during recall. Queries should filter out temporally invalid memories by default and support `as_of` queries for historical truth.

### Privacy-aware filtering
The `privacy_level` field exists but recall doesn't filter by caller context. Surface-aware filtering (spec §32.2) needs a caller identity model.

### Ambient recall entity/topic extraction improvements
Current entity extraction uses capitalized-word heuristics. Improvements:
- NER via a lightweight model.
- Topic extraction from recent conversation context.
- Entity alias resolution against the canonical entity store.

### Decay and promotion automation
`decay_stale_memory` works but `promote_high_value_memories` and `archive_low_salience_session_traces` (spec §26.2) are not implemented.
