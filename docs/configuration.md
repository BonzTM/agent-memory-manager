# Configuration

AMM can be configured via environment variables or configuration files (JSON/TOML).

## Config File Locations
AMM loads configuration in the following order:
1. **Defaults**: Hardcoded in the binary.
2. **User Config**: `~/.amm/config.json` or `~/.amm/config.toml`.
3. **Project Config**: `.amm/config.json` or `.amm/config.toml` in the project root.
4. **Environment Variables**: Overrides all file-based settings.

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
| `AMM_EMBEDDINGS_ENABLED` | Enable vector-based recall | `false` |
| `AMM_ENABLE_EXPLAIN` | Enable explain-recall signal breakdowns | `true` |

### Summarizer (LLM Extraction)
| Variable | Description | Default |
|----------|-------------|---------|
| `AMM_SUMMARIZER_ENDPOINT` | Base URL for OpenAI-compatible API | _(unset)_ |
| `AMM_SUMMARIZER_API_KEY` | API key for the summarizer | _(unset)_ |
| `AMM_SUMMARIZER_MODEL` | Model name for extraction/summarization | `gpt-4o-mini` |

---

## JSON Configuration Example
```json
{
  "storage": {
    "backend": "sqlite",
    "db_path": "~/.amm/amm.db"
  },
  "http": {
    "addr": ":8080",
    "cors_origins": "*"
  },
  "retrieval": {
    "default_limit": 10,
    "ambient_limit": 5,
    "enable_semantic": true
  },
  "summarizer": {
    "endpoint": "https://api.openai.com/v1",
    "api_key": "sk-...",
    "model": "gpt-4o-mini"
  }
}
```

---

## Scoring Signals
Recall ranking is determined by a weighted sum of multiple signals:
- **Lexical**: Keyword match score (FTS5).
- **Semantic**: Vector similarity score.
- **Entity Overlap**: Matches against known entities in the knowledge graph.
- **Recency**: Boosts newer information.
- **Importance**: Weighted by assigned importance (manual or LLM).
- **Source Trust**: Reliability multiplier for the originating system.
- **Kind Boost**: Multiplier based on item kind (e.g., boosting memories over raw history).
- **Anti-Hub Dampening**: Reduces noise from overly common entities.
- **Repetition Penalty**: Dampens items recently seen in the same session.
