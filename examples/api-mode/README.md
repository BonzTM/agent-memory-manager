# AMM API-Mode Examples

This directory contains examples of how to interact with the Agent Memory Manager (AMM) via its HTTP API instead of the binary CLI.

## Integration patterns

API mode now supports two integration paths:

1. **REST hooks** — curl-based scripts that call endpoints like `/v1/recall`, `/v1/events`, and `/v1/memories`.
2. **MCP-over-HTTP** — MCP-capable runtimes such as Claude Code can connect directly with:
   ```json
   {
     "mcpServers": {
       "amm-http": {
         "url": "http://localhost:8080/v1/mcp"
       }
     }
   }
   ```

MCP-over-HTTP is the recommended approach for MCP-capable runtimes.

## When to use API-Mode

- **Remote Server**: Running AMM on a centralized server for multiple agents.
- **Shared Instance**: Multiple users or agents sharing a single memory substrate.
- **Containerized Environments**: When agents run in ephemeral containers but need persistent memory.
- **Multi-Agent Systems**: Coordinating multiple specialized agents through a single memory interface.
- **Non-Go Environments**: Interacting with AMM from languages where the binary is not easily available.

## When to use Binary-Mode

- **Local Development**: Single agent running on the same machine.
- **Low Latency**: Minimizing network overhead for local operations.
- **Simple Setup**: No need to manage a separate server process.

## Environment Variables

The examples here use the following environment variable:

- `AMM_API_URL`: The base URL of the AMM HTTP API. Defaults to `http://localhost:8080`.
- `AMM_API_KEY`: Optional server-side static key. When set, send `Authorization: Bearer <key>` with requests.

OpenAPI docs are available at `/openapi.json`, and Swagger UI is available at `/swagger/`.

## Quick Comparison

### Recall

**Binary Mode:**
```bash
amm recall "user preferences"
```

**API Mode (curl):**
```bash
curl -s -X POST "http://localhost:8080/v1/recall" \
  -H "Content-Type: application/json" \
  -d '{"query": "user preferences", "opts": {"mode": "ambient"}}'
```

### Remember

**Binary Mode:**
```bash
amm remember --type preference --body "User prefers Go" --tight "Go preference"
```

**API Mode (curl):**
```bash
curl -s -X POST "http://localhost:8080/v1/memories" \
  -H "Content-Type: application/json" \
  -d '{"type": "preference", "body": "User prefers Go", "tight_description": "Go preference"}'
```

## Contents

- `hooks/`: Generic shell scripts for common AMM operations via HTTP.
- `claude-code/`: Configuration and hooks for Claude Code.
- `codex/`: Python-based hooks for Codex.
- `opencode/`: Plugin configuration for OpenCode.

For a sidecar deployment example, see [`../../deploy/sidecar/`](../../deploy/sidecar/).
