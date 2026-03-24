# amm v0 Technical Blueprint
## Agent Memory Manager
### Buildable first implementation plan

## Status
Draft v0.4 blueprint

## Purpose
Turn the amm product/spec into something buildable:
- storage model
- command contract
- API surface
- runtime flow
- maintenance jobs
- ranking inputs
- integration shape

This blueprint assumes:

- **amm is its own project**
- amm is **not** defined relative to ACM
- amm may integrate with many runtimes and systems, but none of them define its identity

---

# 1. v0 design goals

## Must-have
1. Local-first persistent memory
2. SQLite as source of truth
3. Raw history/event ingestion
4. Typed memory records
5. Scoped memory (`global`, `project`, `session`) with orthogonal privacy levels
6. Ambient recall packet
7. Deep recall by id
8. Session/episode summaries
9. Provenance links
10. Background maintenance jobs
11. Rebuildable indexes
12. Summary/source expansion

## Nice-to-have
1. Embeddings
2. Entity linking
3. Contradiction detection
4. Supersession support
5. Explain-recall endpoint
6. MCP wrapper
7. Summary hierarchy traversal

## Not in v0
1. Full graph DB
2. Learned ranking models
3. Distributed cluster mode
4. Heavy UI/dashboard
5. Autonomous self-editing memory blocks
6. Enterprise multi-tenant nonsense

---

# 2. System architecture

## 2.1 Main components

### A. Canonical store
SQLite database containing:
- raw events
- summaries
- memories
- claims
- entities
- episodes
- artifacts
- jobs

### B. Derived index layer
Rebuildable retrieval aids:
- FTS5 indexes
- optional embeddings
- optional graph projection
- retrieval cache
- recall history

### C. Ingestion layer
Accepts:
- raw events
- transcripts
- explicit memory commits
- artifacts
- external system events

### D. Reflection workers
Convert history into:
- candidate facts
- decisions
- preferences
- episodes
- open loops
- contradictions
- active context

### E. Compression workers
Convert raw history into:
- leaf summaries
- session summaries
- topic summaries
- higher-level summaries

### F. Retrieval engine
Returns:
- ambient recall packets
- explicit recall results
- describe responses
- expand responses
- explain-recall metadata

### G. Maintenance scheduler
Runs:
- consolidation
- contradiction detection
- dedupe
- decay
- reindexing
- integrity repair

---

# 3. Storage design

## 3.1 Database path
Suggested defaults:

- user/global mode: `~/.amm/amm.db`
- project-local mode: `.amm/amm.db`
- override via config/env/flag

## 3.2 Canonical vs derived
Canonical tables are truth.
Derived tables can be rebuilt.

---

# 4. Database schema

## 4.1 Canonical tables

## `events`
Append-only raw interaction history.

```sql
CREATE TABLE events (
id TEXT PRIMARY KEY,
kind TEXT NOT NULL,
source_system TEXT NOT NULL,
surface TEXT,
session_id TEXT,
project_id TEXT,
-- workspace_id deferred to post-v0
agent_id TEXT,
actor_type TEXT,
actor_id TEXT,
privacy_level TEXT NOT NULL DEFAULT 'private',
content TEXT NOT NULL,
metadata_json TEXT NOT NULL DEFAULT '{}',
hash TEXT,
occurred_at TEXT NOT NULL,
ingested_at TEXT NOT NULL
);
```

Indexes:
```sql
CREATE INDEX idx_events_kind ON events(kind);
CREATE INDEX idx_events_session_id ON events(session_id);
CREATE INDEX idx_events_project_id ON events(project_id);
CREATE INDEX idx_events_occurred_at ON events(occurred_at);
```

---

## `summaries`
Compression layer objects over history.

```sql
CREATE TABLE summaries (
id TEXT PRIMARY KEY,
kind TEXT NOT NULL, -- leaf|session|topic|episode|condensed
scope TEXT NOT NULL,
project_id TEXT,
-- workspace_id deferred to post-v0
session_id TEXT,
agent_id TEXT,
title TEXT,
body TEXT NOT NULL,
tight_description TEXT NOT NULL,
privacy_level TEXT NOT NULL DEFAULT 'private',
source_span_json TEXT NOT NULL DEFAULT '{}',
metadata_json TEXT NOT NULL DEFAULT '{}',
created_at TEXT NOT NULL,
updated_at TEXT NOT NULL
);
```

Indexes:
```sql
CREATE INDEX idx_summaries_kind ON summaries(kind);
CREATE INDEX idx_summaries_scope ON summaries(scope);
CREATE INDEX idx_summaries_project_id ON summaries(project_id);
CREATE INDEX idx_summaries_session_id ON summaries(session_id);
```

---

## `summary_edges`
Optional hierarchy/graph for summary expansion.

```sql
CREATE TABLE summary_edges (
parent_summary_id TEXT NOT NULL,
child_kind TEXT NOT NULL, -- summary|event
child_id TEXT NOT NULL,
edge_order INTEGER,
PRIMARY KEY(parent_summary_id, child_kind, child_id)
);
```

Indexes:
```sql
CREATE INDEX idx_summary_edges_child ON summary_edges(child_kind, child_id);
```

---

## `memories`
Canonical typed memory records.

```sql
CREATE TABLE memories (
id TEXT PRIMARY KEY,
type TEXT NOT NULL,
scope TEXT NOT NULL,
project_id TEXT,
-- workspace_id deferred to post-v0
session_id TEXT,
agent_id TEXT,
subject TEXT,
body TEXT NOT NULL,
tight_description TEXT NOT NULL,
confidence REAL NOT NULL DEFAULT 0.5,
importance REAL NOT NULL DEFAULT 0.5,
privacy_level TEXT NOT NULL DEFAULT 'private',
status TEXT NOT NULL DEFAULT 'active',
observed_at TEXT,
created_at TEXT NOT NULL,
updated_at TEXT NOT NULL,
valid_from TEXT,
valid_to TEXT,
last_confirmed_at TEXT,
supersedes TEXT,
superseded_by TEXT,
superseded_at TEXT,
source_event_ids_json TEXT NOT NULL DEFAULT '[]',
source_summary_ids_json TEXT NOT NULL DEFAULT '[]',
source_artifact_ids_json TEXT NOT NULL DEFAULT '[]',
tags_json TEXT NOT NULL DEFAULT '[]',
metadata_json TEXT NOT NULL DEFAULT '{}'
);
```

Indexes:
```sql
CREATE INDEX idx_memories_type ON memories(type);
CREATE INDEX idx_memories_scope ON memories(scope);
CREATE INDEX idx_memories_project_id ON memories(project_id);
CREATE INDEX idx_memories_status ON memories(status);
CREATE INDEX idx_memories_observed_at ON memories(observed_at);
```

### Freshness (computed, not stored)

`freshness` is **not** a column. It is computed at query time from existing temporal fields:

```sql
-- v0 freshness score: 0.0 (stale) to 1.0 (fresh)
-- Uses the most recent of: last_confirmed_at, updated_at, observed_at
-- Half-life: 14 days (configurable)
SELECT *,
  EXP(-0.693 * (JULIANDAY('now') - JULIANDAY(
    COALESCE(last_confirmed_at, updated_at, observed_at, created_at)
  )) / 14.0) AS freshness
FROM memories;
```

**Rationale:** Storing freshness as a column creates stale metadata about staleness. Computing it from the temporal fields that already exist keeps it always correct with zero maintenance cost.

---

## `claims`
Structured atomic claims.

```sql
CREATE TABLE claims (
id TEXT PRIMARY KEY,
memory_id TEXT NOT NULL,
subject_entity_id TEXT,
predicate TEXT NOT NULL,
object_value TEXT,
object_entity_id TEXT,
confidence REAL NOT NULL DEFAULT 0.5,
source_event_id TEXT,
source_summary_id TEXT,
observed_at TEXT,
valid_from TEXT,
valid_to TEXT,
metadata_json TEXT NOT NULL DEFAULT '{}',
FOREIGN KEY(memory_id) REFERENCES memories(id)
);
```

Indexes:
```sql
CREATE INDEX idx_claims_memory_id ON claims(memory_id);
CREATE INDEX idx_claims_subject_entity_id ON claims(subject_entity_id);
CREATE INDEX idx_claims_predicate ON claims(predicate);
```

---

## `entities`
Canonical entities.

```sql
CREATE TABLE entities (
id TEXT PRIMARY KEY,
type TEXT NOT NULL,
canonical_name TEXT NOT NULL,
aliases_json TEXT NOT NULL DEFAULT '[]',
description TEXT,
metadata_json TEXT NOT NULL DEFAULT '{}',
created_at TEXT NOT NULL,
updated_at TEXT NOT NULL
);
```

Indexes:
```sql
CREATE INDEX idx_entities_type ON entities(type);
CREATE INDEX idx_entities_canonical_name ON entities(canonical_name);
```

---

## `memory_entities`
Join table.

```sql
CREATE TABLE memory_entities (
memory_id TEXT NOT NULL,
entity_id TEXT NOT NULL,
role TEXT,
PRIMARY KEY(memory_id, entity_id)
);
```

---

## `episodes`
Narrative memory units.

```sql
CREATE TABLE episodes (
id TEXT PRIMARY KEY,
title TEXT NOT NULL,
summary TEXT NOT NULL,
tight_description TEXT NOT NULL,
scope TEXT NOT NULL,
project_id TEXT,
-- workspace_id deferred to post-v0
session_id TEXT,
importance REAL NOT NULL DEFAULT 0.5,
privacy_level TEXT NOT NULL DEFAULT 'private',
started_at TEXT,
ended_at TEXT,
source_span_json TEXT NOT NULL DEFAULT '{}',
source_summary_ids_json TEXT NOT NULL DEFAULT '[]',
participants_json TEXT NOT NULL DEFAULT '[]',
related_entities_json TEXT NOT NULL DEFAULT '[]',
outcomes_json TEXT NOT NULL DEFAULT '[]',
unresolved_items_json TEXT NOT NULL DEFAULT '[]',
metadata_json TEXT NOT NULL DEFAULT '{}',
created_at TEXT NOT NULL,
updated_at TEXT NOT NULL
);
```

---

## `artifacts`
Ingested non-message source material.

```sql
CREATE TABLE artifacts (
id TEXT PRIMARY KEY,
kind TEXT NOT NULL,
source_system TEXT,
project_id TEXT,
path TEXT,
content TEXT,
metadata_json TEXT NOT NULL DEFAULT '{}',
created_at TEXT NOT NULL
);
```

---

## `jobs`
Queue/history for workers.

```sql
CREATE TABLE jobs (
id TEXT PRIMARY KEY,
kind TEXT NOT NULL,
status TEXT NOT NULL,
payload_json TEXT NOT NULL DEFAULT '{}',
result_json TEXT NOT NULL DEFAULT '{}',
error_text TEXT,
scheduled_at TEXT,
started_at TEXT,
finished_at TEXT,
created_at TEXT NOT NULL
);
```

---

## `ingestion_policies`
Policy rules for read/write behavior.

```sql
CREATE TABLE ingestion_policies (
id TEXT PRIMARY KEY,
pattern_type TEXT NOT NULL, -- session|source|surface|agent|project|runtime
pattern TEXT NOT NULL,
mode TEXT NOT NULL, -- full|read_only|ignore
metadata_json TEXT NOT NULL DEFAULT '{}',
created_at TEXT NOT NULL,
updated_at TEXT NOT NULL
);
```

---

# 5. Derived schema

## 5.1 FTS tables

## `memories_fts`
```sql
CREATE VIRTUAL TABLE memories_fts USING fts5(
id UNINDEXED,
type,
subject,
body,
tight_description,
tags
);
```

## `summaries_fts`
```sql
CREATE VIRTUAL TABLE summaries_fts USING fts5(
id UNINDEXED,
kind,
title,
body,
tight_description
);
```

## `episodes_fts`
```sql
CREATE VIRTUAL TABLE episodes_fts USING fts5(
id UNINDEXED,
title,
summary,
tight_description
);
```

## `events_fts`
```sql
CREATE VIRTUAL TABLE events_fts USING fts5(
id UNINDEXED,
kind,
content
);
```

### FTS sync strategy

FTS5 tables duplicate data from canonical tables. In v0, **triggers** keep them in sync automatically. This is simpler than relying on worker jobs and guarantees FTS is never stale relative to the canonical store.

```sql
-- memories_fts sync
CREATE TRIGGER memories_fts_insert AFTER INSERT ON memories BEGIN
  INSERT INTO memories_fts(id, type, subject, body, tight_description, tags)
  VALUES (NEW.id, NEW.type, NEW.subject, NEW.body, NEW.tight_description, NEW.tags_json);
END;

CREATE TRIGGER memories_fts_update AFTER UPDATE ON memories BEGIN
  DELETE FROM memories_fts WHERE id = OLD.id;
  INSERT INTO memories_fts(id, type, subject, body, tight_description, tags)
  VALUES (NEW.id, NEW.type, NEW.subject, NEW.body, NEW.tight_description, NEW.tags_json);
END;

CREATE TRIGGER memories_fts_delete AFTER DELETE ON memories BEGIN
  DELETE FROM memories_fts WHERE id = OLD.id;
END;

-- Same pattern for summaries_fts, episodes_fts, events_fts
-- (events are append-only, so only INSERT trigger needed)
```

**Rebuild path:** If triggers are missed or FTS gets corrupted, `amm repair --fix indexes` should truncate and repopulate FTS from canonical tables. FTS is derived — always rebuildable.

---

## 5.2 Optional embeddings
```sql
CREATE TABLE embeddings (
object_id TEXT NOT NULL,
object_kind TEXT NOT NULL, -- memory|summary|episode|artifact
embedding_json TEXT NOT NULL,
model TEXT NOT NULL,
created_at TEXT NOT NULL,
PRIMARY KEY(object_id, object_kind, model)
);
```

---

## 5.3 Retrieval cache
```sql
CREATE TABLE retrieval_cache (
cache_key TEXT PRIMARY KEY,
result_json TEXT NOT NULL,
created_at TEXT NOT NULL,
expires_at TEXT
);
```

---

## 5.4 Recall history
For repetition suppression.

```sql
CREATE TABLE recall_history (
session_id TEXT NOT NULL,
item_id TEXT NOT NULL,
item_kind TEXT NOT NULL, -- memory|summary|episode|history-node
shown_at TEXT NOT NULL
);
```

Indexes:
```sql
CREATE INDEX idx_recall_history_session_item ON recall_history(session_id, item_id, item_kind);
CREATE INDEX idx_recall_history_shown_at ON recall_history(shown_at);
```

### Cleanup strategy

`recall_history` grows per-session and is only useful for recent repetition suppression. Rows older than `recall_history_ttl` (default: 7 days) are deleted by the `cleanup_recall_history` maintenance job.

```sql
-- Run by maintenance scheduler
DELETE FROM recall_history
WHERE shown_at < DATETIME('now', '-7 days');
```

**Rationale:** Repetition suppression only matters within recent sessions. A week-old recall record has no suppression value. Aggressive cleanup keeps this table small without affecting recall quality.

---

# 6. Core data contracts

## 6.1 Event envelope
```json
{
"id": "evt_01HV...",
"kind": "message_user",
"source_system": "agent-runtime",
"surface": "webchat",
"session_id": "session_123",
"project_id": null,
"workspace_id": null,
"agent_id": "aldous",
"actor_type": "user",
"actor_id": "josh",
"privacy_level": "private",
"content": "I want to see a whole detailed spec of everything we've talked about thus far",
"metadata": {
"channel": "webchat"
},
"occurred_at": "2026-03-22T04:02:00Z"
}
```

---

## 6.2 Summary envelope
```json
{
"kind": "session",
"scope": "global",
"project_id": null,
"session_id": "session_123",
"title": "amm architecture discussion",
"body": "Discussion covered persistent agent memory, ambient recall, typed memory, summaries, and integrations.",
"tight_description": "Recent discussion focused on amm architecture and ambient recall.",
"source_span": {
"event_ids": ["evt_1", "evt_2", "evt_3"]
}
}
```

---

## 6.3 Memory envelope
```json
{
"type": "decision",
"scope": "global",
"project_id": null,
"subject": "amm",
"body": "amm should stand as its own project and not be framed around other systems.",
"tight_description": "amm should stand as its own project.",
"confidence": 0.95,
"importance": 0.9,
"privacy_level": "private",
"observed_at": "2026-03-22T17:24:00Z",
"source_event_ids": ["evt_100", "evt_101"],
"tags": ["amm", "architecture"]
}
```

---

## 6.4 Ambient recall result
```json
{
"items": [
{
"id": "mem_1842",
"kind": "memory",
"type": "preference",
"scope": "global",
"score": 0.92,
"confidence": 0.96,
"tight_description": "Josh prefers concise replies by default."
},
{
"id": "sum_9021",
"kind": "summary",
"type": "session_summary",
"scope": "global",
"score": 0.78,
"tight_description": "Recent discussion focused on amm architecture and retrieval design."
}
],
"meta": {
"mode": "ambient",
"query_time_ms": 14
}
}
```

---

# 7. CLI command contract

## 7.1 `amm init`
Initialize local store.

```bash
amm init
amm init --db .amm/amm.db
```

---

## 7.2 `amm ingest event`
Append one raw event.

```bash
amm ingest event --in event.json
echo '{...}' | amm ingest event --in -
```

---

## 7.3 `amm ingest transcript`
Bulk ingest transcript/history.

```bash
amm ingest transcript --format jsonl --in session.jsonl
```

---

## 7.4 `amm remember`
Explicit durable memory commit.

```bash
amm remember \
--type decision \
--scope global \
--subject amm \
--body "amm should stand as its own project." \
--tight "amm should stand as its own project."
```

---

## 7.5 `amm recall`
Primary retrieval.

```bash
amm recall "What do I know about Josh's preferences?"
amm recall --mode ambient --limit 5 "memory architecture"
amm recall --project myproj --mode project "release process"
amm recall --entity Josh
```

Modes:
- `ambient`
- `facts`
- `episodes`
- `timeline`
- `project`
- `entity`
- `active`
- `history`
- `hybrid`

---

## 7.6 `amm describe`
Thin description of one or more items.

```bash
amm describe mem_3011
amm describe sum_9021
amm describe --query "memory architecture"
```

---

## 7.7 `amm expand`
Expand a specific item.

```bash
amm expand mem_3011
amm expand sum_9021
amm expand ep_4401
```

Behavior:
- memory -> full memory + claims + links
- summary -> source span + children + optional raw events
- episode -> linked summaries + unresolved items + source span

---

## 7.8 `amm history`
History-oriented retrieval.

```bash
amm history "what did we say about ambient recall"
amm history --session session_123
```

---

## 7.9 `amm jobs run`
Run maintenance/worker jobs.

```bash
amm jobs run reflect
amm jobs run compress_history
amm jobs run consolidate_sessions
amm jobs run detect_contradictions
amm jobs run repair_links
```

---

## 7.10 `amm explain-recall`
```bash
amm explain-recall --query "memory architecture" --item-id mem_3011
```

---

## 7.11 `amm repair`
```bash
amm repair --check
amm repair --fix links
amm repair --fix indexes
```

---

# 8. HTTP API contract

## 8.1 `POST /events`
Append raw event.

## 8.2 `POST /transcripts`
Bulk transcript import.

## 8.3 `POST /memories`
Explicit memory commit.

## 8.4 `POST /summaries`
Optional explicit summary insert.

## 8.5 `POST /recall`
Primary retrieval endpoint.

Request:
```json
{
"query": "memory architecture",
"mode": "ambient",
"project_id": null,
"session_id": "session_123",
"entity_ids": [],
"limit": 5,
"explain": false
}
```

---

## 8.6 `POST /describe`
Return thin item descriptions.

## 8.7 `POST /expand`
Return rich expansion payload.

Example:
```json
{
"item_id": "sum_9021",
"item_kind": "summary"
}
```

---

## 8.8 `POST /history`
History retrieval over raw events/summaries.

## 8.9 `GET /memories/:id`
Full memory.

## 8.10 `GET /summaries/:id`
Full summary node.

## 8.11 `GET /episodes/:id`
Full episode.

## 8.12 `POST /jobs/run`
Run job.

## 8.13 `POST /repair`
Run integrity check/repair.

## 8.14 `POST /explain-recall`
Why something surfaced.

---

# 9. v0 retrieval engine

## 9.1 Retrieval strategy
Use multi-strategy retrieval even in v0.

Signals:
1. FTS/BM25
2. semantic similarity if enabled
3. entity overlap
4. scope fit
5. recency
6. importance
7. freshness
8. temporal validity
9. repetition penalty
10. structural/source proximity

## 9.2 Suggested v0 scoring
```text
score =
0.25 * lexical
+ 0.18 * semantic
+ 0.18 * entity_overlap
+ 0.10 * scope_fit
+ 0.08 * recency
+ 0.07 * importance
+ 0.05 * temporal_validity
+ 0.05 * structural_proximity
+ 0.04 * freshness          -- computed at query time, not stored
- 0.10 * repetition_penalty
```

Renormalize if semantic disabled.

---

# 10. Ambient recall implementation

## 10.1 Input
- latest inbound message
- session id
- project id if any
- privacy context
- recent topic/entity cache

## 10.2 Fast path
1. cheap entity/topic extraction
2. query memories/summaries/episodes/history
3. merge candidates
4. rerank
5. return thin items only

## 10.3 Entity extraction v0
Keep simple:
- exact entity/alias match
- capitalized token heuristics
- recent topic tags
- optional later LLM extraction

## 10.4 Repetition suppression
Use `recall_history` to penalize repeated surfacing.

---

# 11. Reflection pipeline

## 11.1 `reflect`
Reads new events and creates candidate durable memories.

First target types:
- preference
- decision
- fact
- open_loop
- contradiction candidate
- episode candidate

## 11.2 v0 extraction approach
Heuristic-first:
- phrase cues
- event kind context
- source metadata
- explicit “remember this”
- explicit preferences/decisions

Optional LLM assist, not required every time.

---

# 12. Compression pipeline

## 12.1 `compress_history`
Build leaf summaries over event spans.

## 12.2 `consolidate_sessions`
Build session/topic summaries and episode summaries.

## 12.3 Summary linking
Every summary must link to:
- source events or child summaries
- optional parent summaries

This is what enables expansion.

---

# 13. Maintenance jobs

## 13.1 `reflect`
Create/update durable memories from events.

## 13.2 `compress_history`
Compress raw history into summaries.

## 13.3 `consolidate_sessions`
Build/update episodes and session summaries.

## 13.4 `merge_duplicates`
Find duplicate facts/preferences/decisions.

## 13.5 `detect_contradictions`
Find conflicting claims or stale truths.

## 13.6 `decay_stale_memory`
Downrank stale assumptions/open loops.

## 13.7 `rebuild_indexes`
Rebuild FTS/embeddings/cache.

## 13.8 `repair_links`
Validate and repair summary/source/memory links.

## 13.9 `cleanup_recall_history`
Delete recall_history rows older than `recall_history_ttl` (default: 7 days).

---

# 14. Integration blueprint

## 14.1 Agent runtime integration
On inbound message:
1. runtime sends raw event to amm
2. runtime asks for ambient recall
3. runtime injects compact hints
4. model responds
5. runtime logs response event
6. background jobs reflect/compress/index

## 14.2 External system integration
External systems may emit:
- decisions
- incidents
- task results
- artifacts
- summaries

amm stores them as history and/or canonical memory per policy.

---

# 15. Config model

## 15.1 Suggested config path
- `~/.amm/config.toml`
- optional `.amm/config.toml` override

## 15.2 Example
```toml
[storage]
db_path = "~/.amm/amm.db"

[retrieval]
default_limit = 5
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

[embeddings]
enabled = false
model = "local-minilm"
```

---

# 16. Suggested implementation order

## Step 1
- SQLite schema
- `amm init`
- `amm ingest event`
- `amm remember`
- `amm memory show`

## Step 2
- FTS indexing
- `amm recall`
- ambient retrieval mode
- thin packets

## Step 3
- `summaries` table
- `amm describe`
- `amm expand`
- summary/source linkage

## Step 4
- jobs framework
- `reflect`
- `compress_history`
- `consolidate_sessions`

## Step 5
- explain-recall
- contradiction scan
- repair checks

## Step 6
- runtime adapter

---

# 17. v0 success criteria

amm v0 is successful if it can reliably:

1. Store every interaction/event cheaply
2. Persist high-value typed memories cleanly
3. Preserve raw history and compress it without losing drill-down paths
4. Return a useful 3–7 item ambient recall packet
5. Expand a summary back toward source context
6. Keep scoped memory from bleeding across contexts stupidly
7. Repair/rebuild itself when derived state breaks

---

# 18. My blunt recommendation

Still the same:
do **not** start with:
- graph DB
- fancy cognitive engine
- huge UI
- learned rankers
- overcomplicated autonomous reflection

Start with:
- SQLite
- good schema
- FTS
- summaries
- ambient recall
- explicit memory commits
- repairable links
- maintenance jobs
