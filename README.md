# amm — Agent Memory Manager

AMM gives AI agents durable, structured memory that persists across sessions and projects. It ingests conversation history, extracts typed memories, and serves low-latency recall for context injection.

## Quick Start

1. **Build**
   ```bash
   CGO_ENABLED=1 go build -tags fts5 -o amm ./cmd/amm
   ```
2. **Initialize**
   ```bash
   AMM_DB_PATH=~/.amm/amm.db ./amm init
   ```
3. **Configure (Optional)**
   ```bash
   export AMM_SUMMARIZER_ENDPOINT=https://api.openai.com/v1
   export AMM_SUMMARIZER_API_KEY=sk-...
   ```
4. **Ingest an Event**
   ```bash
   echo '{"kind":"message_user","source_system":"cli","content":"User prefers Go"}' | ./amm ingest event --in -
   ```
5. **Recall**
   ```bash
   ./amm recall "user preferences"
   ```

For detailed setup, see [Getting Started](docs/getting-started.md).

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
internal/
  core/          Service + repository interfaces, domain types
  service/       Business logic, recall, scoring, workers
  adapters/      CLI, MCP, and SQLite implementations
  contracts/v1/  Typed payloads and validation
  runtime/       Config, service factory, logger
```

Full details in [Architecture Documentation](docs/architecture.md).

## Integrations

- **Claude Code**: Native MCP wiring and event hooks — see [examples/claude-code](examples/claude-code/).
- **Codex**: Integrated via [Codex Integration](docs/codex-integration.md).
- **OpenCode**: Dogfooding runtime with [OpenCode Integration](docs/opencode-integration.md).
- **OpenClaw**: Worker-based memory capture in [OpenClaw Integration](docs/openclaw-integration.md).
- **Hermes**: Sidecar model for [Hermes Integration](docs/hermes-agent-integration.md).

AMM pairs with [ACM](https://github.com/bonztm/agent-context-manager) for repositories requiring governed task workflows and durable state.

## Configuration

Set these environment variables to enable LLM-backed extraction and semantic search:

- `AMM_SUMMARIZER_ENDPOINT`: OpenAI-compatible API base URL.
- `AMM_SUMMARIZER_API_KEY`: API key for the summarizer.
- `AMM_EMBEDDINGS_ENABLED`: Set to `true` for vector-based recall.
- `AMM_EMBEDDINGS_API_KEY`: API key for embedding generation.

Full reference in [Configuration Documentation](docs/configuration.md).

## Reference Tables

<details>
<summary>Retrieval Modes</summary>

| Mode | Description |
|------|-------------|
| `ambient` | Default associative recall for every turn. |
| `facts` | Retrieve only durable factual memories. |
| `episodes` | Retrieve narrative episode records. |
| `timeline` | Chronological event and memory retrieval. |
| `project` | Scoped retrieval within a specific project. |
| `entity` | Focus on memories related to specific entities. |
| `active` | Surface context for open loops and in-flight items. |
| `history` | Direct search across raw event history. |
| `hybrid` | Combined multi-strategy retrieval. |
</details>

<details>
<summary>Maintenance Jobs</summary>

| Job | Description |
|-----|-------------|
| `reflect` | Extract candidate memories from recent events. |
| `rebuild_indexes` | Incremental FTS5 and embedding rebuild (runs after reflect). |
| `compress_history` | Build leaf summaries over event spans. |
| `consolidate_sessions` | Build session and episode summaries. |
| `build_topic_summaries` | Build topic-level hierarchical summaries. |
| `merge_duplicates` | Consolidate overlapping memories. |
| `extract_claims` | Extract structured assertions from memories. |
| `enrich_memories` | Entity-link explicitly remembered memories. |
| `rebuild_entity_graph` | Rebuild pre-computed entity graph neighborhoods. |
| `form_episodes` | Group related events into narrative episodes. |
| `detect_contradictions` | Find conflicting claims or stale truths. |
| `decay_stale_memory` | Downrank stale assumptions and open loops. |
| `promote_high_value` | Promote frequently accessed memories. |
| `lifecycle_review` | LLM-powered review for memory decay and promotion. |
| `cross_project_transfer` | Promote reusable memories to global scope. |
| `archive_session_traces` | Archive low-salience session memories. |
| `update_ranking_weights` | Update scoring weights from relevance feedback. |
| `rebuild_indexes_full` | Full FTS5 and embedding rebuild from scratch. |
| `cleanup_recall_history` | Prune old recall history rows. |
| `reprocess` | Re-extract from events, skipping LLM-processed. |
| `reprocess_all` | Re-extract from all events unconditionally. |
</details>

## Documentation

- [Architecture](docs/architecture.md)
- [CLI Reference](docs/cli-reference.md)
- [MCP Reference](docs/mcp-reference.md)
- [Integration Guide](docs/integration.md)
- [Configuration](docs/configuration.md)
- [Agent Onboarding](docs/agent-onboarding.md)

## Build & Test

- **Prerequisites**: Go 1.21+, CGO enabled.
- **Build**: `CGO_ENABLED=1 go build -tags fts5 ./cmd/amm`
- **Test**: `CGO_ENABLED=1 go test -tags fts5 ./...`

## License

MIT. See [LICENSE](LICENSE) for details.
