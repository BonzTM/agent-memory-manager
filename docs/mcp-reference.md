# MCP Reference

amm exposes all its functionality as MCP (Model Context Protocol) tools over a stdio JSON-RPC 2.0 transport. One JSON message per line, newline-delimited.

## Starting the Server

```bash
go build -o amm-mcp ./cmd/amm-mcp
AMM_DB_PATH=~/.amm/amm.db ./amm-mcp
```

The server reads from stdin and writes to stdout. Each line is a complete JSON-RPC 2.0 message.

## Protocol

### Initialization

Send an `initialize` request to negotiate capabilities:

```json
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}
```

Response:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "protocolVersion": "2024-11-05",
    "serverInfo": {
      "name": "amm-mcp",
      "version": "1.0.0"
    },
    "capabilities": {
      "tools": {
        "listChanged": false
      }
    }
  }
}
```

### Listing Tools

```json
{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}
```

Returns a `tools` array containing all tool definitions with their input schemas.

### Calling a Tool

```json
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"<tool_name>","arguments":{...}}}
```

On success, the result contains a `content` array with a single text entry holding the JSON-serialized response:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "content": [{"type": "text", "text": "{...}"}]
  }
}
```

On tool-level errors (as opposed to protocol errors), the response still uses the `result` field but sets `isError` to true:

```json
{
  "jsonrpc": "2.0",
  "id": 3,
  "result": {
    "content": [{"type": "text", "text": "error: something went wrong"}],
    "isError": true
  }
}
```

---

## Available Tools

### amm_init

Initialize the amm database.

**Input schema:**

```json
{
  "type": "object",
  "properties": {}
}
```

**Example:**

```json
{"jsonrpc":"2.0","id":10,"method":"tools/call","params":{"name":"amm_init","arguments":{}}}
```

---

### amm_ingest_event

Append a raw event to history.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "kind":          {"type": "string", "description": "Event kind (e.g. message_user, message_assistant)"},
    "source_system": {"type": "string", "description": "Source system identifier"},
    "content":       {"type": "string", "description": "Event content"},
    "session_id":    {"type": "string", "description": "Session identifier"},
    "project_id":    {"type": "string", "description": "Project identifier"},
    "occurred_at":   {"type": "string", "description": "When the event occurred (RFC3339)"}
  },
  "required": ["kind", "source_system", "content"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 11,
  "method": "tools/call",
  "params": {
    "name": "amm_ingest_event",
    "arguments": {
      "kind": "message_user",
      "source_system": "claude-code",
      "content": "Set up the database schema",
      "session_id": "sess_abc",
      "project_id": "proj_xyz"
    }
  }
}
```

---

### amm_ingest_transcript

Bulk ingest a sequence of events.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "events": {
      "type": "array",
      "description": "List of events to ingest",
      "items": {
        "type": "object",
        "properties": {
          "kind":          {"type": "string"},
          "source_system": {"type": "string"},
          "content":       {"type": "string"},
          "session_id":    {"type": "string"},
          "project_id":    {"type": "string"},
          "occurred_at":   {"type": "string"}
        },
        "required": ["kind", "source_system", "content"]
      }
    }
  },
  "required": ["events"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 12,
  "method": "tools/call",
  "params": {
    "name": "amm_ingest_transcript",
    "arguments": {
      "events": [
        {"kind": "message_user", "source_system": "cli", "content": "First message"},
        {"kind": "message_assistant", "source_system": "cli", "content": "Response"}
      ]
    }
  }
}
```

Response text contains: `{"ingested": 2}`

---

### amm_remember

Commit a durable memory.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "type":              {"type": "string", "description": "Memory type"},
    "scope":             {"type": "string", "description": "Scope: global, project, session"},
    "body":              {"type": "string", "description": "Memory body"},
    "tight_description": {"type": "string", "description": "One-line summary"},
    "subject":           {"type": "string", "description": "Subject of the memory"},
    "project_id":        {"type": "string", "description": "Project identifier"}
  },
  "required": ["type", "body", "tight_description"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 13,
  "method": "tools/call",
  "params": {
    "name": "amm_remember",
    "arguments": {
      "type": "preference",
      "scope": "global",
      "body": "User prefers dark color schemes in all editors",
      "tight_description": "Prefers dark color schemes",
      "subject": "color scheme"
    }
  }
}
```

Response text contains: `{"id": "mem_abc123", "created_at": "2026-03-23T12:00:00Z"}`

---

### amm_recall

Retrieve memories using various recall modes.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "query": {"type": "string", "description": "Search query"},
    "agent_id": {"type": "string", "description": "Agent identifier"},
    "opts": {
      "type": "object",
      "properties": {
        "mode":       {"type": "string"},
        "project_id": {"type": "string"},
        "session_id": {"type": "string"},
        "agent_id":   {"type": "string"},
        "limit":      {"type": "integer"}
      }
    }
  },
  "required": ["query"]
}
```

**Recall modes:** `ambient`, `facts`, `episodes`, `timeline`, `project`, `entity`, `active`, `history`, `contradictions`, `hybrid` (default).

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 14,
  "method": "tools/call",
  "params": {
    "name": "amm_recall",
    "arguments": {
      "query": "color preferences",
      "opts": {
        "mode": "facts",
        "limit": 5
      }
    }
  }
}
```

Response text contains a `RecallResult` with `items` and `meta`.

---

### amm_grep

Search raw events for a text pattern, then group matches by their covering summary.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "pattern": {"type": "string", "description": "Regex or text pattern to search for"},
    "session_id": {"type": "string", "description": "Filter to a session"},
    "project_id": {"type": "string", "description": "Filter to a project"},
    "max_group_depth": {"type": "integer", "description": "Max summary depth when finding a covering summary"},
    "group_limit": {"type": "integer", "description": "Max groups to return"},
    "matches_per_group": {"type": "integer", "description": "Max matches to keep per group"}
  },
  "required": ["pattern"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 141,
  "method": "tools/call",
  "params": {
    "name": "amm_grep",
    "arguments": {
      "pattern": "Neovim",
      "session_id": "sess_abc",
      "group_limit": 5
    }
  }
}
```

---

### amm_format_context_window

Assemble a context window from summaries plus fresh events, in chronological order.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "session_id": {"type": "string"},
    "project_id": {"type": "string"},
    "fresh_tail_count": {"type": "integer"},
    "max_summary_depth": {"type": "integer"},
    "include_parent_refs": {"type": "boolean"}
  }
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 142,
  "method": "tools/call",
  "params": {
    "name": "amm_format_context_window",
    "arguments": {
      "project_id": "amm",
      "fresh_tail_count": 20,
      "max_summary_depth": 3,
      "include_parent_refs": true
    }
  }
}
```


---

### amm_describe

Get thin descriptions of items by ID.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "ids": {"type": "array", "items": {"type": "string"}}
  },
  "required": ["ids"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 15,
  "method": "tools/call",
  "params": {
    "name": "amm_describe",
    "arguments": {
      "ids": ["mem_abc123", "ep_def456"]
    }
  }
}
```

---

### amm_expand

Expand an item to full detail, including linked claims, events, and children.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "id":   {"type": "string", "description": "Item ID to expand"},
    "kind": {"type": "string", "description": "Item kind: memory, summary, episode (defaults to memory when omitted)"},
    "session_id": {"type": "string", "description": "Session identifier for relevance feedback"},
    "delegation_depth": {"type": "integer", "description": "Max recursive delegation depth for linked content"}
  },
  "required": ["id"]
}
```

When `kind` is omitted, the server defaults to `memory`.

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 16,
  "method": "tools/call",
  "params": {
    "name": "amm_expand",
    "arguments": {
      "id": "mem_abc123"
    }
  }
}
```

---

### amm_history

Query raw interaction history.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "query": {"type": "string"},
    "opts": {
      "type": "object",
      "properties": {
        "session_id": {"type": "string"},
        "project_id": {"type": "string"},
        "limit":      {"type": "integer"}
      }
    }
  }
}
```

Both `query` and `opts` are optional. Omit the query to browse by session or project.

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 17,
  "method": "tools/call",
  "params": {
    "name": "amm_history",
    "arguments": {
      "query": "database migration",
      "opts": {"project_id": "proj_xyz", "limit": 20}
    }
  }
}
```

---

### amm_get_memory

Get a single memory by ID.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Item ID"}
  },
  "required": ["id"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 18,
  "method": "tools/call",
  "params": {
    "name": "amm_get_memory",
    "arguments": {
      "id": "mem_abc123"
    }
  }
}
```

---

### amm_update_memory

Update an existing memory. Only the provided fields are changed; omitted fields are left as-is.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "id":                {"type": "string", "description": "Memory ID to update"},
    "body":              {"type": "string", "description": "Updated memory body"},
    "tight_description": {"type": "string", "description": "Updated one-line summary"},
    "status":            {"type": "string", "description": "Memory status: active, superseded, archived, retracted"}
  },
  "required": ["id"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 19,
  "method": "tools/call",
  "params": {
    "name": "amm_update_memory",
    "arguments": {
      "id": "mem_abc123",
      "status": "retracted"
    }
  }
}
```

---

### amm_share

Update a memory privacy level.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "id":      {"type": "string", "description": "Memory ID to share"},
    "privacy": {"type": "string", "description": "Privacy level: private, shared, public_safe"}
  },
  "required": ["id", "privacy"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 19,
  "method": "tools/call",
  "params": {
    "name": "amm_share",
    "arguments": {
      "id": "mem_abc123",
      "privacy": "shared"
    }
  }
}
```

---

### amm_forget

Forget (retract) a memory by ID. The memory is marked as retracted and excluded from recall. Future reflect/reprocess runs will not re-extract forgotten content.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Memory ID to forget"}
  },
  "required": ["id"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 20,
  "method": "tools/call",
  "params": {
    "name": "amm_forget",
    "arguments": {
      "id": "mem_abc123"
    }
  }
}
```

---

### amm_register_project

Register a new project.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "name":        {"type": "string", "description": "Project name"},
    "path":        {"type": "string", "description": "Project path"},
    "description": {"type": "string", "description": "Project description"}
  },
  "required": ["name", "path", "description"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 20,
  "method": "tools/call",
  "params": {
    "name": "amm_register_project",
    "arguments": {
      "name": "amm",
      "path": "/home/user/src/agent-memory-manager",
      "description": "Agent memory manager repository"
    }
  }
}
```

---

### amm_get_project

Get a project by ID.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Item ID"}
  },
  "required": ["id"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 21,
  "method": "tools/call",
  "params": {
    "name": "amm_get_project",
    "arguments": {
      "id": "proj_abc123"
    }
  }
}
```

---

### amm_list_projects

List all projects.

**Input schema:**

```json
{
  "type": "object",
  "properties": {}
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 22,
  "method": "tools/call",
  "params": {
    "name": "amm_list_projects",
    "arguments": {}
  }
}
```

---

### amm_remove_project

Remove a project by ID.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Item ID"}
  },
  "required": ["id"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 23,
  "method": "tools/call",
  "params": {
    "name": "amm_remove_project",
    "arguments": {
      "id": "proj_abc123"
    }
  }
}
```

---

### amm_add_relationship

Add an entity relationship.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "from_entity_id":    {"type": "string", "description": "Source entity ID"},
    "to_entity_id":      {"type": "string", "description": "Destination entity ID"},
    "relationship_type": {"type": "string", "description": "Relationship type"}
  },
  "required": ["from_entity_id", "to_entity_id", "relationship_type"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 24,
  "method": "tools/call",
  "params": {
    "name": "amm_add_relationship",
    "arguments": {
      "from_entity_id": "ent_parent",
      "to_entity_id": "ent_child",
      "relationship_type": "parent_of"
    }
  }
}
```

---

### amm_get_relationship

Get a relationship by ID.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Item ID"}
  },
  "required": ["id"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 25,
  "method": "tools/call",
  "params": {
    "name": "amm_get_relationship",
    "arguments": {
      "id": "rel_abc123"
    }
  }
}
```

---

### amm_list_relationships

List relationships.

**Input schema:**

```json
{
  "type": "object",
  "properties": {}
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 26,
  "method": "tools/call",
  "params": {
    "name": "amm_list_relationships",
    "arguments": {}
  }
}
```

---

### amm_remove_relationship

Remove a relationship by ID.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Item ID"}
  },
  "required": ["id"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 27,
  "method": "tools/call",
  "params": {
    "name": "amm_remove_relationship",
    "arguments": {
      "id": "rel_abc123"
    }
  }
}
```

---

### amm_get_summary

Get a summary by ID.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Item ID"}
  },
  "required": ["id"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 28,
  "method": "tools/call",
  "params": {
    "name": "amm_get_summary",
    "arguments": {
      "id": "sum_abc123"
    }
  }
}
```

---

### amm_get_episode

Get an episode by ID.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Item ID"}
  },
  "required": ["id"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 29,
  "method": "tools/call",
  "params": {
    "name": "amm_get_episode",
    "arguments": {
      "id": "ep_abc123"
    }
  }
}
```

---

### amm_get_entity

Get an entity by ID.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "id": {"type": "string", "description": "Item ID"}
  },
  "required": ["id"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 30,
  "method": "tools/call",
  "params": {
    "name": "amm_get_entity",
    "arguments": {
      "id": "ent_abc123"
    }
  }
}
```

---

### amm_jobs_run

Run a maintenance job.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "kind": {"type": "string", "description": "Job kind to run"}
  },
  "required": ["kind"]
}
```

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

```json
{
  "jsonrpc": "2.0",
  "id": 20,
  "method": "tools/call",
  "params": {
    "name": "amm_jobs_run",
    "arguments": {
      "kind": "reflect"
    }
  }
}
```

---

### amm_explain_recall

Explain why a particular item surfaced for a query.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "query":   {"type": "string"},
    "item_id": {"type": "string"}
  },
  "required": ["query", "item_id"]
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 21,
  "method": "tools/call",
  "params": {
    "name": "amm_explain_recall",
    "arguments": {
      "query": "color preferences",
      "item_id": "mem_abc123"
    }
  }
}
```

---

### amm_repair

Run integrity checks and repairs.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "check": {"type": "boolean"},
    "fix":   {"type": "string", "description": "What to fix: indexes, links, recall_history"}
  }
}
```

Neither field is required. Use `check: true` to audit without modifying anything. Use `fix` to repair a specific subsystem.

**Example (check):**

```json
{
  "jsonrpc": "2.0",
  "id": 22,
  "method": "tools/call",
  "params": {
    "name": "amm_repair",
    "arguments": {
      "check": true
    }
  }
}
```

**Example (fix):**

```json
{
  "jsonrpc": "2.0",
  "id": 23,
  "method": "tools/call",
  "params": {
    "name": "amm_repair",
    "arguments": {
      "fix": "indexes"
    }
  }
}
```

---

### amm_status

Get system status including database path and item counts.

**Input schema:**

```json
{
  "type": "object",
  "properties": {}
}
```

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 24,
  "method": "tools/call",
  "params": {
    "name": "amm_status",
    "arguments": {}
  }
}
```

Response text contains:

```json
{
  "db_path": "/home/user/.amm/amm.db",
  "initialized": true,
  "event_count": 1234,
  "memory_count": 89,
  "summary_count": 23,
  "episode_count": 7,
  "entity_count": 45
}
```

---

### amm_reset_derived

Delete all derived/canonical-derived data and reset event reflection markers while preserving events.

**Input schema:**

```json
{
  "type": "object",
  "properties": {
    "confirm": {"type": "boolean", "description": "Must be true to execute destructive reset"}
  }
}
```

`confirm: true` is required to run the operation. If omitted/false, the tool returns an error message describing the destructive behavior and how to proceed.

**Safety warning:** this operation is irreversible. It deletes derived data (`memories`, `entities`, `relationships`, `claims`, `summaries`, `episodes`, `jobs`, embeddings, caches, and feedback), keeps events, and enables a fresh re-extract via `reflect`.

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 25,
  "method": "tools/call",
  "params": {
    "name": "amm_reset_derived",
    "arguments": {
      "confirm": true
    }
  }
}
```

Response text contains a `ResetDerivedResult` summary including reset/deletion counts.

---

## MCP Client Configuration

### Claude Code

Add to your Claude Code MCP settings (`.claude/settings.json` or global config):

```json
{
  "mcpServers": {
    "amm": {
      "command": "/path/to/amm-mcp",
      "env": {
        "AMM_DB_PATH": "~/.amm/amm.db"
      }
    }
  }
}
```

### Generic MCP Client

Any MCP client that supports stdio transport can connect:

```json
{
  "mcpServers": {
    "amm": {
      "command": "/path/to/amm-mcp",
      "args": [],
      "env": {
        "AMM_DB_PATH": "/absolute/path/to/amm.db"
      }
    }
  }
}
```

## Memory Types

All 16 valid memory types: `identity`, `preference`, `fact`, `decision`, `episode`, `todo`, `relationship`, `procedure`, `constraint`, `incident`, `artifact`, `summary`, `active_context`, `open_loop`, `assumption`, `contradiction`.

## Recall Modes

All 10 recall modes: `ambient`, `facts`, `episodes`, `timeline`, `project`, `entity`, `active`, `history`, `contradictions`, `hybrid`.

## Scopes

Three scopes: `global`, `project`, `session`.

## Memory Statuses

Four statuses: `active`, `superseded`, `archived`, `retracted`.
