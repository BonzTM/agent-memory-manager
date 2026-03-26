# Integration Guide

## Overview

amm (Agent Memory Manager) integrates with agent runtimes through two mechanisms:

1. **Hooks** -- automatic capture of every interaction (events in, ambient recall out).
2. **MCP tools** -- explicit agent-initiated recall, remember, and management.

Both mechanisms ultimately call the same service layer (`internal/service/service.go`), so they produce identical behavior. Choose hooks for transparent capture with no agent awareness, MCP tools when the agent should actively manage its own memory, or combine both for full coverage.

amm's maintenance jobs stay outside the runtime boundary. In practice, that means background work is triggered by external `amm jobs run <kind>` calls against the same SQLite database -- from cron, systemd, a runtime hook, or a runtime-owned background process -- rather than by an internal amm scheduler.

## Runtime Guides

Use this page for the shared model, then jump to the runtime-specific companion that matches your agent host:

| Runtime | Best fit | Guide |
|---|---|---|
| Codex | MCP + hooks + repo instructions | [Codex Integration](codex-integration.md) |
| Hermes-agent | MCP + hook handlers + scheduled workers | [Hermes-Agent Integration](hermes-agent-integration.md) |
| OpenClaw | MCP sidecar + native hooks + explicit recall (`examples/openclaw/`) | [OpenClaw Integration](openclaw-integration.md) |
| OpenCode | MCP + local plugin glue + explicit recall (`examples/opencode/`) | [OpenCode Integration](opencode-integration.md) |
| Claude Code | Complete reference implementation shipped in this repo | This page + `examples/claude-code/` |

---

## Runtime-Neutral Operating Contract

Regardless of runtime, the default AMM operating loop is:

1. **Recall on entry.** At task start, repo switch, or resume after interruption, ask AMM for a thin recall packet.
2. **Expand only what matters.** Use `amm_expand` / `amm expand` only for the items needed to make the current decision.
3. **Remember only durable knowledge.** Explicitly commit stable decisions, preferences, facts, and constraints. Let transient chat flow stay in history.
4. **Keep capture truthful.** If a runtime cannot expose a hook or transcript surface, do not pretend it can; fall back to explicit recall and lightweight lifecycle markers.
5. **Keep workers external.** `reflect`, `compress_history`, and heavier jobs stay outside the runtime boundary.

If a repo also uses ACM, keep the responsibility split explicit: ACM governs task workflow (`context`, `work`, `verify`, `review`, `done`), while AMM governs durable memory (`recall`, `expand`, `remember`, `jobs run`).

## Minimum Capture Contract

Every runtime integration does not need perfect fidelity, but it should stay consistent about the fields it *does* produce:

| Field | Why it matters | Fallback guidance |
|---|---|---|
| `source_system` | Distinguishes Claude, Codex, OpenCode, OpenClaw, etc. | Always set a truthful runtime identifier. |
| `session_id` | Keeps recall and repetition penalties session-aware | Omit only when the runtime truly provides no stable session identity. |
| `project_id` | Prevents project memory bleed | Set whenever the runtime knows the active repo/project. |
| `occurred_at` | Preserves temporal ordering | Let AMM timestamp on ingest only when the runtime does not provide it. |
| `content` | Stores the actual user/tool/assistant payload | Keep it thin and faithful to the exposed surface. |
| `metadata` | Captures hook names, tool names, transcript provenance, or fallbacks | Prefer precise metadata over overloading `content`. |

When a field is unavailable, document the fallback behavior in the runtime guide instead of inventing fake completeness.

---

## The Capture Loop

The ideal integration flow (per the spec, section 33.1):

1. User sends a message. A hook fires. amm ingests the message as an event (`kind: message_user`).
2. The hook requests ambient recall. amm returns 3-7 thin hints scored by the multi-signal retrieval engine.
3. Hints are injected into the agent's context window.
4. The agent responds. A hook fires. amm ingests the response as an event (`kind: message_assistant`).
5. In the background, workers (`reflect`, `compress_history`, `form_episodes`, etc.) process new events into durable memories.

This loop runs on every turn of conversation, building a growing knowledge base without any explicit agent action.

---

## Hook-Based Integration (Claude Code Reference Implementation)

Claude Code supports lifecycle hooks that fire at defined points in the interaction cycle. The shipped amm reference example uses four of them:

- **`UserPromptSubmit`** -- fires when the user submits a prompt. Use this to ingest the user message and request ambient recall.
- **`PostToolUse`** -- fires after a successful tool run. Use this to capture tool results into amm.
- **`PostToolUseFailure`** -- fires after a failed tool run. Use this to capture errorful tool results into amm.
- **`Stop`** -- fires when the session ends. Use this to capture the final assistant message and trigger warm-path maintenance jobs.

### Hook Script Pattern

```bash
#!/bin/bash

AMM="${AMM_BIN:-/usr/local/bin/amm}"
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
}" | AMM_DB_PATH="$DB" "$AMM" ingest event --in -

# Request ambient recall against the prompt text.
RECALL=$(AMM_DB_PATH="$DB" "$AMM" recall --mode ambient --session "$SESSION_ID" --project "$PROJECT_ID" "$PROMPT" 2>/dev/null)

# Output hints so Claude Code can inject them into context.
if [ -n "$RECALL" ]; then
  echo "$RECALL"
fi
```

```bash
#!/bin/bash

AMM="${AMM_BIN:-/usr/local/bin/amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"

PAYLOAD=$(cat)

EVENT=$(printf '%s' "$PAYLOAD" | python3 -c '
import json, sys
payload = json.load(sys.stdin)
last_assistant_message = payload.get("last_assistant_message") or ""
if last_assistant_message:
    print(json.dumps({
        "kind": "message_assistant",
        "source_system": "claude-code",
        "content": last_assistant_message,
        "metadata": {"hook_event": "Stop"},
    }))
else:
    print("")
')

[ -n "$EVENT" ] && printf '%s' "$EVENT" | AMM_DB_PATH="$DB" "$AMM" ingest event --in - >/dev/null 2>&1 || true
printf '%s' '{"kind":"session_stop","source_system":"claude-code","content":"Claude stop hook executed.","metadata":{"hook_event":"Stop"}}' | AMM_DB_PATH="$DB" "$AMM" ingest event --in - >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run reflect >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run compress_history >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run consolidate_sessions >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run extract_claims >/dev/null 2>&1 || true
AMM_DB_PATH="$DB" "$AMM" jobs run form_episodes >/dev/null 2>&1 || true
```

```bash
#!/bin/bash

AMM="${AMM_BIN:-/usr/local/bin/amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"
STATUS="${1:-success}"
PAYLOAD=$(cat)

EVENT=$(printf '%s' "$PAYLOAD" | STATUS="$STATUS" python3 -c '
import json, os, sys
payload = json.load(sys.stdin)
tool_result = payload.get("tool_result") or payload.get("toolResult")
tool_name = payload.get("tool_name") or payload.get("toolName") or "unknown_tool"
tool_input = payload.get("tool_input") or payload.get("toolInput")
content = tool_result if isinstance(tool_result, str) else json.dumps(tool_result)
print(json.dumps({
    "kind": "tool_result",
    "source_system": "claude-code",
    "content": content,
    "metadata": {
        "hook_event": "PostToolUse" if os.environ["STATUS"] == "success" else "PostToolUseFailure",
        "tool_name": tool_name,
        "tool_input": tool_input if isinstance(tool_input, str) or tool_input is None else json.dumps(tool_input),
    },
}))
')

printf '%s' "$EVENT" | AMM_DB_PATH="$DB" "$AMM" ingest event --in - >/dev/null 2>&1
```

### Configuring Hooks in Claude Code

Add these to your `~/.claude/settings.json`:

```json
{
  "hooks": {
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "$HOME/.amm/hooks/on-user-message.sh \"$PROMPT\""
          }
        ]
      }
    ],
    "PostToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "$HOME/.amm/hooks/on-tool-use.sh success"
          }
        ]
      }
    ],
    "PostToolUseFailure": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "$HOME/.amm/hooks/on-tool-use.sh failure"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "$HOME/.amm/hooks/on-session-end.sh"
          }
        ]
      }
    ]
  }
}
```

This is stronger than the earlier two-hook setup, but it still has one real limit: Claude's public hook surface does not provide a separate per-turn assistant-output hook, so the shipped example captures the final assistant message at `Stop` instead of after every reply.

---

## MCP-Based Integration

For runtimes that support the Model Context Protocol, amm runs as an MCP server over stdin/stdout using JSON-RPC 2.0.

### Setup

Configure the MCP server in your runtime's settings. For Claude Code, add to `~/.claude.json`:

```json
{
  "mcpServers": {
    "amm": {
      "command": "/usr/local/bin/amm-mcp",
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
| `amm_init` | Initialize the amm database |

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

amm tracks which items have been shown in the current session. Previously shown items receive a repetition penalty (weight: 0.10) that pushes them down the ranking, ensuring the agent sees fresh information on each turn. The penalty is session-scoped and resets when the session changes.

---

## Recall Modes

amm supports nine recall modes, selectable via the `--mode` flag (CLI) or `opts.mode` (MCP):

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
| `rebuild_indexes` | Rebuild FTS5 and generate embeddings for items missing them (incremental) |
| `rebuild_indexes_full` | Rebuild FTS5 and regenerate all embeddings from scratch |
| `cleanup_recall_history` | Purges recall tracking entries older than 7 days |

### Running Workers

Workers can be triggered manually, on a schedule, or after a threshold of new events. Because SQLite supports only one concurrent writer, we recommend running maintenance jobs sequentially.

#### Baseline vs. Optional Maintenance

We distinguish between **baseline** jobs (essential for daily memory building) and **optional** jobs (aggressive optimization or repair).

- **Baseline Jobs**: The full sequence included in the shared runner: `reflect`, `compress_history`, `consolidate_sessions`, `merge_duplicates`, `extract_claims`, `form_episodes`, `detect_contradictions`, `decay_stale_memory`, `promote_high_value`, `archive_session_traces`, `rebuild_indexes`, `cleanup_recall_history`.
- **System Repairs**: Structural repairs like `repair_links` are not part of the baseline; run them via `amm repair --fix links` as needed.

```bash
# Recommended: Serialized Baseline Runner
# This script runs all maintenance jobs sequentially.
/path/to/agent-memory-manager/examples/scripts/run-workers.sh

# Optional: Low-cadence Structural Repair
amm repair --fix links

# Alternative: Staggered Cron (avoid overlapping minutes)
0,30 * * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run reflect
5,35 * * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run compress_history
```

The `maintenance.auto_*` configuration flags are runtime configuration values, not proof of an internal amm worker daemon. Use an external trigger unless your runtime explicitly shells out to `amm jobs run ...` for you.

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
