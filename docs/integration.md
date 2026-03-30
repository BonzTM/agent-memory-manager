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
- **Examples**: See [`examples/api-mode/`](../examples/api-mode/README.md), [HTTP Sidecar Example](../deploy/sidecar/README.md), and [Helm Quickstart](../deploy/helm/amm/README.md).

### 2. MCP (stdio)
For local runtimes like Claude Code or IDE-based agents.

- **Start Server**: `amm-mcp`
- **When to use**: Direct agent tool usage where the agent is "memory-aware".
- **Reference**: [MCP Reference](mcp-reference.md)

### 3. CLI Hooks (Transparent Capture)
For runtimes that support lifecycle hooks (e.g., shell scripts triggered on user input/output).

- **Binary**: `amm`
- **When to use**: Adding memory to existing CLI-based agents without modifying their source code.
- **Example**: [Agent Onboarding](agent-onboarding.md#step-4-set-up-automatic-capture-hooks)

---

## Runtime Guides

| Runtime | Integration Pattern | Guide |
|---------|---------------------|-------|
| Claude Code | MCP + CLI Hooks | [Agent Onboarding](agent-onboarding.md#step-3-configure-for-claude-code-full-reference-path) |
| Codex | MCP + CLI Hooks | [Codex Integration](codex-integration.md) |
| OpenCode | MCP + Local Plugin | [OpenCode Integration](opencode-integration.md) |
| OpenClaw | MCP Sidecar | [OpenClaw Integration](openclaw-integration.md) |
| Hermes | MCP Sidecar | [Hermes Integration](hermes-agent-integration.md) |

*Note: For all runtimes, the HTTP API mode is also an option for remote or containerized deployments.*

## Runtime Decision Guide

Ask these questions before picking a path:

1. Does the runtime support stdio MCP? If yes, prefer `amm-mcp`.
2. Does the runtime run outside the AMM host or inside containers? If yes, prefer `amm-http` or sidecar deployment.
3. Does the runtime expose reliable lifecycle hooks? If yes, add capture helpers alongside MCP.
4. Does the user want SQLite or PostgreSQL? SQLite is simplest; PostgreSQL is better for shared multi-agent use.
5. Who owns maintenance scheduling? AMM does not ship an internal scheduler.

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

## Recommended Ingestion Policies

Before setting up event capture, configure ingestion policies to filter out tool noise. This is **strongly recommended** for all integrations.

```bash
amm policy-add --pattern-type kind --pattern "tool_call" --mode ignore --match-mode exact --priority 100
amm policy-add --pattern-type kind --pattern "tool_result" --mode ignore --match-mode exact --priority 100
```

**Why**: Without these policies, the extraction pipeline treats raw tool invocation JSON (patch text, shell commands, API payloads) as meaningful content, producing low-quality memories that pollute recall. The meaningful information from tool interactions is already captured in `message_user` and `message_assistant` events. See [Configuration: Ingestion Policies](configuration.md#ingestion-policies) for the full reference.

If your integration captures `tool_call` and `tool_result` events via hooks or plugins, these policies ensure the events are dropped at ingestion before the extraction pipeline ever sees them.

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
