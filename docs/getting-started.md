# Getting Started with AMM

This guide walks you through installing, configuring, and using AMM (Agent Memory Manager) from scratch.

## Prerequisites

- Go 1.21 or later
- C compiler (gcc or clang) for CGO
- SQLite development headers

On Ubuntu/Debian:
```bash
sudo apt-get install build-essential libsqlite3-dev
```

On macOS:
```bash
xcode-select --install
```

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/bonztm/agent-memory-manager.git
cd agent-memory-manager

# Build both binaries
CGO_ENABLED=1 go build -tags fts5 -o amm ./cmd/amm
CGO_ENABLED=1 go build -tags fts5 -o amm-mcp ./cmd/amm-mcp

# Install to your PATH
mv amm amm-mcp ~/.local/bin/
```

### Using go install

```bash
CGO_ENABLED=1 go install -tags fts5 github.com/bonztm/agent-memory-manager/cmd/amm@latest
CGO_ENABLED=1 go install -tags fts5 github.com/bonztm/agent-memory-manager/cmd/amm-mcp@latest
```

## Initial Setup

### 1. Initialize the Database

```bash
amm init
```

This creates a SQLite database at `~/.amm/amm.db` with all necessary tables and indexes.

To use a custom location:
```bash
AMM_DB_PATH=/path/to/custom.db amm init
```

### 2. Verify Installation

```bash
amm status
```

You should see output like:
```json
{
  "ok": true,
  "command": "status",
  "timestamp": "2026-03-25T12:00:00Z",
  "result": {
    "db_path": "/home/user/.amm/amm.db",
    "initialized": true,
    "event_count": 0,
    "memory_count": 0
  }
}
```

## Basic Usage

### Storing Your First Memory

```bash
amm remember \
  --type preference \
  --scope global \
  --subject "editor" \
  --body "I prefer using Neovim with a dark color scheme" \
  --tight "Prefers Neovim dark theme"
```

This creates a durable memory that will be available across all sessions.

### Recalling Memories

```bash
# General recall (hybrid mode by default)
amm recall "editor preferences"

# Facts-only mode
amm recall "color scheme" --mode facts

# Ambient mode (fast, for real-time context)
amm recall "what editor do I use" --mode ambient
```

### Ingesting Events

AMM can ingest raw events from agent interactions:

```bash
echo '{
  "kind": "message_user",
  "source_system": "cli",
  "content": "Set up the database schema",
  "session_id": "sess_001",
  "project_id": "myproject"
}' | amm ingest event
```

Or bulk ingest a transcript:

```bash
amm ingest transcript --in session-transcript.json
```

## Memory Types

AMM supports 16 memory types. Here are the most common:

| Type | Use Case |
|------|----------|
| `preference` | User likes/dislikes |
| `fact` | Verified information |
| `decision` | Choices and rationale |
| `procedure` | How-to steps |
| `constraint` | Hard requirements |

Example of each:

```bash
# Preference
amm remember --type preference --scope global \
  --body "User prefers concise replies" \
  --tight "Prefers concise communication"

# Fact
amm remember --type fact --scope project --project myapp \
  --body "The API uses JWT for authentication" \
  --tight "API uses JWT auth"

# Decision
amm remember --type decision --scope project --project myapp \
  --body "Chose PostgreSQL over MySQL for JSON support" \
  --tight "Using PostgreSQL for JSON features"
```

## Retrieval Modes

Different modes for different use cases:

```bash
# Ambient - fast, general purpose
amm recall "current context" --mode ambient

# Facts - factual information
amm recall "database" --mode facts

# Episodes - narrative accounts
amm recall "deployment" --mode episodes

# Timeline - chronological
amm recall "yesterday" --mode timeline

# Project-scoped
amm recall "api design" --mode project --project myapp

# Entity-focused
amm recall "postgres" --mode entity

# Active items
amm recall "open items" --mode active

# Raw history
amm recall "error" --mode history
```

## Working with the Memory System

### Describing Items

Get thin descriptions of memories without full expansion:

```bash
amm describe mem_abc123 mem_def456
```

### Expanding Items

Get full details including linked claims and events:

```bash
amm expand mem_abc123
```

### Updating Memories

```bash
amm memory update mem_abc123 --status superseded
```

## Maintenance Jobs

Run background jobs to maintain memory quality:

```bash
# Extract memories from recent events
amm jobs run reflect

# Compress history into summaries
amm jobs run compress_history

# Detect contradictions
amm jobs run detect_contradictions

# Merge duplicates
amm jobs run merge_duplicates

# Rebuild search indexes
amm jobs run rebuild_indexes
```

## Ingestion Policies

Control how events are processed:

```bash
# List current policies
amm policy list

# Add a policy to ignore test sessions
amm policy add \
  --pattern-type session \
  --pattern "test_*" \
  --mode ignore

# Add read-only policy for sensitive sources
amm policy add \
  --pattern-type source \
  --pattern "production-logs" \
  --mode read_only
```

## MCP Integration

For agent runtimes that support MCP (Model Context Protocol):

### Start the MCP Server

```bash
amm-mcp
```

The server reads JSON-RPC requests from stdin and writes responses to stdout.

### Example MCP Tool Call

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "amm_recall",
    "arguments": {
      "query": "editor preferences",
      "opts": {"mode": "facts"}
    }
  }
}
```

See [MCP Reference](mcp-reference.md) for complete tool documentation.

## Advanced: Full JSON Envelopes

For scripting and CI/CD, use the `run` command with full envelopes:

```bash
amm run --in request.json
```

Example `request.json`:
```json
{
  "version": "amm.v1",
  "command": "remember",
  "request_id": "req-001",
  "payload": {
    "type": "preference",
    "scope": "global",
    "body": "User prefers dark mode",
    "tight_description": "Prefers dark mode"
  }
}
```

Validate without executing:

```bash
amm validate --in request.json
```

## Troubleshooting

### "database is locked" errors

This happens with concurrent access. Solutions:
- Use a single amm process at a time
- For high concurrency, consider WAL mode (enabled by default)

### FTS5 not available

If you see FTS5 errors, rebuild with the fts5 tag:
```bash
CGO_ENABLED=1 go build -tags fts5 ./cmd/amm
```

### Empty recall results

1. Check if memories exist: `amm status`
2. Try different recall modes: `--mode facts`, `--mode history`
3. Verify scope filters aren't too restrictive
4. Run `amm jobs run rebuild_indexes`

## Next Steps

- Read the [Architecture Overview](architecture.md) to understand how AMM works
- Review the [CLI Reference](cli-reference.md) for all commands
- Check [Integration Guide](integration.md) for connecting your agent runtime
- See runtime-specific guides: [Codex](codex-integration.md), [OpenCode](opencode-integration.md)

## Getting Help

- Run `amm --help` for command help
- Run `amm <command> --help` for specific command flags
- Check [GitHub Issues](https://github.com/bonztm/agent-memory-manager/issues)
