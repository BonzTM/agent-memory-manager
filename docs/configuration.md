# Configuration

AMM can be configured via environment variables or configuration files (JSON/TOML).

## Config File Locations
AMM loads configuration in the following order:
1. **Defaults**: Hardcoded in the binary.
2. **User Config**: `~/.amm/config.json` or `~/.amm/config.toml` (first file found wins).
3. **Environment Variables**: Overrides file/default settings.

---

## Environment Variables

### Core & Storage
| Variable | Description | Default |
|----------|-------------|---------|
| `AMM_STORAGE_BACKEND` | `sqlite` or `postgres` | `sqlite` |
| `AMM_DB_PATH` | Path to the SQLite database file | `~/.amm/amm.db` |
| `AMM_POSTGRES_DSN` | PostgreSQL connection string | _(unset)_ |

### HTTP Server
| Variable | Description | Default |
|----------|-------------|---------|
| `AMM_HTTP_ADDR` | HTTP server listen address | `:8080` |
| `AMM_HTTP_CORS_ORIGINS` | Comma-separated list of allowed CORS origins | _(unset)_ |

### Retrieval
| Variable | Description | Default |
|----------|-------------|---------|
| `AMM_DEFAULT_LIMIT` | Default result limit for non-ambient recall | `10` |
| `AMM_AMBIENT_LIMIT` | Result limit for ambient recall mode | `5` |
| `AMM_ENABLE_SEMANTIC` | Enable semantic vector scoring in recall (requires embeddings to be enabled and populated) | `false` |
| `AMM_ENABLE_EXPLAIN` | Enable explain-recall signal breakdowns | `true` |

### Privacy
| Variable | Description | Default |
|----------|-------------|---------|
| `AMM_DEFAULT_PRIVACY` | Default privacy level for new memories (`private`, `shared`, `public_safe`) | `private` |

### Maintenance (Background Jobs)
| Variable | Description | Default |
|----------|-------------|---------|
| `AMM_AUTO_REFLECT` | Automatically run reflect job after event ingestion | `true` |
| `AMM_AUTO_COMPRESS` | Automatically run compress job | `true` |
| `AMM_AUTO_CONSOLIDATE` | Automatically run consolidate job | `true` |
| `AMM_AUTO_DETECT_CONTRADICTIONS` | Automatically run contradiction detection | `true` |

### Compression
| Variable | Description | Default |
|----------|-------------|---------|
| `AMM_ESCALATION_DETERMINISTIC_MAX_CHARS` | Maximum characters for the Level-3 deterministic truncation fallback in the compression pipeline | `2048` |

### Summarizer (LLM Extraction & Reflection)
Set `AMM_SUMMARIZER_ENDPOINT` and `AMM_SUMMARIZER_API_KEY` to enable LLM-backed memory extraction, NER entity extraction, and relationship inference. Without these, AMM falls back to heuristic extraction.

| Variable | Description | Default |
|----------|-------------|---------|
| `AMM_SUMMARIZER_ENDPOINT` | Base URL for OpenAI-compatible API (e.g. `https://api.openai.com/v1`) | _(unset)_ |
| `AMM_SUMMARIZER_API_KEY` | API key for the extraction/summarization model | _(unset)_ |
| `AMM_SUMMARIZER_MODEL` | Model name for extraction and summarization | `gpt-4o-mini` |
| `AMM_SUMMARIZER_BATCH_SIZE` | Number of events per reprocess/summarization batch | `20` |
| `AMM_REFLECT_BATCH_SIZE` | Number of events claimed per reflect iteration | `100` |
| `AMM_REFLECT_LLM_BATCH_SIZE` | Number of claimed reflect events sent to each LLM analysis call | `20` |
| `AMM_COMPRESS_CHUNK_SIZE` | Events per chunk for history compression | `10` |
| `AMM_COMPRESS_MAX_EVENTS` | Maximum events to compress per job run | `200` |
| `AMM_COMPRESS_BATCH_SIZE` | Number of chunks per LLM compress batch | `15` |
| `AMM_TOPIC_BATCH_SIZE` | Number of topic groups per LLM summarize batch | `15` |
| `AMM_LIFECYCLE_REVIEW_BATCH_SIZE` | Number of memories per lifecycle review batch | `50` |
| `AMM_CROSS_PROJECT_SIMILARITY_THRESHOLD` | Minimum similarity score for cross-project memory transfer | `0.7` |

### Review Model (Optional Separate LLM for Review/Lifecycle)
A separate model can be used for lifecycle review, consolidation, and contradiction detection. Defaults to the summarizer model if unset.

| Variable | Description | Default |
|----------|-------------|---------|
| `AMM_REVIEW_ENDPOINT` | Base URL for the review model API | _(falls back to `AMM_SUMMARIZER_ENDPOINT`)_ |
| `AMM_REVIEW_API_KEY` | API key for the review model | _(falls back to `AMM_SUMMARIZER_API_KEY`)_ |
| `AMM_REVIEW_MODEL` | Model name for review/consolidation tasks | _(falls back to `AMM_SUMMARIZER_MODEL`)_ |

### Embeddings (Semantic Search)
Set `AMM_EMBEDDINGS_ENABLED=true` to generate/store embeddings. Semantic scoring at recall time is separately controlled by `AMM_ENABLE_SEMANTIC`. Supports any OpenAI-compatible embeddings API (OpenAI, Ollama, OpenRouter, etc.).

| Variable | Description | Default |
|----------|-------------|---------|
| `AMM_EMBEDDINGS_ENABLED` | Enable vector embedding generation/storage | `false` |
| `AMM_EMBEDDINGS_ENDPOINT` | Base URL for OpenAI-compatible embeddings API | _(unset)_ |
| `AMM_EMBEDDINGS_API_KEY` | API key for the embeddings provider | _(unset)_ |
| `AMM_EMBEDDINGS_MODEL` | Embedding model name | `text-embedding-3-small` |
| `AMM_EMBEDDINGS_PROVIDER` | Provider label (informational, e.g. `openai`, `ollama`) | _(unset)_ |
| `AMM_EMBEDDING_BATCH_SIZE` | Number of objects per embedding batch | `64` |

---

## JSON Configuration Example

Full reference — all supported keys shown with their defaults:

```json
{
  "storage": {
    "backend": "sqlite",
    "db_path": "~/.amm/amm.db",
    "postgres_dsn": ""
  },
  "http": {
    "addr": ":8080",
    "cors_origins": ""
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
  "compression": {
    "escalation_deterministic_max_chars": 2048
  },
  "summarizer": {
    "endpoint": "https://api.openai.com/v1",
    "api_key": "sk-...",
    "model": "gpt-4o-mini",
    "review_endpoint": "https://api.openai.com/v1",
    "review_api_key": "sk-...",
    "review_model": "gpt-4o",
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
    "enabled": true,
    "provider": "openai",
    "endpoint": "https://api.openai.com/v1",
    "api_key": "sk-...",
    "model": "text-embedding-3-small"
  }
}
```

---

## TOML Configuration Example

Full reference — all supported keys shown with their defaults:

```toml
[storage]
backend = "sqlite"
db_path = "~/.amm/amm.db"
# postgres_dsn = "postgres://user:pass@localhost:5432/amm?sslmode=disable"

[http]
addr = ":8080"
# cors_origins = "*"

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

[compression]
# Maximum characters for the Level-3 deterministic truncation fallback.
# Env: AMM_ESCALATION_DETERMINISTIC_MAX_CHARS
escalation_deterministic_max_chars = 2048

[summarizer]
endpoint = "https://api.openai.com/v1"
api_key = "sk-..."
model = "gpt-4o-mini"
# review_endpoint = "https://api.openai.com/v1"
# review_api_key = "sk-..."
# review_model = "gpt-4o"
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
enabled = true
provider = "openai"
endpoint = "https://api.openai.com/v1"
api_key = "sk-..."
model = "text-embedding-3-small"
```

### Ollama Example (local embeddings)

```toml
[summarizer]
endpoint = "https://api.openai.com/v1"
api_key = "sk-..."
model = "gpt-4o-mini"

[embeddings]
enabled = true
provider = "ollama"
endpoint = "http://localhost:11434/v1"
api_key = "ollama"
model = "nomic-embed-text"
```

---

## Compression Behaviour

The compression pipeline uses three-level escalation to guarantee convergence. All body
summarization calls follow this fallback chain:

1. **Level 1 (Normal)**: LLM summarize at `maxChars` target. Used if output is non-empty and shorter than input.
2. **Level 2 (Aggressive)**: LLM summarize at `maxChars/2` target. Used if level 1 failed to reduce.
3. **Level 3 (Deterministic)**: Truncate to `min(len, maxChars, escalation_deterministic_max_chars)` and append `[Truncated from N chars]`. Always succeeds. No LLM call.

`escalation_deterministic_max_chars` defaults to `2048` and is configurable via `AMM_ESCALATION_DETERMINISTIC_MAX_CHARS` or `[compression].escalation_deterministic_max_chars`.

The `summaries` table includes a `depth` field:
- `depth=0` — leaf summaries (cover raw events) or session summaries
- `depth=1` — topic/condensed summaries (cover leaf summaries)
- `depth=2+` — reserved for future higher condensed levels

---

## Scoring Signals
Recall ranking is determined by a weighted sum of multiple signals:
- **Lexical**: Keyword match score (FTS5/BM25).
- **Semantic**: Vector similarity score (requires embeddings enabled).
- **Entity Overlap**: Matches against known entities in the knowledge graph, with hub dampening.
- **Recency**: Boosts newer information.
- **Importance**: Weighted by assigned importance (manual or LLM-extracted).
- **Freshness**: Computed at query time from temporal fields; decays over a 14-day half-life.
- **Temporal Validity**: Filters and scores by `valid_from`/`valid_to` window.
- **Source Trust**: Reliability multiplier for the originating system.
- **Structural Proximity**: Boosts items closely linked to source history.
- **Repetition Penalty**: Dampens items recently surfaced in the same session.
