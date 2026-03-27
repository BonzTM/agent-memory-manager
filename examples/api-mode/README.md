# AMM API-Mode Examples

This directory contains examples of how to interact with the Agent Memory Manager (AMM) via its HTTP API instead of the binary CLI.

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
