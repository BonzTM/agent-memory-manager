# CLI Reference

## Output Format

All commands write a JSON envelope to stdout on success:

```json
{
  "ok": true,
  "command": "command_name",
  "timestamp": "2026-03-23T12:00:00Z",
  "result": { ... }
}
```

On failure the envelope goes to stderr with `"ok": false` and an `"error"` object instead of `"result"`:

```json
{
  "ok": false,
  "command": "command_name",
  "timestamp": "2026-03-23T12:00:00Z",
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable message"
  }
}
```

## Environment Variables

| Variable | Description | Default |
|---|---|---|
| `AMM_DB_PATH` | Path to the SQLite database file | `~/.amm/amm.db` |
| `AMM_COMPRESS_CHUNK_SIZE` | Max events per history chunk | `10` |
| `AMM_COMPRESS_MAX_EVENTS` | Max events per session summary | `200` |
| `AMM_COMPRESS_BATCH_SIZE` | Max summaries per LLM call | `15` |
| `AMM_TOPIC_BATCH_SIZE` | Max topic summaries per LLM call | `15` |
| `AMM_EMBEDDING_BATCH_SIZE` | Max items per embedding API call | `64` |
| `AMM_CROSS_PROJECT_SIMILARITY_THRESHOLD` | Threshold for cross-project memory transfer | `0.7` |

## Building

```bash
go build -o amm ./cmd/amm
```

---

## Commands

### init

Initialize the amm database. Creates the file and runs all migrations.

```
amm init [--db <path>]
```

| Flag | Description |
|---|---|
| `--db` | Override database path (also settable via `AMM_DB_PATH`) |

**Example:**

```bash
amm init --db ./project.db
```

```json
{
  "ok": true,
  "command": "init",
  "timestamp": "2026-03-23T12:00:00Z",
  "result": {
    "status": "initialized",
    "db_path": "./project.db"
  }
}
```

---

### ingest event

Append a single raw event to history.

```
amm ingest event [--in <file>]
```

Reads a JSON object from stdin (default) or from the file specified by `--in`. Use `--in -` explicitly for stdin.

| Flag | Description |
|---|---|
| `--in` | Path to a JSON file, or `-` for stdin (default: stdin) |

**Required fields:** `kind`, `source_system`, `content`.

**Optional fields:** `surface`, `session_id`, `project_id`, `agent_id`, `actor_type`, `actor_id`, `privacy_level`, `metadata`, `occurred_at` (RFC 3339).

**Example:**

```bash
echo '{"kind":"message_user","source_system":"cli","content":"Hello world"}' | amm ingest event
```

```json
{
  "ok": true,
  "command": "ingest_event",
  "timestamp": "2026-03-23T12:00:01Z",
  "result": {
    "id": "evt_abc123",
    "ingested_at": "2026-03-23T12:00:01Z"
  }
}
```

---

### ingest transcript

Bulk-ingest a sequence of events.

```
amm ingest transcript [--in <file>]
```

Reads from stdin or the file given by `--in`. Three input formats are accepted:

1. **Wrapped object** -- `{"events": [...]}`
2. **Plain JSON array** -- `[{...}, {...}]`
3. **JSONL** -- one JSON object per line

Each element uses the same schema as `ingest event`.

| Flag | Description |
|---|---|
| `--in` | Path to input file, or `-` for stdin (default: stdin) |

**Example:**

```bash
amm ingest transcript --in transcript.json
```

```json
{
  "ok": true,
  "command": "ingest_transcript",
  "timestamp": "2026-03-23T12:01:00Z",
  "result": {
    "ingested": 42
  }
}
```

---

### remember

Commit a durable memory.

```
amm remember --type <type> --body <body> --tight <summary> [--scope <scope>] [--subject <subject>] [--project <project>]
```

| Flag | Required | Description |
|---|---|---|
| `--type` | Yes | Memory type (see Memory Types below) |
| `--body` | Yes | Full memory body text |
| `--tight` | Yes | One-line tight description / summary |
| `--scope` | No | `global`, `project`, or `session` |
| `--subject` | No | Subject of the memory |
| `--project` | No | Project identifier |

**Example:**

```bash
amm remember \
  --type preference \
  --scope global \
  --subject "editor" \
  --body "User prefers Neovim with Lua config over VS Code" \
  --tight "Prefers Neovim over VS Code"
```

```json
{
  "ok": true,
  "command": "remember",
  "timestamp": "2026-03-23T12:02:00Z",
  "result": {
    "id": "mem_xyz789",
    "created_at": "2026-03-23T12:02:00Z"
  }
}
```

---

### recall

Retrieve memories using a recall mode.

```
amm recall <query> [--mode <mode>] [--project <id>] [--session <id>] [--agent-id <id>]
```

The query is positional (all non-flag arguments are joined with spaces).

| Flag | Default | Description |
|---|---|---|
| `--mode` | `hybrid` | Recall mode (see Recall Modes below) |
| `--project` | | Filter to a project |
| `--session` | | Filter to a session |
| `--agent-id` | | Filter to an agent ID |

**Example:**

```bash
amm recall "editor preferences" --mode facts
```

```json
{
  "ok": true,
  "command": "recall",
  "timestamp": "2026-03-23T12:03:00Z",
  "result": {
    "items": [
      {
        "id": "mem_xyz789",
        "kind": "memory",
        "type": "preference",
        "scope": "global",
        "score": 0.92,
        "tight_description": "Prefers Neovim over VS Code"
      }
    ],
    "meta": {
      "mode": "facts",
      "query_time_ms": 14
    }
  }
}
```

---

### grep

Search raw events for a text pattern, then group matches by their covering summary.

```
amm grep <pattern> [--project-id <id>] [--session-id <id>] [--max-group-depth <n>] [--group-limit <n>] [--matches-per-group <n>]
```

| Flag | Description |
|---|---|
| `--project-id` | Filter to a project |
| `--session-id` | Filter to a session |
| `--max-group-depth` | Max summary depth when finding a covering summary |
| `--group-limit` | Max groups to return |
| `--matches-per-group` | Max matches to keep per group |

**Example:**

```bash
amm grep "Neovim"
```

```json
{
  "ok": true,
  "command": "grep",
  "timestamp": "2026-03-30T12:00:00Z",
  "result": {
    "pattern": "Neovim",
    "total_hits": 1,
    "sample_limited": false,
    "groups": [
      {
        "summary_id": "sum_xyz789",
        "summary_text": "Editor preferences",
        "matches": [
          {
            "event_id": "evt_abc123",
            "kind": "message_user",
            "content": "User prefers Neovim with Lua config over VS Code"
          }
        ]
      }
    ]
  }
}
```

---

### context-window

Assemble and format a context window for an agent based on recent activity and relevant memories.

```
amm context-window [--project-id <id>] [--session-id <id>] [--fresh-tail-count <n>] [--max-summary-depth <n>] [--include-parent-refs]
```

| Flag | Default | Description |
|---|---|---|
| `--project-id` | | Project identifier |
| `--session-id` | | Session identifier |
| `--fresh-tail-count` | `32` | Number of fresh events to include |
| `--max-summary-depth` | `0` | Max summary depth to include |
| `--include-parent-refs` | `false` | Include parent summary references |

**Example:**

```bash
amm context-window --project-id amm --fresh-tail-count 32 --max-summary-depth 1 --include-parent-refs
```

```json
{
  "ok": true,
  "command": "context_window",
  "timestamp": "2026-03-30T12:05:00Z",
  "result": {
    "content": "...",
    "summary_count": 3,
    "fresh_count": 32,
    "est_tokens": 1240,
    "manifest": []
  }
}
```

---

### describe

Return thin descriptions for one or more items by ID.

```
amm describe <id> [<id> ...]
```

IDs are positional. Pass as many as needed.

**Example:**

```bash
amm describe mem_xyz789 ep_abc123
```

```json
{
  "ok": true,
  "command": "describe",
  "timestamp": "2026-03-23T12:04:00Z",
  "result": [
    {
      "id": "mem_xyz789",
      "kind": "memory",
      "type": "preference",
      "scope": "global",
      "tight_description": "Prefers Neovim over VS Code",
      "status": "active",
      "created_at": "2026-03-23T12:02:00Z"
    }
  ]
}
```

---

### expand

Expand a single item to its full detail, including linked claims, events, and children.

```
amm expand <id> [--kind <kind>] [--session-id <id>] [--delegation-depth <n>]
```

| Flag | Default | Description |
|---|---|---|
| `--kind` | Auto-inferred from ID prefix | Item kind: `memory`, `summary`, or `episode` |
| `--session-id` | | Session identifier used for expand-time relevance feedback attribution |
| `--delegation-depth` | `0` | Max recursive delegation depth for linked content |

The kind is inferred automatically from the ID prefix:

- `mem_` -- `memory`
- `sum_` -- `summary`
- `ep_` -- `episode`
- Other -- defaults to `memory`

**Example:**

```bash
amm expand mem_xyz789
```

```json
{
  "ok": true,
  "command": "expand",
  "timestamp": "2026-03-23T12:05:00Z",
  "result": {
    "memory": {
      "id": "mem_xyz789",
      "type": "preference",
      "scope": "global",
      "subject": "editor",
      "body": "User prefers Neovim with Lua config over VS Code",
      "tight_description": "Prefers Neovim over VS Code",
      "confidence": 0.9,
      "importance": 0.7,
      "status": "active",
      "created_at": "2026-03-23T12:02:00Z",
      "updated_at": "2026-03-23T12:02:00Z"
    },
    "claims": [],
    "events": []
  }
}
```

---

### history

Query raw interaction history.

```
amm history [<query>] [--session <id>] [--project <id>]
```

The query is positional (optional). When omitted, returns recent events filtered by the provided flags.

| Flag | Description |
|---|---|
| `--session` | Filter to a session ID |
| `--project` | Filter to a project ID |

**Example:**

```bash
amm history "database migration" --project myproject
```

```json
{
  "ok": true,
  "command": "history",
  "timestamp": "2026-03-23T12:06:00Z",
  "result": {
    "events": [
      {
        "id": "evt_abc123",
        "kind": "message_user",
        "source_system": "cli",
        "content": "Let's set up the database migration...",
        "occurred_at": "2026-03-22T10:00:00Z",
        "ingested_at": "2026-03-22T10:00:01Z"
      }
    ],
    "count": 1
  }
}
```

---

### memory [show] \<id\>

Retrieve a single memory by ID. The `show` subcommand is optional.

```
amm memory <id>
amm memory show <id>
```

**Example:**

```bash
amm memory mem_xyz789
```

```json
{
  "ok": true,
  "command": "memory",
  "timestamp": "2026-03-23T12:07:00Z",
  "result": {
    "id": "mem_xyz789",
    "type": "preference",
    "scope": "global",
    "subject": "editor",
    "body": "User prefers Neovim with Lua config over VS Code",
    "tight_description": "Prefers Neovim over VS Code",
    "confidence": 0.9,
    "importance": 0.7,
    "status": "active",
    "created_at": "2026-03-23T12:02:00Z",
    "updated_at": "2026-03-23T12:02:00Z"
  }
}
```

---

### memory update <id>

Update an existing memory.

```
amm memory update <id> [--body <body>] [--tight <summary>] [--status <status>] [--type <type>] [--scope <scope>]
```

| Flag | Description |
|---|---|
| `--body` | Updated memory body text |
| `--tight` | Updated one-line summary |
| `--status` | Updated status: `active`, `superseded`, `archived`, `retracted` |
| `--type` | Updated memory type |
| `--scope` | Updated scope |

**Example:**

```bash
amm memory update mem_xyz789 --status archived
```

---

### share

Update a memory's privacy level.

```
amm share <memory-id> [--privacy <level>]
```

| Flag | Default | Description |
|---|---|---|
| `--privacy` | | Privacy level: `private`, `shared`, or `public_safe` |

`<memory-id>` is positional and required.

**Example:**

```bash
amm share mem_xyz789 --privacy shared
```

```json
{
  "ok": true,
  "command": "share",
  "timestamp": "2026-03-23T12:07:30Z",
  "result": {
    "id": "mem_xyz789",
    "privacy_level": "shared"
  }
}
```

---

### forget

Forget (retract) a memory by ID.

```
amm forget <id>
```

**Example:**

```bash
amm forget mem_xyz789
```

---

### policy list

List all ingestion policies.

```
amm policy list
```

---

### policy add

Add an ingestion policy.

```
amm policy add --pattern-type <type> --pattern <pattern> --mode <mode> [--priority <priority>] [--match-mode <match-mode>]
```

| Flag | Required | Description |
|---|---|---|
| `--pattern-type` | Yes | `kind`, `session`, `source`, `surface`, `agent`, `project`, or `runtime` |
| `--pattern` | Yes | Pattern string to match |
| `--mode` | Yes | `full`, `read_only`, or `ignore` |
| `--priority` | No | Integer priority (higher wins) |
| `--match-mode` | No | `exact`, `glob`, or `regex` |

**Recommended policies** (add these after `amm init` for all deployments):

```bash
# Ignore tool_call and tool_result events to prevent tool JSON from polluting extracted memories
amm policy-add --pattern-type kind --pattern "tool_call" --mode ignore --match-mode exact --priority 100
amm policy-add --pattern-type kind --pattern "tool_result" --mode ignore --match-mode exact --priority 100
```

---

### policy remove

Remove an ingestion policy by ID.

```
amm policy remove <id>
```

---

### project add

Register a new project.

```
amm project add --name <name> --path <path> --description <description>
```

| Flag | Required | Description |
|---|---|---|
| `--name` | Yes | Project name |
| `--path` | Yes | Absolute or relative project path |
| `--description` | Yes | Human-readable project description |

**Example:**

```bash
amm project add --name "amm" --path "/home/user/src/agent-memory-manager" --description "Agent memory manager repository"
```

---

### project show

Show a project by ID.

```
amm project show <id>
```

`<id>` is positional and required.

**Example:**

```bash
amm project show proj_abc123
```

---

### project list

List all registered projects.

```
amm project list
```

No flags or positional arguments.

**Example:**

```bash
amm project list
```

---

### project remove

Remove a project by ID.

```
amm project remove <id>
```

`<id>` is positional and required.

**Example:**

```bash
amm project remove proj_abc123
```

---

### relationship add

Add a directed relationship between entities.

```
amm relationship add --from <id> --to <id> --type <type>
```

| Flag | Required | Description |
|---|---|---|
| `--from` | Yes | Source entity ID |
| `--to` | Yes | Destination entity ID |
| `--type` | Yes | Relationship type |

**Example:**

```bash
amm relationship add --from ent_parent --to ent_child --type parent_of
```

---

### relationship show

Show a relationship by ID.

```
amm relationship show <id>
```

`<id>` is positional and required.

**Example:**

```bash
amm relationship show rel_abc123
```

---

### relationship list

List relationships with optional filters.

```
amm relationship list [--entity-id <id>] [--relationship-type <type>] [--limit <n>]
```

| Flag | Description |
|------|-------------|
| `--entity-id` | Filter by entity ID |
| `--relationship-type` | Filter by relationship type |
| `--limit` | Max results to return |

**Example:**

```bash
amm relationship list
amm relationship list --entity-id ent_abc123
amm relationship list --relationship-type depends_on --limit 10
```

---

### relationship remove

Remove a relationship by ID.

```
amm relationship remove <id>
```

`<id>` is positional and required.

**Example:**

```bash
amm relationship remove rel_abc123
```

---

### summary show

Show a summary by ID.

```
amm summary show <id>
```

`<id>` is positional and required.

**Example:**

```bash
amm summary show sum_abc123
```

---

### episode show

Show an episode by ID.

```
amm episode show <id>
```

`<id>` is positional and required.

**Example:**

```bash
amm episode show ep_abc123
```

---

### entity show

Show an entity by ID.

```
amm entity show <id>
```

`<id>` is positional and required.

**Example:**

```bash
amm entity show ent_abc123
```

---

### jobs run <kind>

Execute a maintenance job.

```
amm jobs run <kind>
```

The job kind is positional.

For reprocessing flows, you can use either positional kinds or convenience flags:

```bash
# Equivalent
amm jobs run reprocess
amm jobs run --reprocess

# Equivalent
amm jobs run reprocess_all
amm jobs run --reprocess-all
```

Do not combine a positional job kind with `--reprocess` or `--reprocess-all`.

**Recommended pipeline order:**

1. `reflect` (Phase 1: Extraction)
2. `rebuild_indexes` (Phase 2: Initial embedding)
3. `compress_history`, `consolidate_sessions`, `build_topic_summaries` (Phase 3: Compression)
4. `merge_duplicates`, `extract_claims`, `enrich_memories`, `rebuild_entity_graph`, `form_episodes` (Phase 4: Linking)
5. `detect_contradictions`, `decay_stale_memory`, `lifecycle_review`, `cross_project_transfer`, `archive_session_traces` (Phase 5: Quality)
6. `rebuild_indexes`, `cleanup_recall_history`, `update_ranking_weights` (Phase 6: Finalization)
7. `purge_old_events`, `purge_old_jobs`, `expire_retrieval_cache`, `purge_relevance_feedback`, `vacuum_analyze` (Phase 7: DB trim and compaction)

**Available job kinds (25):**

| Kind | Description |
|---|---|
| `reflect` | Extract candidate durable memories from recent events |
| `compress_history` | Compress raw history into summaries |
| `consolidate_sessions` | Merge session-level summaries |
| `build_topic_summaries` | Build topic-level hierarchical summaries |
| `extract_claims` | Extract structured claims from memories |
| `enrich_memories` | Entity-link and enrich explicitly remembered memories |
| `form_episodes` | Form narrative episodes from related events |
| `detect_contradictions` | Find contradictions between memories |
| `decay_stale_memory` | Reduce confidence on stale memories |
| `merge_duplicates` | Merge duplicate memories |
| `rebuild_indexes` | Rebuild FTS and embeddings (incremental — skips existing) |
| `rebuild_indexes_full` | Rebuild FTS and all embeddings from scratch |
| `cleanup_recall_history` | Clean up recall history tracking data |
| `lifecycle_review` | LLM-powered batch review for decay/promote/contradict |
| `cross_project_transfer` | Detect and promote cross-project memories to global |
| `rebuild_entity_graph` | Rebuild pre-computed entity neighborhoods |
| `archive_session_traces` | Archive low-salience session-scoped memories |
| `update_ranking_weights` | Update scoring weights from relevance feedback |
| `reprocess` | Batch re-extract memories from events using LLM; skips events already processed by LLM. Uses endgame pipeline logic (triage, entity linking, processing ledger). |
| `reprocess_all` | Batch re-extract all memories unconditionally, superseding both heuristic and LLM results. Uses endgame pipeline logic. |
| `purge_old_events` | Delete reflected events older than 30 days to reclaim space |
| `purge_old_jobs` | Delete completed and failed job records older than 30 days |
| `expire_retrieval_cache` | Delete expired retrieval cache entries |
| `purge_relevance_feedback` | Delete relevance feedback signals older than 30 days |
| `vacuum_analyze` | Run backend-specific DB maintenance (SQLite: WAL checkpoint + ANALYZE + VACUUM; Postgres: ANALYZE) |

**Example:**

```bash
amm jobs run reflect
```

```json
{
  "ok": true,
  "command": "jobs_run",
  "timestamp": "2026-03-23T12:08:00Z",
  "result": {
    "id": "job_def456",
    "kind": "reflect",
    "status": "completed",
    "started_at": "2026-03-23T12:08:00Z",
    "finished_at": "2026-03-23T12:08:02Z"
  }
}
```

---

### explain-recall

Explain why a particular item surfaced for a given query.

```
amm explain-recall --query <query> --item-id <id>
```

| Flag | Required | Description |
|---|---|---|
| `--query` | Yes | The recall query to explain |
| `--item-id` | Yes | The item ID that surfaced |

**Example:**

```bash
amm explain-recall --query "editor preferences" --item-id mem_xyz789
```

```json
{
  "ok": true,
  "command": "explain_recall",
  "timestamp": "2026-03-23T12:09:00Z",
  "result": {
    "explanation": {
      "query": "editor preferences",
      "item_id": "mem_xyz789",
      "factors": {
        "text_match": 0.85,
        "recency": 0.6,
        "importance": 0.7
      }
    }
  }
}
```

---

### repair

Run integrity checks and optionally fix issues.

```
amm repair [--check] [--fix <target>]
```

| Flag | Description |
|---|---|
| `--check` | Run integrity checks without fixing (boolean flag, no value needed) |
| `--fix` | Fix a specific target: `indexes`, `links`, or `recall_history` |

When neither flag is provided, the command does nothing. Use `--check` to audit, then `--fix` to repair a specific subsystem.

**Fix targets:**

- `indexes` -- Rebuild FTS5 and derived indexes
- `links` -- Repair broken links between memories, claims, and events
- `recall_history` -- Clean up stale recall history entries

**Example (check only):**

```bash
amm repair --check
```

```json
{
  "ok": true,
  "command": "repair",
  "timestamp": "2026-03-23T12:10:00Z",
  "result": {
    "checked": 150,
    "issues": 2,
    "fixed": 0,
    "details": [
      "orphaned claim clm_abc has no parent memory",
      "FTS index missing 3 entries"
    ]
  }
}
```

**Example (fix):**

```bash
amm repair --fix indexes
```

```json
{
  "ok": true,
  "command": "repair",
  "timestamp": "2026-03-23T12:10:05Z",
  "result": {
    "checked": 150,
    "issues": 1,
    "fixed": 1,
    "details": [
      "rebuilt FTS index: 3 entries restored"
    ]
  }
}
```

---

### status

Show system status.

```
amm status
```

No flags. Returns database path and item counts.

**Example:**

```bash
amm status
```

```json
{
  "ok": true,
  "command": "status",
  "timestamp": "2026-03-23T12:11:00Z",
  "result": {
    "db_path": "/home/user/.amm/amm.db",
    "initialized": true,
    "event_count": 1234,
    "memory_count": 89,
    "summary_count": 23,
    "episode_count": 7,
    "entity_count": 45
  }
}
```

---

### reset-derived

Delete all derived/canonical-derived data and reset event reflection markers while preserving raw events.

```
amm reset-derived [--confirm]
```

The command only runs with `--confirm`.

| Flag | Description |
|---|---|
| `--confirm` | Skip the interactive prompt and execute immediately |

**Safety warning:**

- This deletes derived data (`memories`, `entities`, `relationships`, `claims`, `summaries`, `episodes`, `jobs`, embeddings, caches, and feedback).
- Events are preserved.
- A later `amm jobs run reflect` will re-extract from events.
- This operation is irreversible.

**Example (without `--confirm`):**

```bash
amm reset-derived
```

**Example (with `--confirm`):**

```bash
amm reset-derived --confirm
```

```json
{
  "ok": true,
  "command": "reset_derived",
  "timestamp": "2026-03-23T12:12:00Z",
  "result": {
    "events_preserved": 1234,
    "events_reset_for_reflect": 1200,
    "memories_deleted": 89,
    "entities_deleted": 45,
    "summaries_deleted": 23,
    "episodes_deleted": 7,
    "relationships_deleted": 34,
    "jobs_deleted": 12,
    "unreflected_after_reset": 1234
  }
}
```

---

## Memory Types

All 16 valid values for the `--type` flag on `remember`:

| Type | Description |
|---|---|
| `identity` | Who the user is, self-descriptions |
| `preference` | Likes, dislikes, preferred tools/styles |
| `fact` | Verified or stated factual information |
| `decision` | Choices made, with rationale |
| `episode` | Narrative account of something that happened |
| `todo` | Pending action items |
| `relationship` | Connections between entities |
| `procedure` | How-to steps, workflows, processes |
| `constraint` | Rules, limits, hard requirements |
| `incident` | Errors, failures, unexpected events |
| `artifact` | References to files, URLs, code |
| `summary` | Condensed recaps of history |
| `active_context` | Currently relevant working context |
| `open_loop` | Unresolved questions or dangling threads |
| `assumption` | Believed-true statements needing confirmation |
| `contradiction` | Conflicting information detected between memories |

## Recall Modes

All 11 valid values for the `--mode` flag on `recall`:

| Mode | Description |
|---|---|
| `ambient` | General-purpose retrieval across all memory types |
| `facts` | Retrieve factual memories and claims |
| `episodes` | Retrieve narrative episode records |
| `sessions` | List and search session summaries with optional date filtering |
| `timeline` | Chronological ordering of events and memories |
| `project` | Scoped to a specific project |
| `entity` | Retrieve memories related to specific entities |
| `active` | Currently active context and open loops |
| `history` | Raw event history search |
| `contradictions` | Retrieve memories with detected contradictions |
| `hybrid` | Combined scoring across multiple strategies (default) |

### Temporal Filtering

All recall modes support `--after` and `--before` flags (RFC3339 timestamps) to
filter results by time range. Natural-language temporal references in the query
text (e.g., "last week", "yesterday", "in March") are automatically extracted
and applied when explicit flags are not set.

## Scopes

| Scope | Description |
|---|---|
| `global` | Visible across all projects and sessions |
| `project` | Scoped to a specific project |
| `session` | Scoped to a specific session |

## Memory Statuses

| Status | Description |
|---|---|
| `active` | Current and valid |
| `superseded` | Replaced by a newer memory |
| `archived` | No longer active but preserved |
| `retracted` | Explicitly withdrawn as incorrect |
