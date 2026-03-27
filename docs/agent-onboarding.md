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

## Step 2b: (Optional) Enable LLM-Backed Extraction

By default, amm extracts memories from events using a heuristic phrase-cue system. For significantly higher extraction quality, configure an LLM endpoint. This is optional — amm works without it.

```bash
# For OpenAI
export AMM_SUMMARIZER_ENDPOINT=https://api.openai.com/v1
export AMM_SUMMARIZER_API_KEY=sk-your-key-here
export AMM_SUMMARIZER_MODEL=gpt-4o-mini

# For a local Ollama instance
export AMM_SUMMARIZER_ENDPOINT=http://localhost:11434/v1
export AMM_SUMMARIZER_API_KEY=ollama
export AMM_SUMMARIZER_MODEL=llama3.2
```

To make these persistent, add them to `~/.amm/config.toml`:

```toml
[summarizer]
endpoint = "https://api.openai.com/v1"
api_key = "sk-your-key-here"
model = "gpt-4o-mini"
```

When enabled, the `reflect` and `compress_history` workers use the LLM for structured extraction and summarization. If the LLM is unavailable, the workers automatically fall back to heuristics — no data is lost.

Verify with:

```bash
AMM_DB_PATH=~/.amm/amm.db AMM_SUMMARIZER_ENDPOINT=https://api.openai.com/v1 AMM_SUMMARIZER_API_KEY=sk-... /usr/local/bin/amm jobs run reflect
```

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

To enable LLM-backed extraction via MCP, add the LLM variables to the env block:

```json
{
  "mcpServers": {
    "amm": {
      "command": "/usr/local/bin/amm-mcp",
      "env": {
        "AMM_DB_PATH": "$HOME/.amm/amm.db",
        "AMM_SUMMARIZER_ENDPOINT": "https://api.openai.com/v1",
        "AMM_SUMMARIZER_API_KEY": "sk-your-key-here",
        "AMM_SUMMARIZER_MODEL": "gpt-4o-mini"
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

The `on-session-end.sh` script typically triggers a lightweight maintenance sequence (e.g., `reflect` → `rebuild_indexes`) to ensure the session's knowledge is distilled immediately.

Install the maintained hook scripts from this repo (instead of inlining them into docs):

```bash
mkdir -p ~/.amm/hooks
cp /path/to/agent-memory-manager/examples/claude-code/*.sh ~/.amm/hooks/
chmod +x ~/.amm/hooks/*.sh
```

See `docs/integration.md` for more detail on the hook-based capture loop.

With `UserPromptSubmit`, `AssistantResponse`, `PreToolUse`, and `PostToolUse*` enabled, AMM captures a full transcript stream: user messages, assistant responses, tool calls, and tool results (plus session-stop metadata).

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

SQLite allows only one writer at a time. To avoid "database is locked" errors, we recommend running maintenance jobs sequentially using the shared worker runner script.

### Option A: Serialized Runner (Baseline)

This approach uses a single script to run the full maintenance sequence one after another. This is the recommended default for most users.

```bash
# Add to crontab (crontab -e)
# Run the serialized worker runner every 30 minutes
*/30 * * * * AMM_DB_PATH=$HOME/.amm/amm.db /path/to/agent-memory-manager/examples/scripts/run-workers.sh >/dev/null 2>&1
```

The baseline runner follows a 6-phase structure to ensure clean dependencies:

1. **Phase 1: Reflection** — `reflect` extracts candidates from events.
2. **Phase 2: Initial Indexing** — `rebuild_indexes` builds embeddings so downstream jobs can use semantic scoring.
3. **Phase 3: Compression** — `compress_history`, `consolidate_sessions`, `build_topic_summaries` structure the raw history.
4. **Phase 4: Linking** — `merge_duplicates`, `extract_claims`, `enrich_memories`, `rebuild_entity_graph`, `form_episodes` build the knowledge graph.
5. **Phase 5: Quality** — `detect_contradictions`, `decay_stale_memory`, `lifecycle_review`, `cross_project_transfer`, `archive_session_traces` refine the store.
6. **Phase 6: Finalization** — `rebuild_indexes` (catches items from phases 3-5), `cleanup_recall_history`, `update_ranking_weights` finalize the cycle.

### Option B: Structural Repair (As Needed)

Structural repairs are not part of the baseline runner and should be run manually or on a slow cadence when integrity issues are suspected.

```bash
# Optional: Link Repair (e.g., weekly on Sunday at 5am)
0 5 * * 0 AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm repair --fix links >/dev/null 2>&1
```

### Option C: Staggered Cron (Alternative)

If you prefer individual cron entries, you must stagger them so they do not fire on the same minute. SQLite's single-writer model means overlapping write-heavy jobs will block each other.

```bash
# Add to crontab (crontab -e)
# Stagger frequent extraction/compression jobs
0,30 * * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run reflect >/dev/null 2>&1
5,35 * * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run compress_history >/dev/null 2>&1
10 * * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run consolidate_sessions >/dev/null 2>&1
15 * * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run build_topic_summaries >/dev/null 2>&1

# Stagger extraction/enrichment and episode formation
20 * * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run extract_claims >/dev/null 2>&1
25 * * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run enrich_memories >/dev/null 2>&1
30 * * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run rebuild_entity_graph >/dev/null 2>&1
35 * * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run form_episodes >/dev/null 2>&1

# Run dedupe/lifecycle/value-transfer jobs on a slower cadence
0 2 * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run merge_duplicates >/dev/null 2>&1
10 2 * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run detect_contradictions >/dev/null 2>&1
20 2 * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run decay_stale_memory >/dev/null 2>&1
40 2 * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run lifecycle_review >/dev/null 2>&1
50 2 * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run cross_project_transfer >/dev/null 2>&1

# Run archive/index/ranking hygiene overnight
0 3 * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run archive_session_traces >/dev/null 2>&1
10 3 * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run rebuild_indexes >/dev/null 2>&1
20 3 * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run cleanup_recall_history >/dev/null 2>&1
30 3 * * * AMM_DB_PATH=$HOME/.amm/amm.db /usr/local/bin/amm jobs run update_ranking_weights >/dev/null 2>&1
```

### Option D: systemd Timer (Linux)

Create `~/.config/systemd/user/amm-maintenance.service`:

```ini
[Unit]
Description=amm background maintenance

[Service]
Type=oneshot
Environment=AMM_DB_PATH=%h/.amm/amm.db
# Optional: Enable LLM-backed extraction
# Environment=AMM_SUMMARIZER_ENDPOINT=https://api.openai.com/v1
# Environment=AMM_SUMMARIZER_API_KEY=sk-your-key-here
# Environment=AMM_SUMMARIZER_MODEL=gpt-4o-mini
ExecStart=/path/to/agent-memory-manager/examples/scripts/run-workers.sh
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

### Option E: Agent-Triggered

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

## Resetting and Re-distilling

If you change your summarizer model, update your extraction prompts, or want to re-baseline memory quality from the same raw history:

1. **Purge derived data** while keeping events:
   ```bash
   amm reset-derived --confirm
   ```
2. **Re-run the extraction pipeline**:
   ```bash
   # Run the full worker sequence
   ./examples/scripts/run-workers.sh
   ```
   Or use `amm jobs run reprocess_all` to unconditionally re-extract using the full endgame logic (triage, entity linking, processing ledger).

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
