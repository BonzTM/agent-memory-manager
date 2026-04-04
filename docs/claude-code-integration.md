# Claude Code Integration Guide

Claude Code is one of several supported runtimes. amm is the memory substrate; Claude Code owns the runtime.

> **Quick links**
>
> - [Example files](../examples/claude-code/) — hook scripts, MCP config, settings.json
> - [Agent Onboarding](agent-onboarding.md) — database init, LLM config, worker scheduling
> - [HTTP API Reference](http-api-reference.md) — REST endpoints for API mode
> - [MCP Reference](mcp-reference.md) — tool definitions and schemas
> - [API-mode examples](../examples/api-mode/) — HTTP transport patterns

## Responsibility Split

| Concern | Claude Code owns | amm owns |
|---|---|---|
| Runtime lifecycle | hook registration, prompt execution, session management | none |
| Memory storage | none | SQLite database, canonical memory/history records |
| Explicit memory tools | MCP subprocess management, tool exposure | `amm-mcp` implementation |
| Event capture | hook firing and payload delivery | event ingestion |
| Maintenance | deciding when jobs run (external schedule) | executing `reflect`, `compress_history`, and other jobs |

## Recommended Shape

Use three pieces together:

1. **MCP** for explicit agent-controlled memory access (`amm_recall`, `amm_expand`, `amm_remember`, `amm_jobs_run`)
2. **Claude Code hooks** for automatic event capture (user messages, assistant responses, tool calls, session lifecycle)
3. **External worker scheduling** for `reflect`, `compress_history`, and the heavier maintenance jobs

## 1. Register `amm-mcp`

Add the MCP server to `~/.claude.json`:

### Option A: MCP-over-stdio (Local)

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

### Option B: MCP-over-HTTP (Remote/Sidecar)

```json
{
  "mcpServers": {
    "amm": {
      "url": "http://localhost:8080/v1/mcp"
    }
  }
}
```

This gives the agent access to all amm tools (`amm_recall`, `amm_remember`, `amm_expand`, `amm_ingest_event`, etc.) via MCP.

## 2. Add Hook-Based Capture

Claude Code exposes lifecycle hooks for automatic event capture. Add these to `~/.claude/settings.json`:

| Hook | Best amm use |
|---|---|
| `UserPromptSubmit` | Ingest the user prompt and return ambient recall hints |
| `AssistantResponse` | Capture assistant replies |
| `PreToolUse` | Record tool invocations |
| `PostToolUse` / `PostToolUseFailure` | Record tool results with success/failure status |
| `Stop` | Record session closeout and run light maintenance |

### Install Hook Scripts

```bash
mkdir -p ~/.amm/hooks

cp examples/claude-code/*.sh ~/.amm/hooks/
chmod +x ~/.amm/hooks/*.sh
```

### Example `~/.claude/settings.json`

```json
{
  "hooks": {
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "$HOME/.amm/hooks/on-user-message.sh"
          }
        ]
      }
    ],
    "AssistantResponse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "$HOME/.amm/hooks/on-assistant-message.sh"
          }
        ]
      }
    ],
    "PreToolUse": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "$HOME/.amm/hooks/on-tool-use.sh pre"
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

If you already have Claude config files, merge only the `mcpServers.amm` and `hooks` sections instead of overwriting them.

## 2.5. Configure Recommended Ingestion Policies

The hooks capture `tool_call` and `tool_result` events. To prevent these from polluting extracted memories, **strongly consider** adding ignore policies:

```bash
amm policy-add --pattern-type kind --pattern "tool_call" --mode ignore --match-mode exact --priority 100
amm policy-add --pattern-type kind --pattern "tool_result" --mode ignore --match-mode exact --priority 100
```

Without these policies, the extraction pipeline treats raw tool invocation JSON as meaningful content, producing low-quality memories. The meaningful information is already captured in `message_user` and `message_assistant` events. See [Configuration: Ingestion Policies](configuration.md#ingestion-policies) for the full reference.

## 3. Keep Workers External

AMM does not ship an internal scheduler loop. Because SQLite only allows one writer at a time, we recommend running maintenance jobs sequentially:

```bash
# Recommended: Serialized Baseline Runner
/path/to/agent-memory-manager/examples/scripts/run-workers.sh
```

The baseline runner covers the full maintenance sequence. See [`examples/scripts/run-workers.sh`](../examples/scripts/run-workers.sh) for the phases.

Choose the trigger that fits:

- host cron or systemd timer (serialized baseline runner)
- the `Stop` hook runs a light warm-path sequence via `on-session-end.sh`
- the agent triggers jobs via MCP (`amm_jobs_run`)

See [Agent Onboarding: Step 7](agent-onboarding.md#step-7-schedule-background-workers) for detailed cron/systemd/agent-triggered scheduling options.

## Configuration

Environment variables configure the hook scripts' transport and capture behavior:

| Env Variable | Default | Description |
|-------------|---------|-------------|
| `AMM_BIN` | `/usr/local/bin/amm` | Path to local `amm` binary |
| `AMM_DB_PATH` | `~/.amm/amm.db` | SQLite database path |
| `AMM_PROJECT_ID` | unset | Stable project identifier for scoped recall |
| `AMM_SESSION_ID` | unset | Session identifier (set automatically by Claude Code hooks) |

## Suggested Operational Pattern

Use a hot/warm/cold split:

- **Hot path**: `UserPromptSubmit` ingests the prompt and returns ambient recall hints; `AssistantResponse` captures replies; `PreToolUse`/`PostToolUse` capture tool activity
- **Warm path**: `Stop` hook runs the session-end sequence (`reflect`, `compress_history`, light maintenance)
- **Cold path**: scheduled jobs run the broader maintenance sequence through the shared runner

## Agent Instructions Snippet

```md
## amm memory usage

- Treat amm as the durable memory system for this repository.
- At task start, repo switch, or resume after interruption, consult amm via `amm_recall` or `amm recall --mode ambient`.
- If amm returns thin recall items, expand only the items you actually need before acting.
- Record only stable, high-confidence memories explicitly with `amm_remember`; let background workers extract the rest from history.
- Do not assume amm runs its own scheduler. Maintenance jobs run externally via `amm jobs run <kind>`.
```

## Verification Checklist

- `/usr/local/bin/amm-mcp` starts successfully with the configured `AMM_DB_PATH`
- Claude Code can see and call the `amm` MCP server
- hook scripts exist and are executable at `~/.amm/hooks/`
- a sample conversation produces `message_user` and `message_assistant` events in `amm history --limit 5`
- `Stop` hook runs session-end maintenance without errors
- scheduled worker runs via `run-workers.sh` complete without errors

## What This Repo Does Not Promise

- a built-in amm scheduler or daemon
- a one-size-fits-all Claude Code configuration
- automatic maintenance without an external trigger
