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
