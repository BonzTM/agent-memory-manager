# Getting Started with AMM

AMM (Agent Memory Manager) provides durable, structured memory for AI agents. This guide walks you through installation, configuration, and basic usage.

## Installation

### 1. Release Binary (Recommended)
The fastest way to get started is by downloading a pre-compiled binary:
1. Go to the [Releases](https://github.com/bonztm/agent-memory-manager/releases) page.
2. Download the archive for your operating system and architecture.
3. Extract the `amm`, `amm-mcp`, and `amm-http` binaries.
4. Move them to a directory in your system PATH (e.g., `/usr/local/bin` or `~/.local/bin`).

### 2. Docker
AMM is available as a Docker image on GitHub Container Registry:
```bash
docker pull ghcr.io/bonztm/agent-memory-manager:latest
```

To run AMM with a persistent SQLite database:
```bash
docker run -it \
  -v ~/.amm:/data \
  -e AMM_DB_PATH=/data/amm.db \
  ghcr.io/bonztm/agent-memory-manager:latest \
  amm init
```

For production deployments, see the [PostgreSQL Backend](postgres.md) guide and the Helm charts in `deploy/helm/amm`.

### 3. Build from Source
If you prefer to build locally, you need:
- Go 1.21 or later

**Build Commands:**
```bash
# Clone the repository
git clone https://github.com/bonztm/agent-memory-manager.git
cd agent-memory-manager

# Build all three binaries
go build ./cmd/amm ./cmd/amm-mcp ./cmd/amm-http
```

---

## Initial Setup

### 1. Initialize the Database
Before using AMM, initialize one of the supported backends:

**SQLite (default):**
```bash
amm init
```
By default, this creates a SQLite database at `~/.amm/amm.db`. You can override this with the `AMM_DB_PATH` environment variable.

**PostgreSQL:**
```bash
export AMM_STORAGE_BACKEND=postgres
export AMM_POSTGRES_DSN='postgres://postgres:postgres@localhost:5432/amm?sslmode=disable'
amm init
```

When `AMM_STORAGE_BACKEND=postgres` is set, AMM uses PostgreSQL instead of SQLite.

### 2. Verify Installation
Check the system status:
```bash
amm status
```
You should see `initialized: true` in the output.

Verify all three binaries are installed:
```bash
amm --help
amm-mcp --help
amm-http --help
```

---

## Basic Usage

### Storing a Memory
Add a preference or fact explicitly:
```bash
amm remember \
  --type preference \
  --scope global \
  --body "I prefer using Go for systems programming" \
  --tight "Prefers Go for systems"
```

### Recalling Context
Retrieve relevant memories based on a query:
```bash
amm recall "programming language preferences"
```

### Context Window Assembly
Assemble a pre-formatted context window for an agent:
```bash
amm context-window --project-id amm --fresh-tail-count 32 --max-summary-depth 1 --include-parent-refs
```

### High-Precision Search
Search raw events and group matches by covering summary:
```bash
amm grep "Neovim"
```

### Starting the HTTP Server
If you want to use the REST API or MCP-over-HTTP, start the HTTP adapter:
```bash
amm-http
# Server starts on :8080 by default
```

#### Securing the Server
To secure the HTTP server, set the `AMM_API_KEY` environment variable. When set, all requests (except health, status, and OpenAPI docs) must include this key in the header.

```bash
# Generate a secure key
export AMM_API_KEY=$(openssl rand -base64 32)
# Start the server
amm-http

# In another terminal, test with authentication:
curl -H "Authorization: Bearer $AMM_API_KEY" http://localhost:8080/v1/policies
```

Test status with curl:
```bash
curl http://localhost:8080/v1/status
```

Test recall with curl:
```bash
curl -s -X POST "http://localhost:8080/v1/recall" \
  -H "Content-Type: application/json" \
  -d '{"query":"user preferences","opts":{"mode":"ambient"}}'
```

---

## Next Steps
- [HTTP API Reference](http-api-reference.md)
- [CLI Reference](cli-reference.md)
- [MCP Reference](mcp-reference.md)
- [Configuration Guide](configuration.md)
- [Architecture Overview](architecture.md)
