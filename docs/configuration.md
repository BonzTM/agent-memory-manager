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
| `AMM_SUMMARIZER_MODEL` | Model name to use for extraction and summarization | `gpt-4o-mini` |
| `AMM_SUMMARIZER_BATCH_SIZE` | Number of events per summarizer call during `reprocess`/`reprocess_all` jobs | `20` |
| `AMM_EMBEDDINGS_ENABLED` | Enable embeddings provider integration | `false` |
| `AMM_EMBEDDINGS_PROVIDER` | Embeddings provider name | _(unset)_ |
| `AMM_EMBEDDINGS_ENDPOINT` | Base URL for embeddings provider API | _(unset)_ |
| `AMM_EMBEDDINGS_API_KEY` | API key for embeddings provider | _(unset)_ |
| `AMM_EMBEDDINGS_MODEL` | Embeddings model name | _(unset)_ |

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
    "batch_size": 20
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
batch_size = 20

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
| `default_privacy` | string | `private` | Default privacy level assigned to new memories when none is specified. Valid values: `private`, `shared`, `public`. |

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
| `model` | string | `gpt-4o-mini` | Model identifier passed in the request body. Use a cheap, fast model — extraction is structured JSON output, not creative writing. Good defaults: `gpt-4o-mini` (OpenAI), `claude-3-5-haiku-latest` (Anthropic), `llama3.2` (Ollama). |
| `batch_size` | int | `20` | Number of events per summarizer call during `reprocess` and `reprocess_all` jobs. Higher values use fewer API calls but reduce extraction quality. Recommended range: 10-50. |

### embeddings

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `enabled` | bool | `false` | Enable the embedding provider. When enabled, amm generates vector embeddings for memories and summaries, enabling semantic search in recall. |
| `endpoint` | string | _(unset)_ | Base URL for an OpenAI-compatible embeddings API. Compatible with OpenAI, OpenRouter, Ollama (`http://localhost:11434/v1`), LiteLLM, or any provider exposing `/v1/embeddings`. |
| `api_key` | string | _(unset)_ | Bearer token sent in the `Authorization` header. For local models (Ollama) set to any non-empty string or leave empty. |
| `provider` | string | _(unset)_ | Provider name tag stored with embedding records. Informational only. |
| `model` | string | `text-embedding-3-small` | Model identifier passed in the request body. Good defaults: `text-embedding-3-small` (OpenAI/OpenRouter), `all-minilm` (Ollama), `nomic-embed-text` (Ollama). |

When `endpoint` is set and `enabled` is `true`, amm uses the API provider to generate embeddings on every memory and summary write, and during `rebuild_indexes`. Embeddings are stored in the `embeddings` table and used for semantic similarity scoring during recall (when `enable_semantic` is also `true`).

When `endpoint` is unset, amm falls back to a noop provider that generates empty vectors. Recall still works via FTS5 full-text search — semantic scoring is simply skipped.

Embedding generation is best-effort: if an API call fails, the canonical memory/summary write still succeeds. Failed embeddings are logged at WARN level and can be backfilled by running `amm jobs run rebuild_indexes`.

---

When `endpoint` and `api_key` are both unset or empty, amm falls back to a built-in heuristic summarizer that uses phrase-cue matching and truncation. This is the zero-dependency default — no external API calls are made.

When LLM extraction is enabled, the `reflect` worker sends recent events to the LLM with a structured extraction prompt and parses typed `MemoryCandidate` records from the response. The `reprocess` and `reprocess_all` jobs use batch extraction — sending `batch_size` events per LLM call for cross-event deduplication. Each event's content is truncated to 1500 characters before being sent to the LLM.

If any LLM call fails (network error, timeout, rate limit, malformed response), that batch automatically falls back to the heuristic — no data is lost.

Memories created by LLM extraction are tagged with `metadata.extraction_method = "llm"`. The `reprocess` job uses this tag to skip events that have already been processed by the LLM, while `reprocess_all` ignores it and reprocesses everything. Old memories are marked `superseded` and linked to their replacement via the `superseded_by` field.

---

## Scoring Weights

amm uses a multi-signal scoring formula to rank recall results. Each candidate item is evaluated against 9 signals, weighted and summed to produce a final score between 0.0 and 1.0.

### Signal Weights (v0, semantic disabled)

With semantic search disabled (the current default), the 8 positive signals are renormalized so their weights sum to 0.75. The repetition penalty is a flat deduction, not part of the normalized sum.

| Signal | Weight | Description |
|--------|--------|-------------|
| **Lexical** | ~0.329 | FTS5 result position. Position 0 scores 1.0, decaying as `1 / (1 + position * 0.2)`. |
| **Entity Overlap** | ~0.237 | Fraction of query entities found in the item's subject, body, tight_description, and tags. Entity extraction uses capitalized-token heuristics. |
| **Scope Fit** | ~0.132 | How well the item's scope matches the query context. Project-matched items score 1.0; global items score 0.5 when a project context is set. |
| **Recency** | ~0.105 | Exponential decay from the item's most recent timestamp. Half-life: 14 days. |
| **Importance** | ~0.092 | The item's stored importance value (0.0-1.0), passed through directly. |
| **Temporal Validity** | ~0.066 | 1.0 if the item is still valid (not expired, not superseded). 0.5 if superseded. 0.0 if `valid_to` is in the past. |
| **Structural Proximity** | ~0.066 | 1.0 if the item has source event links. 0.5 otherwise. Rewards well-provenance items. |
| **Freshness** | ~0.053 | Exponential decay from the item's last update or confirmation time. Same 14-day half-life as recency. |
| **Repetition Penalty** | -0.10 | Deducted if the item was shown in the current session. Binary: 1.0 (shown) or 0.0 (not shown). |

### Formula

```
final_score = wLexical * lexical
            + wEntityOverlap * entity_overlap
            + wScopeFit * scope_fit
            + wRecency * recency
            + wImportance * importance
            + wTemporalValidity * temporal_validity
            + wStructuralProximity * structural_proximity
            + wFreshness * freshness
            - wRepetitionPenalty * repetition_penalty
```

The result is clamped to `[0.0, 1.0]`.

### When Semantic Search Is Enabled

Once semantic search is enabled (`enable_semantic: true`), the original weight distribution applies and a 10th signal (semantic similarity, weight 0.18) is added. The other positive weights return to their pre-renormalization values with a total positive sum of 0.75.

### Tuning

The weights are currently hardcoded in `internal/service/scoring.go`. To adjust retrieval behavior:

- Increase `wRecency` to favor recent items over historically relevant ones.
- Increase `wEntityOverlap` to favor items that mention the same entities as the query.
- Increase `wRepetitionPenalty` to more aggressively suppress repeated items.
- Decrease `wLexical` if FTS ranking does not align well with your content patterns.

Future versions may expose these weights as configuration.
