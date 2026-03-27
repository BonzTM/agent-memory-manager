# MCP Reference

amm exposes all its functionality as MCP (Model Context Protocol) tools over a stdio JSON-RPC 2.0 transport. One JSON message per line, newline-delimited.

## Starting the Server

```bash
CGO_ENABLED=1 go build -tags fts5 -o amm-mcp ./cmd/amm-mcp
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
      "version": "0.1.0"
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

**Recall modes:** `ambient`, `facts`, `episodes`, `timeline`, `project`, `entity`, `active`, `history`, `hybrid` (default).

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
    "kind": {"type": "string", "description": "Item kind: memory, summary, episode"},
    "session_id": {"type": "string", "description": "Session identifier for relevance feedback"}
  },
  "required": ["id"]
}
```

When `kind` is omitted, the server infers it from the ID prefix (`mem_`, `sum_`, `ep_`).

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

**Available job kinds (20):**

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

All 9 recall modes: `ambient`, `facts`, `episodes`, `timeline`, `project`, `entity`, `active`, `history`, `hybrid`.

## Scopes

Three scopes: `global`, `project`, `session`.

## Memory Statuses

Four statuses: `active`, `superseded`, `archived`, `retracted`.
