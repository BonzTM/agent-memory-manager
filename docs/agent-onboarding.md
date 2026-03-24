# Agent Onboarding Guide

This is a step-by-step guide designed to be handed to an AI agent so it can set up amm for its user with minimal human intervention.

Use Steps 1-2 for every runtime. After that, choose the runtime-specific path that matches the user's host. This repo still ships the richest end-to-end reference hooks for Claude Code, but the operating model is intentionally cross-runtime:

- [Codex Integration](codex-integration.md)
- [Hermes-Agent Integration](hermes-agent-integration.md)
- [OpenClaw Integration](openclaw-integration.md)
- [OpenCode Integration](opencode-integration.md)

In every runtime, the worker model stays the same: amm background jobs are external `amm jobs run <kind>` calls against the amm database, not a built-in scheduler.

## Runtime-Neutral Operating Contract

Once amm is installed, the agent should follow the same durable-memory rules regardless of runtime:

1. **Recall first.** At task start, repo switch, or resume after interruption, query AMM with `amm_recall` or `amm recall --mode ambient`.
2. **Expand on demand.** If AMM returns thin recall items, expand only the items needed for the current task.
3. **Remember only stable knowledge.** Use `amm_remember` or `amm remember` for decisions, preferences, constraints, and other high-confidence facts that should survive the current session.
4. **Let capture stay honest.** Hooks and plugins should capture what the runtime can really expose, not what we wish it exposed.
5. **Keep workers external.** Reflection, compression, and heavier maintenance stay outside the runtime boundary as `amm jobs run <kind>` calls.

If the repo also uses ACM, the agent should use ACM for task workflow and AMM for durable memory.

---

## Prerequisites Check

```bash
# Verify Go is installed (1.21+ required)
go version

# Verify CGO is available (required for SQLite with FTS5)
CGO_ENABLED=1 go env CGO_ENABLED
# Expected output: 1

# Verify jq is available (used by hook scripts)
jq --version
```

If `CGO_ENABLED` does not print `1`, the user needs a C compiler installed (e.g., `gcc` or `clang`). On Debian/Ubuntu: `sudo apt install build-essential`. On macOS: `xcode-select --install`.

---

## Step 1: Build amm

```bash
cd /path/to/agent-memory-manager

mkdir -p /tmp/amm-build

# Build the CLI binary
CGO_ENABLED=1 go build -tags fts5 -o /tmp/amm-build/amm ./cmd/amm

# Build the MCP server binary
CGO_ENABLED=1 go build -tags fts5 -o /tmp/amm-build/amm-mcp ./cmd/amm-mcp

# Install both binaries globally
sudo install -m 755 /tmp/amm-build/amm /usr/local/bin/amm
sudo install -m 755 /tmp/amm-build/amm-mcp /usr/local/bin/amm-mcp

# Verify both binaries exist
ls -la /usr/local/bin/amm /usr/local/bin/amm-mcp
```

The `-tags fts5` flag enables SQLite full-text search, which amm requires for retrieval.

---

## Step 2: Initialize the Database

```bash
# Create the database directory and run migrations
AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm init

# Verify initialization
AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm status
```

Expected output from `status` should show `initialized: true` with all counts at 0.

---

## Choose Your Runtime Path

After Steps 1-2, pick the path that matches the user's host:

| Runtime | Start here | What you get |
|---|---|---|
| Claude Code | Continue below with Steps 3-7 | Full MCP + public hook reference implementation |
| Codex | [Codex Integration](codex-integration.md) | MCP + Codex hooks + transcript-aware closeout |
| OpenCode | [OpenCode Integration](opencode-integration.md) | MCP + local plugin glue + explicit recall |
| OpenClaw | [OpenClaw Integration](openclaw-integration.md) | MCP sidecar + native hooks |
| Hermes-Agent | [Hermes-Agent Integration](hermes-agent-integration.md) | MCP + sidecar/helper-script pattern |

The Claude sections below remain the most detailed copy-paste walkthrough, but they are no longer the only mental model.

---

## Step 3: Configure for Claude Code (full reference path)

### 3a: Register the MCP Server

Add the following to `~/.claude.json` for global config:

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

This gives the agent access to all amm tools (`amm_recall`, `amm_remember`, `amm_ingest_event`, etc.) via MCP.

### 3b: Verify MCP Server

```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' | AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm-mcp
```

Expected: a JSON response containing `protocolVersion`, `serverInfo.name: "amm-mcp"`, and `capabilities.tools`.

```bash
echo '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' | AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm-mcp
```

Expected: a JSON response listing all available tools (amm_recall, amm_remember, amm_ingest_event, etc.).

---

## Step 4: Set Up Automatic Capture Hooks

For fuller transparent memory capture, add hooks to `~/.claude/settings.json`. These run alongside the MCP server -- hooks capture interactions automatically, while MCP tools let the agent act deliberately.

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

Create the hook scripts directory and scripts:

```bash
mkdir -p ~/.amm/hooks
```

**on-user-message.sh:**

```bash
#!/bin/bash
AMM="${AMM_BIN:-/usr/local/bin/amm}"
DB="${AMM_DB_PATH:-$HOME/.amm/amm.db}"

PROMPT=$(cat)

echo "{
  \"kind\": \"message_user\",
  \"source_system\": \"claude-code\",
  \"content\": $(printf '%s' "$PROMPT" | jq -Rs .),
  \"session_id\": \"${SESSION_ID}\",
  \"project_id\": \"${PROJECT_ID}\"
}" | AMM_DB_PATH="$DB" "$AMM" ingest event --in -

RECALL=$(AMM_DB_PATH="$DB" "$AMM" recall --mode ambient --session "$SESSION_ID" --project "$PROJECT_ID" "$PROMPT" 2>/dev/null)

if [ -n "$RECALL" ]; then
  echo "$RECALL"
fi
```

**on-session-end.sh:**

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

**on-tool-use.sh:**

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

Make them executable:

```bash
chmod +x ~/.amm/hooks/on-user-message.sh
chmod +x ~/.amm/hooks/on-session-end.sh
chmod +x ~/.amm/hooks/on-tool-use.sh
```

See `docs/integration.md` for more detail on the hook-based capture loop.

This still does not give Claude a public per-turn assistant-response hook. The stronger setup captures tool results immediately, but the assistant text itself is still captured at `Stop` via `last_assistant_message`.

---

## Step 5: Verify the Integration

Run these commands to confirm everything works end-to-end:

```bash
# Test remember: commit a memory
AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm remember \
  --type fact \
  --scope global \
  --body "amm is now configured and ready to use" \
  --tight "amm configured"

# Test recall: retrieve the memory just created
AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm recall --mode ambient "amm configured"

# Test status: confirm counts updated
AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm status
```

Expected: `status` should now show `memory_count: 1`.

---

## Step 6: Seed Initial Memories

Help the user bootstrap their memory store with foundational knowledge. Use `amm_remember` (MCP) or `amm remember` (CLI) to create memories in these categories:

### User Preferences

```bash
amm remember --type preference --scope global \
  --body "User prefers concise responses with code examples over lengthy explanations" \
  --tight "Prefers concise responses with code"

amm remember --type preference --scope global \
  --body "User uses Neovim as their primary editor" \
  --tight "Primary editor: Neovim"
```

### Project Facts

```bash
amm remember --type fact --scope project --project "my-project" \
  --body "The project is a Go REST API using Chi router with PostgreSQL, deployed on Fly.io" \
  --tight "Go REST API with Chi, PostgreSQL, Fly.io"

amm remember --type decision --scope project --project "my-project" \
  --body "We chose sqlc over GORM for database access because we want type-safe queries without the ORM overhead" \
  --tight "Using sqlc over GORM for type-safe queries"
```

### Active Context

```bash
amm remember --type active_context --scope project --project "my-project" \
  --body "Currently implementing the user authentication flow with OAuth2 and JWT tokens" \
  --tight "Working on OAuth2/JWT auth flow"
```

### Constraints

```bash
amm remember --type constraint --scope project --project "my-project" \
  --body "All API responses must use the standard envelope format: {data, error, meta}" \
  --tight "API responses use standard envelope format"
```

---

## Step 7: Schedule Background Workers

Background workers extract structure from raw events. Without them, amm only stores what you explicitly `remember`. With them, amm automatically discovers memories from conversation history.

### Option A: Cron (simplest)

```bash
# Add to crontab (crontab -e)
# Run reflect and compress every 30 minutes
*/30 * * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run reflect >/dev/null 2>&1
*/30 * * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run compress_history >/dev/null 2>&1

# Run heavier jobs once per hour
0 * * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run consolidate_sessions >/dev/null 2>&1
0 * * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run extract_claims >/dev/null 2>&1
0 * * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run form_episodes >/dev/null 2>&1

# Run maintenance jobs once per day at 3am
0 3 * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run detect_contradictions >/dev/null 2>&1
0 3 * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run decay_stale_memory >/dev/null 2>&1
0 3 * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run merge_duplicates >/dev/null 2>&1
0 3 * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run cleanup_recall_history >/dev/null 2>&1
```

### Option B: systemd Timer (Linux)

Create `~/.config/systemd/user/amm-maintenance.service`:

```ini
[Unit]
Description=amm background maintenance

[Service]
Type=oneshot
Environment=AMM_DB_PATH=%h/.amm/amm.db
ExecStart=/usr/local/bin/amm jobs run reflect
ExecStart=/usr/local/bin/amm jobs run compress_history
ExecStart=/usr/local/bin/amm jobs run consolidate_sessions
ExecStart=/usr/local/bin/amm jobs run extract_claims
ExecStart=/usr/local/bin/amm jobs run form_episodes
```

Create `~/.config/systemd/user/amm-maintenance.timer`:

```ini
[Unit]
Description=Run amm maintenance every 30 minutes

[Timer]
OnCalendar=*:0/30
Persistent=true

[Install]
WantedBy=timers.target
```

Enable:

```bash
systemctl --user daemon-reload
systemctl --user enable --now amm-maintenance.timer
```

### Option C: Agent-Triggered

The agent can run workers via MCP whenever it judges the time is right:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "amm_jobs_run",
    "arguments": { "kind": "reflect" }
  }
}
```

---

## Verification Checklist

Run through this checklist to confirm everything is working:

- [ ] `amm status` returns `initialized: true`
- [ ] `amm remember` followed by `amm recall` round-trips successfully (the remembered item appears in recall results)
- [ ] MCP server responds to `initialize` and `tools/list` requests
- [ ] Hooks are configured in `.claude/settings.json` (if using Claude Code)
- [ ] Hook scripts exist and are executable at the configured paths
- [ ] Background workers are scheduled (cron, systemd timer, or agent-triggered)
- [ ] At least one background worker runs successfully: `amm jobs run reflect`

---

## Troubleshooting

**`amm: command not found`** -- Ensure the install location is on your PATH (for this guide, `/usr/local/bin`), or use the full path to the binary.

**`database is locked`** -- SQLite allows only one writer at a time. If a hook and a cron job collide, one will briefly block. This is normal and resolves automatically. If it persists, check for zombie processes.

**`no memories returned from recall`** -- Verify memories exist with `amm status`. If `memory_count` is 0, either `remember` some memories or run `amm jobs run reflect` to extract them from events.

**MCP server returns parse errors** -- Ensure you are sending one JSON-RPC message per line. The MCP server reads newline-delimited JSON from stdin.

**CGO build errors** -- Install a C compiler. On Debian/Ubuntu: `sudo apt install build-essential`. On macOS: `xcode-select --install`. On Alpine: `apk add gcc musl-dev`.
