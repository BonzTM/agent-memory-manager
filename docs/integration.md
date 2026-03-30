# Integration Guide

AMM (Agent Memory Manager) integrates with agent runtimes through four primary mechanisms:

1. **Hooks**: Automatic capture of interactions (events in, ambient recall out).
2. **MCP Tools**: Explicit agent-initiated memory management for MCP-compatible runtimes.
3. **MCP-over-HTTP**: Streamable HTTP transport for remote or containerized MCP clients.
4. **HTTP API**: RESTful integration for networked or web-based agents.

## Integration Modes

### 1. HTTP API (REST / MCP-over-HTTP)
For runtimes that prefer network communication or are not local to the AMM binary.

- **Start Server**: `amm-http`
- **When to use**: Web-based agents, multi-agent shared memory, or remote backends.
- **REST Endpoints**: [HTTP API Reference](http-api-reference.md)
- **MCP Endpoint**: `/v1/mcp` (Streamable HTTP)
- **Examples**: See `examples/api-mode/` and `deploy/sidecar/`.

### 2. MCP (stdio)
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
3. **Context Window Assembly**: Use the `context-window` command/tool to get a pre-formatted block of recent activity and relevant memories.
4. **Ingest Interaction**: Capture the user message and assistant response as events.
5. **Grep for Reference**: When the agent needs to find a specific past detail, use the `grep` tool for a high-precision search across all memory types.
6. **Remember Durable Facts**: Agent explicitly calls "remember" for high-confidence knowledge.
7. **Background Maintenance**: Run periodic jobs (reflect, compress, detect_contradictions) to consolidate and clean up information.

---

## Minimum Capture Requirements

For high-quality recall, ensure your integration provides these fields:
- `source_system`: Name of your agent/runtime.
- `session_id`: To keep recall session-aware (prevents repeating the same hints).
- `project_id`: To scope memories to the current workspace.
- `content`: The actual text of the message or tool result.

---

## Expand Delegation Depth Control (P7)

AMM protects against recursive expansion loops with a depth guard.

- Runtime config: `AMM_MAX_EXPAND_DEPTH` (default `1`)
  - `-1` disables the guard (unlimited depth)
  - `0` blocks any delegated expand where `delegation_depth > 0`
- Expand input parameter: `delegation_depth` (integer, `>= 0`)
- Guard behavior: expand fails with `EXPANSION_RECURSION_BLOCKED` when
  `delegation_depth >= AMM_MAX_EXPAND_DEPTH` and depth is non-zero.

### Runtime responsibility

If your runtime delegates `expand` calls, it must pass `delegation_depth` and increment it on nested delegation.

- First direct expand call: `delegation_depth = 0`
- First delegated expand call: `delegation_depth = 1`
- Continue incrementing for each nested delegation hop

The current example integrations (`examples/opencode`, `examples/openclaw`, `examples/codex`, `examples/hermes-agent`) do not issue `expand` calls today. When adding expand usage to those runtimes, include `delegation_depth` from the start.

---

## Context Window Assembly (P7)

The `context-window` command/tool provides a unified assembly of the most important recent information for an agent. Runtimes should call this at the start of a new task or session to quickly populate the agent's prompt with relevant background.

- **CLI**: `amm context-window --project-id <id> --session-id <id> --fresh-tail-count <n> --max-summary-depth <n> [--include-parent-refs]`
- **MCP**: `amm_format_context_window`
- **HTTP**: `GET /v1/context-window`

## Grouped Search (Grep) (P7)

When standard vector or keyword recall isn't enough, the `grep` tool searches raw events for literal patterns and groups matches by covering summary. This is especially useful for finding specific error codes, unique identifiers, or literal code snippets from past interactions.

- **CLI**: `amm grep "pattern" --session-id <id> --project-id <id> --group-limit <n> --matches-per-group <n>`
- **MCP**: `amm_grep`
- **HTTP**: `GET /v1/grep`
