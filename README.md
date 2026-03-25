# amm — Agent Memory Manager

A persistent, typed, temporal memory substrate for agents.

## What is amm?

amm is a database-backed memory system that gives agents durable, structured, scoped memory that persists beyond context windows and across sessions and projects. It ingests raw interaction history, compresses it into expandable summaries, extracts typed durable memories (facts, preferences, decisions, episodes, and more), and serves low-latency ambient recall packets so agents always have a relevant halo of context.

amm is **not** a chat runtime, a task workflow engine, a vector DB wrapper dressed up as cognition, a markdown convention, or a pure transcript archive. It handles memory -- nothing more, nothing less.

### Key Capabilities

- Append-only raw event and transcript ingestion
- 16 typed durable memory records (preference, fact, decision, episode, identity, procedure, constraint, and more)
- Scoped memory: global, project, and session with orthogonal privacy levels
- Ambient recall: low-latency associative retrieval on every turn
- 9 retrieval modes for different query intents
- Expandable summaries linked back to source history spans
- Multi-signal scoring with 10-factor ranking
- Background reflection, compression, and maintenance workers
- Provenance tracking and recall explainability
- Integrity checking and self-repair
- CLI and MCP (JSON-RPC over stdio) interfaces backed by a single service layer
- SQLite-backed, single binary, local-first, no external services required

## Quick Start

```bash
# Prerequisites
# Go 1.21+, CGO enabled (for SQLite)

# Build
CGO_ENABLED=1 go build -tags fts5 -o amm ./cmd/amm

# Initialize
AMM_DB_PATH=~/.amm/amm.db ./amm init

# Store a memory
./amm remember --type preference --scope global \
  --subject "Josh" \
  --body "Josh prefers concise replies by default" \
  --tight "Prefers concise replies"

# Recall
./amm recall --mode ambient "communication preferences"

# Ingest a raw event
echo '{"kind":"message_user","source_system":"claude-code","content":"Hello world","occurred_at":"2026-03-23T12:00:00Z"}' | ./amm ingest event --in -

# Run reflection to auto-extract memories from events
./amm jobs run reflect

# Check status
./amm status
```

## How agents should use amm

The durable-memory loop is intentionally small:

1. Connect `amm-mcp` or the `amm` CLI to the runtime.
2. At task start, repo switch, or resume after interruption, ask AMM for thin recall (`ambient` is the default hot path).
3. Expand only the memories, summaries, or episodes you actually need before acting.
4. Commit only stable, high-confidence knowledge explicitly with `amm remember` / `amm_remember`; let hooks, plugins, and background workers capture the rest from history.
5. Keep the runtime boundary honest. Use only the hook, plugin, or MCP surfaces the runtime actually documents, and keep maintenance jobs external.

amm pairs well with [ACM](https://github.com/bonztm/agent-context-manager) for repos that also use governed task workflows — ACM handles task framing, verification, and closeout while amm handles durable memory.

### Start here

- Want the fastest end-to-end setup for a user? Start with [Agent Onboarding](docs/agent-onboarding.md).
- Want the shared runtime model first? Read [Integration Guide](docs/integration.md).
- Wiring a specific runtime? Jump straight to [Codex Integration](docs/codex-integration.md), [OpenCode Integration](docs/opencode-integration.md), [OpenClaw Integration](docs/openclaw-integration.md), or [Hermes-Agent Integration](docs/hermes-agent-integration.md).

## Architecture

amm is a Go binary with a clean layered architecture. All business logic flows through a single `Service` interface -- CLI, MCP, and HTTP adapters are thin wrappers that call into it.

```
cmd/amm/         CLI entrypoint
cmd/amm-mcp/     MCP adapter (JSON-RPC over stdio)
internal/
  core/          Service + repository interfaces, domain types
  service/       Business logic, recall, scoring, workers
  adapters/
    cli/         JSON envelope CLI runner
    mcp/         MCP tool server
    sqlite/      SQLite repository + migrations
  contracts/v1/  Typed payloads, validation
  runtime/       Config, service factory, logger
```

See [docs/architecture.md](docs/architecture.md) for the full architecture reference.

## Memory Model

amm organizes information into five layers, from ephemeral to durable:

| Layer | Name | Purpose |
|-------|------|---------|
| A | Working Memory | Ephemeral, runtime-only current turn state |
| B | History Layer | Append-only raw events and transcripts -- complete archive |
| C | Compression Layer | Summaries over raw history, linked back to source spans |
| D | Canonical Memory Layer | Typed durable records -- the authoritative memory substrate |
| E | Derived Index Layer | FTS5, optional embeddings, retrieval cache -- disposable and rebuildable |

The core invariant: canonical records (Layer D) and source history (Layer B) are authoritative truth. Derived indexes (Layer E) can always be rebuilt from them.

See [docs/architecture.md](docs/architecture.md) for detailed layer descriptions.

## Retrieval Modes

| Mode | Description |
|------|-------------|
| `ambient` | Low-latency associative recall for injection on every turn |
| `facts` | Retrieve durable factual memories matching a query |
| `episodes` | Retrieve narrative episode records |
| `timeline` | Chronologically ordered retrieval |
| `project` | Scoped retrieval within a specific project |
| `entity` | Retrieve memories related to specific entities |
| `active` | Surface active context, open loops, and in-flight items |
| `history` | Search raw event history directly |
| `hybrid` | Multi-strategy combined retrieval |

## Maintenance Jobs

| Job | Description |
|-----|-------------|
| `reflect` | Extract candidate durable memories from recent events |
| `compress_history` | Build leaf summaries over raw event spans |
| `consolidate_sessions` | Build session/topic summaries and episode summaries |
| `merge_duplicates` | Find and merge duplicate facts, preferences, and decisions |
| `detect_contradictions` | Find conflicting claims or stale truths |
| `decay_stale_memory` | Downrank stale assumptions and open loops |
| `rebuild_indexes` | Rebuild FTS5, embeddings, and retrieval cache |
| `repair_links` | Validate and repair summary/source/memory links |
| `cleanup_recall_history` | Delete recall history rows older than TTL (default: 7 days) |
| `reprocess` | Batch re-extract memories from events using LLM; skips events already processed by LLM |
| `reprocess_all` | Batch re-extract all memories unconditionally, superseding both heuristic and LLM results |

## Build Requirements

- **Go 1.21+**
- **CGO_ENABLED=1** (required for SQLite via go-sqlite3)
- **`-tags fts5`** required for full-text search (enforced at compile time)

```bash
# Build
CGO_ENABLED=1 go build -tags fts5 -o amm ./cmd/amm

# Run tests
CGO_ENABLED=1 go test -tags fts5 ./...
```

## Optional: LLM-Backed Extraction

By default, amm uses a heuristic phrase-cue system for memory extraction. For higher-quality extraction, set three environment variables to enable LLM-backed reflection and summarization:

```bash
export AMM_LLM_ENDPOINT=https://api.openai.com/v1   # or http://localhost:11434/v1 for Ollama
export AMM_LLM_API_KEY=sk-...
export AMM_LLM_MODEL=gpt-4o-mini                     # optional, defaults to gpt-4o-mini
```

Any OpenAI-compatible endpoint works (OpenAI, Anthropic, Ollama, vLLM, LM Studio). When unset, amm operates entirely locally with no external API calls. See [Configuration](docs/configuration.md) for details.

## Documentation

- [Architecture](docs/architecture.md) -- Memory layers, retrieval engine, schema, and key invariants
- [CLI Reference](docs/cli-reference.md) -- All CLI commands and flags
- [MCP Reference](docs/mcp-reference.md) -- MCP tool definitions for JSON-RPC integration
- [Integration Guide](docs/integration.md) -- Shared integration model, hooks, MCP, workers, and runtime guide index
- [Codex Integration](docs/codex-integration.md) -- MCP + hooks + AGENTS snippet for Codex workflows
- [Hermes-Agent Integration](docs/hermes-agent-integration.md) -- Sidecar model, MCP wiring, and amm-side helper scripts for Hermes
- [OpenClaw Integration](docs/openclaw-integration.md) -- Real OpenClaw example, native hooks, and amm worker strategy
- [OpenCode Integration](docs/opencode-integration.md) -- MCP + local plugin glue for OpenCode dogfooding and user setup
- [Configuration](docs/configuration.md) -- Config file format and environment variables
- [Agent Onboarding](docs/agent-onboarding.md) -- Guide for agents using amm as their memory substrate

## License

MIT. See [LICENSE](LICENSE) for details.
