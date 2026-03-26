# Configuration

## Config File Locations

amm loads configuration from the following sources, in order. Later sources override earlier ones:

1. **Defaults** -- hardcoded in `internal/runtime/config.go` via `DefaultConfig()`.
2. **User config file** -- `~/.amm/config.json` or `~/.amm/config.toml`.
3. **Project config file** -- `.amm/config.json` or `.amm/config.toml` (relative to the project root).
4. **Environment variables** -- override any file-based values.

If a config file does not exist, amm silently uses defaults. JSON is auto-detected by a leading `{` character; otherwise the file is parsed as TOML.

---

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `AMM_DB_PATH` | Path to the SQLite database file | `~/.amm/amm.db` |
| `AMM_DEFAULT_LIMIT` | Default result limit for non-ambient recall | `10` |
| `AMM_AMBIENT_LIMIT` | Result limit for ambient recall mode | `5` |
| `AMM_ENABLE_SEMANTIC` | Enable semantic (embedding-based) search | `false` |
| `AMM_ENABLE_EXPLAIN` | Enable explain-recall signal breakdowns | `true` |
| `AMM_DEFAULT_PRIVACY` | Default privacy level for new memories | `private` |
| `AMM_AUTO_REFLECT` | Automatically run reflect worker | `true` |
| `AMM_AUTO_COMPRESS` | Automatically run compress_history worker | `true` |
| `AMM_AUTO_CONSOLIDATE` | Automatically run consolidate_sessions worker | `true` |
| `AMM_AUTO_DETECT_CONTRADICTIONS` | Automatically run contradiction detection | `true` |
| `AMM_SUMMARIZER_ENDPOINT` | Base URL for OpenAI-compatible chat completions API used by the summarizer | _(unset)_ |
| `AMM_SUMMARIZER_API_KEY` | API key for the summarizer endpoint | _(unset)_ |
| `AMM_SUMMARIZER_MODEL` | Model name for extraction/summarization requests | `gpt-4o-mini` |
| `AMM_REVIEW_ENDPOINT` | Optional separate endpoint for review/lifecycle intelligence calls | _(unset; defaults to `AMM_SUMMARIZER_ENDPOINT`)_ |
| `AMM_REVIEW_API_KEY` | API key for review/lifecycle endpoint | _(unset; defaults to `AMM_SUMMARIZER_API_KEY`)_ |
| `AMM_REVIEW_MODEL` | Model for review/lifecycle intelligence tasks | _(unset; defaults to `AMM_SUMMARIZER_MODEL`)_ |
| `AMM_SUMMARIZER_BATCH_SIZE` | Events per summarizer call during `reprocess` | `20` |
| `AMM_REFLECT_BATCH_SIZE` | Unreflected events claimed per reflect loop iteration | `100` |
| `AMM_REFLECT_LLM_BATCH_SIZE` | Triaged reflect events per extraction call | `20` |
| `AMM_LIFECYCLE_REVIEW_BATCH_SIZE` | Memories per lifecycle review intelligence call | `50` |
| `AMM_COMPRESS_CHUNK_SIZE` | Number of events per compression chunk | `10` |
| `AMM_COMPRESS_MAX_EVENTS` | Maximum events allowed in a single compression span | `200` |
| `AMM_COMPRESS_BATCH_SIZE` | Batches of events processed in compression jobs | `15` |
| `AMM_TOPIC_BATCH_SIZE` | Batches of leaf summaries for topic summarization | `15` |
| `AMM_EMBEDDING_BATCH_SIZE` | Items per batch embedding request | `64` |
| `AMM_CROSS_PROJECT_SIMILARITY_THRESHOLD` | Threshold for project-to-global promotion | `0.7` |
| `AMM_EMBEDDINGS_ENABLED` | Enable embeddings provider integration | `false` |
| `AMM_EMBEDDINGS_PROVIDER` | Embeddings provider name | _(unset)_ |
| `AMM_EMBEDDINGS_ENDPOINT` | Base URL for embeddings provider API | _(unset)_ |
| `AMM_EMBEDDINGS_API_KEY` | API key for embeddings provider | _(unset)_ |
| `AMM_EMBEDDINGS_MODEL` | Embeddings model name | _(unset; runtime falls back to `text-embedding-3-small` when endpoint mode is used)_ |

Environment variables accept the same value types as their config file equivalents. Boolean variables accept `true`, `false`, `1`, `0`, `t`, `f` (parsed by Go's `strconv.ParseBool`).

---

## JSON Config Format

```json
{
  "storage": {
    "db_path": "~/.amm/amm.db"
  },
  "retrieval": {
    "default_limit": 10,
    "ambient_limit": 5,
    "enable_semantic": false,
    "enable_explain": true
  },
  "privacy": {
    "default_privacy": "private"
  },
  "maintenance": {
    "auto_reflect": true,
    "auto_compress": true,
    "auto_consolidate": true,
    "auto_detect_contradictions": true
  },
  "summarizer": {
    "endpoint": "https://api.openai.com/v1",
    "api_key": "",
    "model": "gpt-4o-mini",
    "review_endpoint": "",
    "review_api_key": "",
    "review_model": "",
    "batch_size": 20,
    "reflect_batch_size": 100,
    "reflect_llm_batch_size": 20,
    "lifecycle_review_batch_size": 50,
    "compress_chunk_size": 10,
    "compress_max_events": 200,
    "compress_batch_size": 15,
    "topic_batch_size": 15,
    "embedding_batch_size": 64,
    "cross_project_similarity_threshold": 0.7
  },
  "embeddings": {
    "enabled": false,
    "provider": "",
    "endpoint": "",
    "api_key": "",
    "model": ""
  }
}
```

---

## TOML Config Format

```toml
[storage]
db_path = "~/.amm/amm.db"

[retrieval]
default_limit = 10
ambient_limit = 5
enable_semantic = false
enable_explain = true

[privacy]
default_privacy = "private"

[maintenance]
auto_reflect = true
auto_compress = true
auto_consolidate = true
auto_detect_contradictions = true

[summarizer]
endpoint = "https://api.openai.com/v1"
api_key = ""
model = "gpt-4o-mini"
review_endpoint = ""
review_api_key = ""
review_model = ""
batch_size = 20
reflect_batch_size = 100
reflect_llm_batch_size = 20
lifecycle_review_batch_size = 50
compress_chunk_size = 10
compress_max_events = 200
compress_batch_size = 15
topic_batch_size = 15
embedding_batch_size = 64
cross_project_similarity_threshold = 0.7

[embeddings]
enabled = false
provider = ""
endpoint = ""
api_key = ""
model = ""
```

amm supports a flat subset of TOML: `[section]` headers and `key = value` pairs. Nested tables and arrays are not supported. Values can be quoted strings, bare integers, or bare booleans.

---

## Config Sections

### storage

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `db_path` | string | `~/.amm/amm.db` | Path to the SQLite database. Parent directories are created automatically on `amm init`. |

### retrieval

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `default_limit` | int | `10` | Maximum items returned for non-ambient recall modes (facts, episodes, project, entity, history, hybrid, timeline, active). |
| `ambient_limit` | int | `5` | Maximum items returned for ambient recall. Keep this low (3-7) to avoid overloading agent context. |
| `enable_semantic` | bool | `false` | Enable embedding-based semantic search. Requires an embedding provider. When disabled, retrieval uses FTS5 full-text search only. |
| `enable_explain` | bool | `true` | Enable the `explain_recall` command, which returns per-signal score breakdowns for debugging retrieval quality. |

### privacy

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `default_privacy` | string | `private` | Default privacy level assigned to new memories when none is specified. Valid values: `private`, `shared`, `public_safe`. |

### maintenance

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `auto_reflect` | bool | `true` | Enable automatic memory extraction from events. |
| `auto_compress` | bool | `true` | Enable automatic history compression into summaries. |
| `auto_consolidate` | bool | `true` | Enable automatic session consolidation. |
| `auto_detect_contradictions` | bool | `true` | Enable automatic contradiction detection between memories. |

### summarizer

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `endpoint` | string | _(unset)_ | Base URL for an OpenAI-compatible chat completions API. When set together with `api_key`, amm uses LLM-backed extraction for the `reflect` and `compress_history` workers instead of the built-in heuristic. Compatible with OpenAI, Anthropic (via OpenAI-compat endpoint), Ollama (`http://localhost:11434/v1`), vLLM, LM Studio, or any provider exposing `/v1/chat/completions`. |
| `api_key` | string | _(unset)_ | Bearer token sent in the `Authorization` header. Required for most hosted providers. For local models (Ollama, LM Studio) set to any non-empty string. |
| `model` | string | _(unset)_ | Model identifier passed in extraction/summarization requests. If unset, runtime falls back to `gpt-4o-mini` when constructing an LLM summarizer. |
| `review_endpoint` | string | _(unset)_ | Optional separate endpoint for lifecycle/review intelligence calls (`ReviewMemories`, `ConsolidateNarrative`). If unset, review calls use the primary summarizer endpoint. |
| `review_api_key` | string | _(unset)_ | API key for `review_endpoint`. If unset, runtime falls back to `api_key`. |
| `review_model` | string | _(unset)_ | Model for lifecycle/review tasks. If unset, runtime falls back to `model`, then `gpt-4o-mini`. |
| `batch_size` | int | `20` | Number of events per summarizer call during `reprocess` and `reprocess_all` jobs. Higher values use fewer API calls but reduce extraction quality. Recommended range: 10-50. |
| `reflect_batch_size` | int | `100` | Number of unreflected events claimed per reflect loop iteration. |
| `reflect_llm_batch_size` | int | `20` | Number of triaged reflect events per extraction call. |
| `lifecycle_review_batch_size` | int | `50` | Number of memories per lifecycle review intelligence call. |
| `compress_chunk_size` | int | `10` | Number of events per compression chunk. |
| `compress_max_events` | int | `200` | Maximum events in a single compression span. |
| `compress_batch_size` | int | `15` | Batches of event chunks processed per compression call. |
| `topic_batch_size` | int | `15` | Batches of leaf summaries processed for topic summarization. |
| `embedding_batch_size` | int | `64` | Number of items per batch embedding request. Recommend `256` for large datasets. |
| `cross_project_similarity_threshold` | float | `0.7` | Semantic similarity threshold for promoting project memories to global scope. |

### embeddings

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `enabled` | bool | `false` | Enable the embedding provider. When enabled, amm generates vector embeddings for memories and summaries, enabling semantic search in recall. |
| `endpoint` | string | _(unset)_ | Base URL for an OpenAI-compatible embeddings API. Compatible with OpenAI, OpenRouter, Ollama (`http://localhost:11434/v1`), LiteLLM, or any provider exposing `/v1/embeddings`. |
| `api_key` | string | _(unset)_ | Bearer token sent in the `Authorization` header. For local models (Ollama) set to any non-empty string or leave empty. |
| `provider` | string | _(unset)_ | Provider name tag stored with embedding records. Informational only. |
| `model` | string | _(unset)_ | Model identifier passed in the request body. If `endpoint` mode is used and this is empty, runtime falls back to `text-embedding-3-small`. |

When `endpoint` is set and `enabled` is `true`, amm uses the API provider to generate embeddings on every memory and summary write, and during `rebuild_indexes`. Embeddings are stored in the `embeddings` table and used for semantic similarity scoring during recall (when `enable_semantic` is also `true`).

When `endpoint` is unset, amm falls back to a noop provider that generates empty vectors. Recall still works via FTS5 full-text search — semantic scoring is simply skipped.

Embedding generation is best-effort: if an API call fails, the canonical memory/summary write still succeeds. Failed embeddings are logged at WARN level and can be backfilled by running `amm jobs run rebuild_indexes`.

---

When `endpoint` and `api_key` are both unset or empty, amm falls back to a built-in heuristic summarizer that uses phrase-cue matching and truncation. This is the zero-dependency default — no external API calls are made.

When LLM extraction is enabled, the `reflect` worker sends recent events to the LLM with a structured extraction prompt and parses typed `MemoryCandidate` records from the response. The `reprocess` and `reprocess_all` jobs use batch extraction — sending `batch_size` events per LLM call for cross-event deduplication. Each event's content is truncated to 1500 characters before being sent to the LLM.

If any LLM call fails (network error, timeout, rate limit, malformed response), that batch automatically falls back to the heuristic — no data is lost.

When `review_endpoint` is configured, lifecycle review and narrative-consolidation intelligence calls are routed to that endpoint/model; otherwise they use the primary summarizer endpoint/model.

Memories created by LLM extraction are tagged with `metadata.extraction_method = "llm"`. The `reprocess` job uses this tag to skip events that have already been processed by the LLM, while `reprocess_all` ignores it and reprocesses everything. Old memories are marked `superseded` and linked to their replacement via the `superseded_by` field.

---

## Scoring Weights

Recall uses a weighted multi-signal formula with dynamic renormalization and feedback-based learning.

### Active Signals

Positive signals:

- lexical
- extraction_quality
- semantic (optional; only when embeddings are available for both query and candidate)
- entity_overlap
- scope_fit
- recency
- importance
- temporal_validity
- structural_proximity
- freshness

Penalty:

- repetition_penalty

### Endgame scoring behavior

- **Extraction quality signal**: provisional extractions are downweighted relative to verified/upgraded memories.
- **Graph-aware entity overlap**: entity overlap uses weighted query entities expanded by aliases and related entities (via entity graph traversal/projection), not only direct text tokens.
- **Learned weight adjustments**: `update_ranking_weights` applies Bayesian updates to selected weights using access stats + `relevance_feedback` (`expanded` actions).
- **Weights loaded from DB**: service initialization loads scoring weights from recent completed `update_ranking_weights` jobs when available; otherwise defaults are used.

### Formula

```
final_score =
    wLexical * lexical
  + wExtractionQuality * extraction_quality
  + wSemantic * semantic
  + wEntityOverlap * entity_overlap
  + wScopeFit * scope_fit
  + wRecency * recency
  + wImportance * importance
  + wTemporalValidity * temporal_validity
  + wStructuralProximity * structural_proximity
  + wFreshness * freshness
  - wRepetitionPenalty * repetition_penalty
```

Result is clamped to `[0.0, 1.0]`. Positive signals are renormalized when optional signals are unavailable for a candidate.

### Tuning

- For immediate deterministic changes, adjust defaults in `internal/service/scoring.go`.
- For behavior learned from usage, run `update_ranking_weights` periodically to persist updated weights into job results for next startup load.
