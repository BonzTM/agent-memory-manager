# amm Next Priorities

Remaining gaps and improvement areas, ordered by impact.

## Completed Recently

### Semantic scoring + embedding pipeline
Completed.
- Added an `EmbeddingProvider` abstraction and runtime wiring.
- Embeddings are now populated for memory/summary write paths and rebuilt via `rebuild_indexes`.
- Recall scoring now includes semantic similarity via cosine similarity over existing FTS candidates.
- Explain output now includes semantic contribution.

### Supersession workflows
Completed for the originally identified gaps.
- `Reflect` now supersedes conflicting active memories in real time.
- `UpdateMemory` now handles supersession chaining via `Supersedes`.
- `DetectContradictions` now supersedes the older conflicting memory.

### LLM-backed summarization for compress/consolidate
Completed.
- `ConsolidateSessions` now uses `s.summarizer.Summarize()` instead of concatenating and truncating raw event content.

### `projects` and `relationships` tables
Completed.
- Added canonical tables, migration, repository methods, service methods, and adapter wiring.

### End-to-end integration tests
Completed.
- Added CLI integration coverage against a temp DB for init, remember, recall, ingest, status, jobs, and error paths.

### Ingestion policy matching improvements
Completed.
- Added glob/regex matching support.
- Added policy priority ordering.

### Ingestion noise filtering
Completed.
- Noisy events are now conservatively downgraded to `read_only` and tagged so reflect/reprocess skip them.

### Decay and promotion automation
Completed.
- `promote_high_value` and `archive_session_traces` jobs are implemented.

## Remaining Priorities

## High Priority

### Entity-overlap scoring quality
Semantic similarity is now implemented, but entity overlap is still basic string/name matching. Once entity volume grows, consider TF-IDF or frequency-dampened overlap so hub entities do not dominate recall.

## Medium Priority

### JSON Schema validation (`spec/v1/`)
The spec calls for `spec/v1` JSON schemas that stay in lockstep with `internal/contracts/v1`. The directory now exists, but schema coverage and enforcement are still incomplete.

**What's needed**:
- JSON Schema files for each command's request/response payloads.
- A validation script (Python or Go) that checks contract types match schemas.
- CI step to enforce lockstep.

### MCP end-to-end integration tests
CLI integration tests now exist, but there is still no end-to-end coverage for the MCP JSON-RPC protocol.

**What's needed**:
- Start the MCP server in tests, send JSON-RPC messages, assert responses.
- Cover happy paths and error paths for at least: init, remember, recall, ingest, jobs run.

## Lower Priority

### Graph-assisted retrieval
The spec (section 23.2, 30.2) mentions optional `graph_projection` tables and graph-assisted retrieval. This is explicitly post-v0 but worth tracking.

### Contradiction resolution workflow
`DetectContradictions` now helps drive supersession, but there is still no full contradiction-resolution workflow. A real workflow would:
- Present contradictions to the user or agent.
- Allow explicit resolution (pick winner, merge, or archive both).
- Record resolution provenance.

### Temporal validity enforcement
`valid_from` and `valid_to` now participate in recall scoring, but there is still no full `as_of` query workflow or stronger temporal reasoning.

### Privacy-aware filtering
The `privacy_level` field exists but recall doesn't filter by caller context. Surface-aware filtering (spec section 32.2) needs a caller identity model.

### Ambient recall entity/topic extraction improvements
Current entity extraction uses capitalized-word heuristics. Improvements:
- NER via a lightweight model.
- Topic extraction from recent conversation context.
- Entity alias resolution against the canonical entity store.

### Local embedding provider implementation
Completed.
- Implemented `APIEmbeddingProvider` supporting any OpenAI-compatible `/v1/embeddings` endpoint (OpenRouter, Ollama, OpenAI, etc.).
- Separate config from summarizer: `AMM_EMBEDDINGS_ENDPOINT`, `AMM_EMBEDDINGS_API_KEY`, `AMM_EMBEDDINGS_MODEL`.
- Noop provider remains as fallback when embeddings are disabled.
- Default model: `text-embedding-3-small`.

**Remaining**: A fully local ONNX-based provider (e.g., all-MiniLM-L6-v2 via onnxruntime_go) for zero-network embedding. The API provider covers Ollama local use in the meantime.
