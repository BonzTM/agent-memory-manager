# Integration Guide

## Overview

AMM (Agent Memory Manager) integrates with agent runtimes through two mechanisms:

1. **Hooks** -- automatic capture of every interaction (events in, ambient recall out).
2. **MCP tools** -- explicit agent-initiated recall, remember, and management.

Both mechanisms ultimately call the same service layer (`internal/service/service.go`), so they produce identical behavior. Choose hooks for transparent capture with no agent awareness, MCP tools when the agent should actively manage its own memory, or combine both for full coverage.

---

## The Capture Loop

The ideal integration flow (per the spec, section 33.1):

1. User sends a message. A hook fires. AMM ingests the message as an event (`kind: message_user`).
2. The hook requests ambient recall. AMM returns 3-7 thin hints scored by the multi-signal retrieval engine.
3. Hints are injected into the agent's context window.
4. The agent responds. A hook fires. AMM ingests the response as an event (`kind: message_assistant`).
5. In the background, workers (`reflect`, `compress_history`, `form_episodes`, etc.) process new events into durable memories.

This loop runs on every turn of conversation, building a growing knowledge base without any explicit agent action.

---

## Hook-Based Integration (Claude Code)

Claude Code supports lifecycle hooks that fire at defined points in the interaction cycle. AMM leverages three:

- **`UserPromptSubmit`** -- fires when the user submits a prompt. Use this to ingest the user message and request ambient recall.
- **`AssistantResponse`** -- fires after the assistant finishes responding. Use this to ingest the assistant's response.
- **`Stop`** -- fires when the session ends. Use this to create a session summary.

### Hook Script Pattern

```bash
#!/bin/bash
# hook-user-prompt.sh
# Fires on UserPromptSubmit: captures the user message, returns ambient hints.

AMM="${AMM_BIN:-$HOME/.local/bin/amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"

# Read the prompt from stdin (Claude Code pipes it in).
PROMPT=$(cat)

# Ingest the user's message as a history event.
echo "{
  \"kind\": \"message_user\",
  \"source_system\": \"claude-code\",
  \"content\": $(printf '%s' "$PROMPT" | jq -Rs .),
  \"session_id\": \"${SESSION_ID}\",
  \"project_id\": \"${PROJECT_ID}\"
}" | AMM_DB_PATH="$DB" "$AMM" ingest event --stdin

# Request ambient recall against the prompt text.
RECALL=$(AMM_DB_PATH="$DB" "$AMM" recall --mode ambient --session "$SESSION_ID" --project "$PROJECT_ID" "$PROMPT" 2>/dev/null)

# Output hints so Claude Code can inject them into context.
if [ -n "$RECALL" ]; then
  echo "$RECALL"
fi
```

```bash
#!/bin/bash
# hook-assistant-response.sh
# Fires on AssistantResponse: captures the assistant's output.

AMM="${AMM_BIN:-$HOME/.local/bin/amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"

RESPONSE=$(cat)

echo "{
  \"kind\": \"message_assistant\",
  \"source_system\": \"claude-code\",
  \"content\": $(printf '%s' "$RESPONSE" | jq -Rs .),
  \"session_id\": \"${SESSION_ID}\",
  \"project_id\": \"${PROJECT_ID}\"
}" | AMM_DB_PATH="$DB" "$AMM" ingest event --stdin
```

### Configuring Hooks in Claude Code

Add these to your `.claude/settings.json`:

```json
{
  "hooks": {
    "UserPromptSubmit": [
      {
        "command": "$HOME/.local/bin/amm-hooks/hook-user-prompt.sh"
      }
    ],
    "AssistantResponse": [
      {
        "command": "$HOME/.local/bin/amm-hooks/hook-assistant-response.sh"
      }
    ]
  }
}
```

---

## MCP-Based Integration

For runtimes that support the Model Context Protocol, AMM runs as an MCP server over stdin/stdout using JSON-RPC 2.0.

### Setup

Configure the MCP server in your runtime's settings. For Claude Code, add to `.claude/settings.json`:

```json
{
  "mcpServers": {
    "amm": {
      "command": "$HOME/.local/bin/amm-mcp",
      "env": {
        "AMM_DB_PATH": "$HOME/.amm/amm.db"
      }
    }
  }
}
```

### Available MCP Tools

| Tool | Description |
|------|-------------|
| `amm_recall` | Retrieve memories using various modes (ambient, facts, episodes, project, entity, history, hybrid, timeline, active) |
| `amm_remember` | Commit a durable memory with type, scope, body, and tight_description |
| `amm_ingest_event` | Log a single interaction event to history |
| `amm_ingest_transcript` | Bulk-ingest a sequence of events |
| `amm_describe` | Get thin descriptions for one or more item IDs |
| `amm_expand` | Expand an item to full detail (memory, summary, or episode) |
| `amm_history` | Query raw interaction history by search text or session |
| `amm_get_memory` | Retrieve a single memory by ID |
| `amm_update_memory` | Update an existing memory (body, status, tight_description) |
| `amm_explain_recall` | Explain why a specific item surfaced for a query |
| `amm_jobs_run` | Run a maintenance job (reflect, compress_history, etc.) |
| `amm_repair` | Run integrity checks and repairs |
| `amm_status` | Get system status (counts, initialization state) |
| `amm_init` | Initialize the AMM database |

### Example: MCP Recall Request

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "amm_recall",
    "arguments": {
      "query": "database migration strategy",
      "opts": {
        "mode": "ambient",
        "project_id": "my-project",
        "session_id": "sess_abc123",
        "limit": 5
      }
    }
  }
}
```

### Example: MCP Remember Request

```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "method": "tools/call",
  "params": {
    "name": "amm_remember",
    "arguments": {
      "type": "decision",
      "scope": "project",
      "body": "We chose PostgreSQL over MySQL for the analytics service because of its better JSON support and window functions.",
      "tight_description": "Chose PostgreSQL for analytics service",
      "project_id": "analytics-svc"
    }
  }
}
```

---

## Ambient Recall Flow

Ambient recall is the default mode, designed for automatic context injection on every turn. It returns a small, high-signal packet that fits in the agent's context without overwhelming it.

### What the Agent Receives

Each ambient recall response contains 3-7 items, each with:

| Field | Description |
|-------|-------------|
| `id` | Unique identifier (e.g., `mem_...`, `sum_...`, `ep_...`) |
| `kind` | Item type: `memory`, `summary`, `episode` |
| `type` | Subtype (e.g., `fact`, `decision`, `preference`, `active_context`) |
| `scope` | Visibility: `global`, `project`, `session` |
| `score` | Relevance score from 0.0 to 1.0 |
| `tight_description` | One-line summary of the item |

### Expand on Demand

The agent can expand any item for full detail using `amm_expand` (MCP) or `amm expand` (CLI), passing the `id` and `kind`. This returns the full body, claims, source events, and related items.

### Repetition Suppression

AMM tracks which items have been shown in the current session. Previously shown items receive a repetition penalty (weight: 0.10) that pushes them down the ranking, ensuring the agent sees fresh information on each turn. The penalty is session-scoped and resets when the session changes.

---

## Recall Modes

AMM supports nine recall modes, selectable via the `--mode` flag (CLI) or `opts.mode` (MCP):

| Mode | Default Limit | Searches | Use Case |
|------|---------------|----------|----------|
| `ambient` | 5 | memories + summaries + episodes | Automatic per-turn context injection |
| `facts` | 10 | memories only | Focused fact retrieval |
| `episodes` | 10 | episodes only | Narrative recall |
| `project` | 10 | memories filtered by project_id | Project-scoped retrieval |
| `entity` | 10 | memories + entities | Entity-focused lookup |
| `history` | 10 | raw events | Interaction history search |
| `hybrid` | 10 | memories + summaries + episodes + events | Full cross-type search |
| `timeline` | 10 | events ordered by occurred_at | Chronological event listing |
| `active` | 10 | active_context memories only | Current working context |

---

## Background Processing

After events accumulate, background workers extract structure and consolidate knowledge. Each worker is a maintenance job run via `amm jobs run <kind>` (CLI) or `amm_jobs_run` (MCP).

### Workers

| Job Kind | What It Does |
|----------|--------------|
| `reflect` | Scans unprocessed events, extracts candidate durable memories using phrase-cue heuristics (preferences, decisions, facts, open loops, constraints) |
| `compress_history` | Creates summaries from groups of raw events |
| `consolidate_sessions` | Produces session-level summaries |
| `extract_claims` | Extracts structured assertions from memories |
| `form_episodes` | Groups related events into narrative episodes |
| `detect_contradictions` | Finds conflicting memories |
| `decay_stale_memory` | Reduces importance of untouched memories over time |
| `merge_duplicates` | Consolidates duplicate memories |
| `rebuild_indexes` | Rebuilds FTS5 full-text search indexes |
| `cleanup_recall_history` | Purges recall tracking entries older than 7 days |

### Running Workers

Workers can be triggered manually, on a schedule, or after a threshold of new events:

```bash
# Manual
amm jobs run reflect
amm jobs run compress_history

# Cron (every 30 minutes)
*/30 * * * * AMM_DB_PATH=$HOME/.amm/amm.db $HOME/.local/bin/amm jobs run reflect
*/30 * * * * AMM_DB_PATH=$HOME/.amm/amm.db $HOME/.local/bin/amm jobs run compress_history

# Full maintenance pass
for job in reflect compress_history consolidate_sessions extract_claims form_episodes detect_contradictions decay_stale_memory merge_duplicates; do
  amm jobs run "$job"
done
```

---

## Ingestion Policies

Ingestion policies control what gets captured and whether captured events can produce memories. Policies match on session, project, agent, source system, or surface.

### Modes

| Mode | Events Stored | Memories Created |
|------|---------------|------------------|
| `full` | Yes | Yes |
| `read_only` | Yes | No (events tagged `ingestion_mode=read_only`, skipped by Reflect) |
| `ignore` | No | No (event silently dropped) |

### How Policies Are Evaluated

Policies are checked in priority order against the incoming event:

1. `session` -- matches `session_id`
2. `project` -- matches `project_id`
3. `agent` -- matches `agent_id`
4. `source` -- matches `source_system`
5. `surface` -- matches `surface`

The first matching policy wins. If no policy matches, the default mode is `full`.

### Use Cases

- Set a project to `read_only` to capture history without polluting the memory store during exploratory work.
- Set a session to `ignore` to completely exclude a debugging or testing session.
- Set a source system to `read_only` to capture events from a CI pipeline without generating memories.
