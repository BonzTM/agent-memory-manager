# amm — Agent Memory Manager

AMM gives AI agents durable, structured memory that persists across sessions and projects. It ingests conversation history, extracts typed memories, and serves low-latency recall for context injection.

## Quick Start

1. **Initialize**
   ```bash
   AMM_DB_PATH=~/.amm/amm.db ./amm init
   ```
2. **Configure (Optional)**
   ```bash
   export AMM_SUMMARIZER_ENDPOINT=https://api.openai.com/v1
   export AMM_SUMMARIZER_API_KEY=sk-...
   ```
3. **Ingest an Event**
   ```bash
   echo '{"kind":"message_user","source_system":"cli","content":"User prefers Go"}' | ./amm ingest event --in -
   ```
4. **Recall**
   ```bash
   ./amm recall "user preferences"
   ```

For detailed setup, see [Getting Started](docs/getting-started.md).

## Installation

### 1. Release Binary (Recommended)
Download the latest pre-compiled binary for your platform from the [Releases](https://github.com/bonztm/agent-memory-manager/releases) page. Extract `amm`, `amm-mcp`, and `amm-http`, then add them to your system PATH.

### 2. Docker
Pull the official image from GitHub Container Registry:
```bash
docker pull ghcr.io/bonztm/agent-memory-manager:latest
```
Run with a persistent volume:
```bash
docker run -v ~/.amm:/data -e AMM_DB_PATH=/data/amm.db ghcr.io/bonztm/agent-memory-manager:latest amm init
```

### 3. Build from Source
If you prefer building locally, ensure you have Go 1.21+.
```bash
go build ./cmd/amm ./cmd/amm-mcp ./cmd/amm-http
```
See [Getting Started](docs/getting-started.md) for more build options.

## Deployment Modes

### CLI Mode
Run `amm` directly for interactive use or shell-based scripts.

### MCP Mode
Run `amm-mcp` to use AMM as a Model Context Protocol server. This is the primary way to integrate with tools like Claude Code and IDEs.

### HTTP API Mode
Run `amm-http` to start a persistent HTTP server. This is ideal for shared memory backends or integration with web-based agent runtimes.
```bash
./amm-http
# Server starts on :8080 by default
```
See the [HTTP API Reference](docs/http-api-reference.md) for details.

## What AMM Does

AMM is a database-backed memory substrate, not a chat runtime or task engine. It focuses on the transition from ephemeral interaction to durable knowledge.

- **Event Ingestion**: Captures every turn in an append-only, full-transcript archive.
- **LLM Extraction**: Auto-extracts facts, preferences, and decisions with heuristic fallback when no LLM is configured.
- **Entity Graph**: Builds a relationship-aware model of the workspace for precision scoring.
- **Multi-Signal Recall**: Supports associative retrieval with 9 modes to match agent intent.
- **Background Pipeline**: Runs reflection, compression, and maintenance to keep memory fresh.

## How Agents Use AMM

The memory loop fits into every agent turn:
1. **Recall**: Ask AMM for context at the start of a task or repo switch.
2. **Expand**: Fetch full details only for the relevant memories or summaries.
3. **Act**: Use the recalled context to inform the next tool call or response.
4. **Remember**: Commit high-confidence decisions and facts explicitly.

See the [Agent Onboarding Guide](docs/agent-onboarding.md) for integration patterns.

## Architecture at a Glance

AMM uses a five-layer model to manage information from raw history to durable truth:

| Layer | Name | Purpose |
|-------|------|---------|
| A | Working Memory | Ephemeral, runtime-only state for the current turn. |
| B | History Layer | Append-only raw events and transcripts. Authoritative truth. |
| C | Compression Layer | Summaries linked to source history spans. |
| D | Canonical Memory Layer | Typed durable records like facts, preferences, and decisions. |
| E | Derived Index Layer | FTS5 and embeddings for low-latency retrieval. |

### Module Layout
```
cmd/amm/         CLI entrypoint
cmd/amm-mcp/     MCP adapter (JSON-RPC over stdio)
cmd/amm-http/    HTTP API adapter (RESTful server)
internal/
  core/          Service + repository interfaces, domain types
  service/       Business logic, recall, scoring, workers
  adapters/
    cli/         CLI JSON envelope adapter
    mcp/         MCP adapter
    http/        HTTP API adapter
    sqlite/      SQLite backend (default)
    postgres/    PostgreSQL backend
  contracts/v1/  Typed payloads and validation
  buildinfo/     Version + commit injection via ldflags
  runtime/       Config, service factory, logger
deploy/
  helm/amm/      Helm chart for Kubernetes deployment
```

Full details in [Architecture Documentation](docs/architecture.md).

AMM keeps adapter parity across CLI (`amm`), MCP (`amm-mcp`), and HTTP (`amm-http`) and storage parity across SQLite and PostgreSQL backends.

## Configuration

Set these environment variables to enable LLM-backed extraction and semantic search:

- `AMM_SUMMARIZER_ENDPOINT`: OpenAI-compatible API base URL.
- `AMM_SUMMARIZER_API_KEY`: API key for the summarizer.
- `AMM_EMBEDDINGS_ENABLED`: Set to `true` for vector-based recall.
- `AMM_EMBEDDINGS_API_KEY`: API key for embedding generation.
- `AMM_STORAGE_BACKEND`: Set to `postgres` to use a PostgreSQL database.

Full reference in [Configuration Documentation](docs/configuration.md).

## Documentation

- [Architecture](docs/architecture.md)
- [HTTP API Reference](docs/http-api-reference.md)
- [CLI Reference](docs/cli-reference.md)
- [MCP Reference](docs/mcp-reference.md)
- [Configuration](docs/configuration.md)
- [PostgreSQL Backend](docs/postgres.md)
- [Getting Started](docs/getting-started.md)
- [Agent Onboarding](docs/agent-onboarding.md)
- [Integration Guide](docs/integration.md)

## License

MIT. See [LICENSE](LICENSE) for details.
