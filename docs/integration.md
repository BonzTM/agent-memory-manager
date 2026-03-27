# Integration Guide

AMM (Agent Memory Manager) integrates with agent runtimes through three primary mechanisms:

1. **Hooks**: Automatic capture of interactions (events in, ambient recall out).
2. **MCP Tools**: Explicit agent-initiated memory management for MCP-compatible runtimes.
3. **HTTP API**: RESTful integration for networked or web-based agents.

## Integration Modes

### 1. HTTP API (Networked/Cloud)
For runtimes that prefer network communication or are not local to the AMM binary, use the HTTP API.

- **Start Server**: `amm-http`
- **When to use**: Web-based agents, multi-agent shared memory, or remote backends.
- **Reference**: [HTTP API Reference](http-api-reference.md)
- **Examples**: See `examples/api-mode/`.

### 2. MCP (Model Context Protocol)
For local runtimes like Claude Code or IDE-based agents.

- **Start Server**: `amm-mcp`
- **When to use**: Direct agent tool usage where the agent is "memory-aware".
- **Reference**: [MCP Reference](mcp-reference.md)

### 3. CLI Hooks (Transparent Capture)
For runtimes that support lifecycle hooks (e.g., shell scripts triggered on user input/output).

- **Binary**: `amm`
- **When to use**: Adding memory to existing CLI-based agents without modifying their source code.
- **Example**: [Claude Code Hooks](docs/integration.md#hook-based-integration-claude-code-reference-implementation)

---

## Runtime Guides

| Runtime | Integration Pattern | Guide |
|---------|---------------------|-------|
| Claude Code | MCP + CLI Hooks | [Claude Integration](docs/integration.md#hook-based-integration-claude-code-reference-implementation) |
| Codex | MCP + CLI Hooks | [Codex Integration](codex-integration.md) |
| OpenCode | MCP + Local Plugin | [OpenCode Integration](opencode-integration.md) |
| OpenClaw | MCP Sidecar | [OpenClaw Integration](openclaw-integration.md) |
| Hermes | MCP Sidecar | [Hermes Integration](hermes-agent-integration.md) |

*Note: For all runtimes, the HTTP API mode is also an option for remote or containerized deployments.*

---

## The Integration Loop

Regardless of the mechanism, the ideal integration follows this loop:

1. **Recall on Entry**: At the start of a turn, ask AMM for context (ambient recall).
2. **Inject Context**: Add the recalled hints to the agent's system prompt.
3. **Ingest Interaction**: Capture the user message and assistant response as events.
4. **Remember Durable Facts**: Agent explicitly calls "remember" for high-confidence knowledge.
5. **Background Maintenance**: Run periodic jobs (reflect, compress) to consolidate new information.

---

## Minimum Capture Requirements

For high-quality recall, ensure your integration provides these fields:
- `source_system`: Name of your agent/runtime.
- `session_id`: To keep recall session-aware (prevents repeating the same hints).
- `project_id`: To scope memories to the current workspace.
- `content`: The actual text of the message or tool result.
