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

## Building

```bash
CGO_ENABLED=1 go build -tags fts5 -o amm ./cmd/amm
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
amm recall <query> [--mode <mode>] [--project <id>] [--session <id>]
```

The query is positional (all non-flag arguments are joined with spaces).

| Flag | Default | Description |
|---|---|---|
| `--mode` | `hybrid` | Recall mode (see Recall Modes below) |
| `--project` | | Filter to a project |
| `--session` | | Filter to a session |

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
amm expand <id> [--kind <kind>]
```

| Flag | Default | Description |
|---|---|---|
| `--kind` | Auto-inferred from ID prefix | Item kind: `memory`, `summary`, or `episode` |

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

### jobs run \<kind\>

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

**Available job kinds (12):**

| Kind | Description |
|---|---|
| `reflect` | Extract candidate durable memories from recent events |
| `compress_history` | Compress raw history into summaries |
| `consolidate_sessions` | Merge session-level summaries |
| `extract_claims` | Extract structured claims from memories |
| `form_episodes` | Form narrative episodes from related events |
| `detect_contradictions` | Find contradictions between memories |
| `decay_stale_memory` | Reduce confidence on stale memories |
| `merge_duplicates` | Merge duplicate memories |
| `rebuild_indexes` | Rebuild FTS and derived indexes |
| `cleanup_recall_history` | Clean up recall history tracking data |
| `reprocess` | Batch re-extract memories from events using LLM; skips events already processed by LLM |
| `reprocess_all` | Batch re-extract all memories unconditionally, superseding both heuristic and LLM results |

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

All 9 valid values for the `--mode` flag on `recall`:

| Mode | Description |
|---|---|
| `ambient` | General-purpose retrieval across all memory types |
| `facts` | Retrieve factual memories and claims |
| `episodes` | Retrieve narrative episode records |
| `timeline` | Chronological ordering of events and memories |
| `project` | Scoped to a specific project |
| `entity` | Retrieve memories related to specific entities |
| `active` | Currently active context and open loops |
| `history` | Raw event history search |
| `hybrid` | Combined scoring across multiple strategies (default) |

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
