# amm — Agent Memory Manager

Durable, structured memory for AI agents. AMM ingests conversation history, extracts typed memories from session narratives, and serves low-latency recall for context injection — across sessions, projects, and agent runtimes.

## What AMM Does

AMM is a database-backed memory substrate, not a chat runtime or task engine. It turns ephemeral conversations into durable knowledge.

- **Session-First Extraction** — Conversations are summarized as full session narratives, then memories are extracted with the complete arc as context. Produces rich, reasoned memories instead of thin fragments.
- **Event Ingestion** — Every turn is captured in an append-only, full-transcript archive.
- **Entity Graph** — Builds a relationship-aware model of the workspace for precision scoring.
- **Multi-Signal Recall** — 11 retrieval modes with learned ranking, temporal search, and contradiction surfacing.
- **Background Pipeline** — Automated reflection, compression, consolidation, and maintenance.

> **LLM requirement:** Automatic memory extraction requires an LLM endpoint (`AMM_SUMMARIZER_ENDPOINT`). Without one, event storage and explicit `amm remember` calls still work, but the extraction pipeline is disabled.

## How Agents Use AMM

```
Recall → Expand → Act → Remember
```

1. **Recall** — Query AMM for context at task start, repo switch, or resume.
2. **Expand** — Fetch full details for relevant memories or summaries.
3. **Act** — Use recalled context to inform the next tool call or response.
4. **Remember** — Commit high-confidence decisions and facts explicitly.

## Supported Runtimes

| Runtime | Integration | Memory Slot |
|---------|------------|-------------|
| [Claude Code](docs/agent-onboarding.md) | Hooks + MCP sidecar | — |
| [Codex](docs/codex-integration.md) | Hooks | — |
| [OpenCode](docs/opencode-integration.md) | Native plugin + MCP sidecar | Native tools |
| [OpenClaw](docs/openclaw-integration.md) | Native plugin + MCP sidecar | — |
| [Hermes](docs/hermes-agent-integration.md) | Memory provider or Python hook plugin | External provider |
| Any HTTP client | [HTTP API](docs/http-api-reference.md) | — |

## Quick Start

```bash
# 1. Initialize
AMM_DB_PATH=~/.amm/amm.db ./amm init

# 2. Configure LLM (optional but recommended)
export AMM_SUMMARIZER_ENDPOINT=https://api.openai.com/v1
export AMM_SUMMARIZER_API_KEY=sk-...

# 3. Add recommended ingestion policies
amm policy-add --pattern-type kind --pattern "tool_call" --mode ignore --match-mode exact --priority 100
amm policy-add --pattern-type kind --pattern "tool_result" --mode ignore --match-mode exact --priority 100

# 4. Ingest an event
echo '{"kind":"message_user","source_system":"cli","content":"User prefers Go"}' | ./amm ingest event --in -

# 5. Recall
./amm recall "user preferences"
```

For detailed setup, see [Getting Started](docs/getting-started.md). For agent integration, see [Agent Onboarding](docs/agent-onboarding.md).

## Installation

### Release Binary (Recommended)
Download from [Releases](https://github.com/bonztm/agent-memory-manager/releases). Extract `amm`, `amm-mcp`, and `amm-http`, then add to PATH.

### Docker
```bash
docker pull ghcr.io/bonztm/agent-memory-manager:latest

# Initialize
docker run --rm -v ~/.amm:/data -e AMM_DB_PATH=/data/amm.db \
  --entrypoint amm ghcr.io/bonztm/agent-memory-manager:latest init

# Run HTTP server
docker run --rm -p 8080:8080 -v ~/.amm:/data -e AMM_DB_PATH=/data/amm.db \
  ghcr.io/bonztm/agent-memory-manager:latest
```

### Build from Source
Requires Go 1.26.1+.
```bash
go build ./cmd/amm ./cmd/amm-mcp ./cmd/amm-http
```

### Helm
```bash
helm repo add amm https://bonztm.github.io/agent-memory-manager
helm install amm amm/amm
```

## Deployment Modes

| Mode | Binary | Use Case |
|------|--------|----------|
| CLI | `amm` | Interactive use, shell scripts, hooks |
| MCP | `amm-mcp` | Model Context Protocol server for Claude Code, IDEs |
| HTTP | `amm-http` | Shared memory backend, web agents, Kubernetes |

## Architecture

AMM uses a five-layer model from raw history to durable truth:

| Layer | Name | Purpose |
|-------|------|---------|
| A | Working Memory | Ephemeral, runtime-only state for the current turn |
| B | History Layer | Append-only raw events and transcripts |
| C | Compression Layer | Session narratives and topic summaries |
| D | Canonical Memory Layer | Typed durable records (facts, preferences, decisions) |
| E | Derived Index Layer | FTS5 and embeddings for low-latency retrieval |

```
cmd/amm/         CLI entrypoint
cmd/amm-mcp/     MCP adapter (JSON-RPC over stdio)
cmd/amm-http/    HTTP API adapter (RESTful server)
internal/
  core/          Service + repository interfaces, domain types
  service/       Business logic, recall, scoring, workers
  adapters/      CLI, MCP, HTTP, SQLite, PostgreSQL
  contracts/v1/  Typed payloads and validation
  runtime/       Config, service factory, logger
deploy/helm/     Helm chart for Kubernetes
```

Adapter parity: CLI, MCP, and HTTP expose the same service methods. Storage parity: SQLite and PostgreSQL implement the full Repository interface.

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `AMM_SUMMARIZER_ENDPOINT` | — | OpenAI-compatible API base URL (enables extraction) |
| `AMM_SUMMARIZER_API_KEY` | — | API key for the summarizer model |
| `AMM_REVIEW_ENDPOINT` | — | Separate endpoint for review/extraction model (falls back to summarizer) |
| `AMM_EMBEDDINGS_ENABLED` | `false` | Enable vector-based semantic recall |
| `AMM_EMBEDDINGS_API_KEY` | — | API key for embedding generation |
| `AMM_STORAGE_BACKEND` | `sqlite` | `sqlite` or `postgres` |
| `AMM_SESSION_IDLE_TIMEOUT_MINUTES` | `15` | Minutes of inactivity before session consolidation |
| `AMM_SUMMARIZER_CONTEXT_WINDOW` | `128000` | Token budget for summarizer (sessions exceeding this are chunked) |

Full reference: [Configuration Documentation](docs/configuration.md)

## Documentation

| Topic | Link |
|-------|------|
| Getting Started | [docs/getting-started.md](docs/getting-started.md) |
| Agent Onboarding | [docs/agent-onboarding.md](docs/agent-onboarding.md) |
| Integration Guide | [docs/integration.md](docs/integration.md) |
| Configuration | [docs/configuration.md](docs/configuration.md) |
| HTTP API Reference | [docs/http-api-reference.md](docs/http-api-reference.md) |
| CLI Reference | [docs/cli-reference.md](docs/cli-reference.md) |
| MCP Reference | [docs/mcp-reference.md](docs/mcp-reference.md) |
| Architecture | [docs/architecture.md](docs/architecture.md) |
| PostgreSQL Backend | [docs/postgres.md](docs/postgres.md) |
| Helm Chart | [deploy/helm/amm/README.md](deploy/helm/amm/README.md) |
| HTTP Sidecar | [deploy/sidecar/README.md](deploy/sidecar/README.md) |
| Changelog | [CHANGELOG.md](CHANGELOG.md) |

## License

MIT. See [LICENSE](LICENSE) for details.
