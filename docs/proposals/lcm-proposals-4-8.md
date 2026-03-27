# LCM Alignment Proposals: 4–8

Proposals derived from comparison with the LCM paper (Ehrlich & Blackman, Feb 2026) and the
`lossless-claw` reference implementation for OpenClaw.

Proposals 1–3 (three-level escalation, summary depth/kind columns, session consolidation
convergence) are tracked separately and implemented in the same change batch.

---

## Proposal 4 — `FormatContextWindow`: Active Context Assembly

### Problem

LCM's core "deterministic retrievability" property does not come from the recall system. It
comes from the *engine assembler* injecting summary IDs into the prompt as structured XML
attributes on every turn:

```xml
<summary id="sum_abc123" kind="leaf" depth="0" tokens="847">
  ... summary text ...
  <parents><summary_ref id="sum_def456"/></parents>
</summary>
```

The model always sees the IDs of every compacted block — the engine guarantees this, not
the agent. AMM currently has all the data (events + summaries + summary_edges) but no
assembly layer. Agents must infer structure themselves from flat `recall` results.

### Proposal

Add a `FormatContextWindow(ctx, opts FormatContextWindowOptions) (*ContextWindowResult, error)`
method to `core.Service`.

**Behaviour:**
1. Load recent raw events (up to `FreshTailCount`, default 32) for the session.
2. Load all summary nodes that cover events older than the fresh tail, ordered chronologically.
3. Interleave: summaries first (oldest → newest), then raw fresh tail events.
4. Render each node with stable IDs and provenance metadata:
   - Summary nodes: `<summary id="..." kind="..." depth="..." token_count="...">...</summary>`
   - Raw events: `<event id="..." kind="..." occurred_at="...">...</event>`
5. Return the assembled string + a manifest (total tokens estimated, summary count, fresh event count).

**Options:**
```go
type FormatContextWindowOptions struct {
    SessionID     string
    ProjectID     string
    FreshTailCount int   // default 32
    MaxSummaryDepth int  // 0 = all depths
    IncludeParentRefs bool
}

type ContextWindowResult struct {
    Content       string
    SummaryCount  int
    FreshCount    int
    EstTokens     int
    Manifest      []ContextWindowItem
}

type ContextWindowItem struct {
    ID    string
    Kind  string  // "event" | "summary"
    Depth int
    Tokens int
}
```

**Adapter parity:** expose via:
- CLI: `amm context-window [--session <id>] [--project <id>] [--fresh-tail <n>]`
- MCP: `amm_format_context_window` tool
- HTTP: `GET /v1/context-window?session_id=...&project_id=...`

**Contract:** add `format_context_window` to `internal/contracts/v1/commands.go`.

### Reference
- lossless-claw `assembler.ts#L584-L622` — XML attribute injection
- lossless-claw `assembler.ts#L606-L615` — parent ref embedding
- lossless-claw `summary-store.ts#L664-L721` — `replaceContextRangeWithSummary`

### Dependencies
- Proposal 2 (depth + kind columns on summaries) should land first for clean depth queries.

---

## Proposal 5 — Large Payload / Large File Handling

### Problem

AMM ingests events with arbitrary content payloads. A single tool result containing a 50 K+
token file dump is stored verbatim, pollutes recall candidate scoring, and can never be
meaningfully summarised (it's too large for any leaf chunk). There is no token-threshold
interception at ingest time.

### Proposal

Add a `large_files` table and `LargeFileTokenThreshold` config key (default: `25000` tokens,
matching lossless-claw).

**Ingest-time interception in `IngestEvent`:**
1. Estimate token count of `event.Content` (simple `len(content)/4` heuristic is sufficient).
2. If count exceeds threshold:
   a. Write content to `~/.amm/large-files/<session_id>/<file_id>.<ext>` (or equivalent).
   b. Insert row into `large_files`: `(id, session_id, project_id, path, mime_type, token_count, exploration_summary, created_at)`.
   c. Replace `event.Content` with a compact reference stub:
      ```
      [large-file: <file_id> | path: <path> | tokens: ~<N> | type: <mime>]
      <exploration_summary>
      ```
3. Continue normal event ingestion with the stub payload.

**Type-aware exploration summary generation (deterministic first, LLM fallback):**

| File type | Strategy |
|-----------|----------|
| `.json`, `.csv`, `.tsv`, `.yaml`, `.xml` | Schema + shape extraction (keys, row count, column names) — no LLM |
| `.go`, `.ts`, `.py`, `.js`, etc. | Function/type signatures + import list — no LLM |
| `.sql` | Table/view/index names — no LLM |
| Unstructured text (`.txt`, `.md`, unknown) | LLM summary with three-level escalation fallback |

**File ID propagation through DAG:**
When events referencing a large file are compacted into a summary node, the `file_ids` field on
the summary must be populated (union of all `file_ids` referenced by covered events). This
ensures the file reference survives arbitrarily many rounds of compaction.

Add `file_ids_json TEXT NOT NULL DEFAULT '[]'` to the `summaries` table (both SQLite and
PostgreSQL migrations, append-only).

**New service methods:**
```go
GetLargeFile(ctx, id string) (*LargeFile, error)
ListLargeFiles(ctx, opts ListLargeFilesOptions) ([]LargeFile, error)
```

**Adapter parity:** expose `get_large_file` and `list_large_files` via CLI/MCP/HTTP.

**Config additions:**
```toml
[compression]
large_file_token_threshold = 25000
large_file_storage_dir = "~/.amm/large-files"
```

Corresponding env vars: `AMM_LARGE_FILE_TOKEN_THRESHOLD`, `AMM_LARGE_FILE_STORAGE_DIR`.

### Reference
- lossless-claw `large-files.ts` (~600 lines), especially `L275-L448`
- lossless-claw `migration.ts#L536-L545` — `large_files` table
- lossless-claw `engine.ts#L1301-L1560` — ingest-time interception
- lossless-claw `config.ts#L162-L167` — `largeFileTokenThreshold = 25000`

---

## Proposal 6 — `amm_grep`: Structured Search Grouped by Summary Context

### Problem

AMM has `SearchEvents` (FTS) and `SearchMemories` (FTS), but results are flat. There is no
indication of which summary node covers a given event, no ability to scope a search to a
particular summary subtree, and no grouped presentation that tells the agent *where in the
conversation* a match was found.

LCM's `lcm_grep` groups results by covering summary node so the model can understand the
temporal/structural context of each hit without loading the raw event.

### Proposal

Add a `Grep(ctx, pattern string, opts GrepOptions) (*GrepResult, error)` method to
`core.Service`.

```go
type GrepOptions struct {
    SessionID   string
    ProjectID   string
    SummaryID   string  // restrict search to this subtree
    Limit       int
    IncludeRaw  bool    // also search raw events outside any summary
}

type GrepResult struct {
    Groups []GrepGroup
    Total  int
}

type GrepGroup struct {
    CoveringSummaryID   string   // "" if uncovered raw event
    CoveringSummaryKind string   // "leaf" | "condensed" | "topic" | "session" | ""
    CoveringSummaryDesc string
    Matches             []GrepMatch
}

type GrepMatch struct {
    EventID   string
    Content   string   // matched content snippet
    Kind      string   // event kind
    OccurredAt time.Time
}
```

**Implementation sketch:**
1. Run FTS (or regex) search over `events` content.
2. For each matched event, look up its covering summary via `summary_edges` (walk up to
   the shallowest summary that covers it).
3. Group results by covering summary ID.
4. If `SummaryID` filter is set, restrict to events whose covering summary is within that
   subtree.
5. Paginate groups to prevent context flooding (default max 10 groups × 5 matches each).

**Adapter parity:**
- CLI: `amm grep "<pattern>" [--session <id>] [--summary <id>]`
- MCP: `amm_grep` tool
- HTTP: `GET /v1/grep?pattern=...&session_id=...`

**Contract:** add `grep` to `internal/contracts/v1/commands.go`.

### Reference
- lossless-claw `lcm-grep-tool.ts#L83-L214`
- lossless-claw `conversation-store.ts#L662-L891` — message search with regex/FTS fallback
- lossless-claw `summary-store.ts#L749-L943` — summary search with ReDoS guards

---

## Proposal 7 — Delegation Depth Control

### Problem

AMM has no concept of sub-agent spawning depth. An agent can call `amm_expand` from a
sub-agent that itself was spawned by another sub-agent, producing unbounded expansion chains.

LCM enforces a `scope-reduction invariant`: each level of nested delegation must strictly
reduce the delegated scope. lossless-claw implements this as `EXPANSION_DELEGATION_DEPTH_CAP = 1`
with child grant inheritance: `childMaxDepth = parentGrant.maxDepth - 1`.

### Proposal

Add optional delegation depth metadata to AMM session context.

**Session-level depth tracking:**
Add `delegation_depth INTEGER NOT NULL DEFAULT 0` to the session context passed with
`IngestEvent` (via `event.Metadata["delegation_depth"]` initially — no schema change required
for the MVP).

**`amm_expand` depth guard:**
In the `Expand` service method, if `delegation_depth >= max_expand_depth` (configurable,
default 1), return an explicit error code `EXPANSION_RECURSION_BLOCKED` with the message:
> "Expansion is not available at this delegation depth. Perform the work directly or surface
> findings to the parent agent."

**Config addition:**
```toml
[retrieval]
max_expand_depth = 1   # 0 = root only, 1 = one level of sub-agents, -1 = unlimited
```

Env var: `AMM_MAX_EXPAND_DEPTH`.

**Note:** Full enforcement of the scope-reduction invariant (requiring `delegated_scope` +
`kept_work` parameters) requires cooperation from the agent runtime (OpenCode, Claude Code,
etc.) passing depth metadata. AMM can only enforce the cap; the runtime must set depth.
Document this as a joint responsibility in `docs/integration.md`.

### Reference
- lossless-claw `lcm-expansion-recursion-guard.ts#L5-L7` — `EXPANSION_DELEGATION_DEPTH_CAP = 1`
- lossless-claw `lcm-expansion-recursion-guard.ts#L166-L214` — cap enforcement
- lossless-claw `engine.ts#L2596-L2618` — child grant inheritance

---

## Proposal 8 — `llm_map` / `agentic_map` Operator Primitives

### Assessment

`llm_map` and `agentic_map` are **not implemented in lossless-claw** either — they are
absent from the registered tool list. They appear to be Volt-only (closed-source) features.

This is **not a gap AMM has vs. existing open implementations.** No open reference
implementation exists to adapt from.

### If AMM were to implement it

The feature would require:
1. A `map_jobs` table tracking per-item status: `(id, kind, session_id, input_path, prompt, schema_json, concurrency, status, created_at, finished_at)`.
2. A `map_items` table: `(job_id, item_index, input_json, output_json, status, retries, error_text)`.
3. A `RunMap(ctx, opts MapOptions) (*MapJob, error)` service method launching workers.
4. Worker pool using Go goroutines with pessimistic locking (UPDATE WHERE status='pending' LIMIT 1).
5. Schema-validated output per item using a JSON Schema validator.
6. `agentic_map` variant would require sub-agent spawning — outside AMM's current scope as a
   memory substrate (AMM doesn't execute agents, it serves them).

**Recommendation:** Defer. `llm_map` makes more sense as a feature of the agent runtime
(OpenCode, Volt) than of the memory substrate. AMM's role is to store map job results once
they complete, not to orchestrate the execution. Flag for the runtime teams.

### Reference
- LCM paper Section 3.1 and Figure 4
- lossless-claw registered tools (lcm_grep, lcm_describe, lcm_expand, lcm_expand_query only — no map tools)

---

## Implementation Priority

| Proposal | Effort | Blocks | Recommended Order |
|----------|--------|--------|-------------------|
| 4 — FormatContextWindow | Medium | Proposal 2 (depth) | 2nd wave, after P1-P3 land |
| 5 — Large file handling | Medium-large | None | 2nd wave, standalone |
| 6 — amm_grep | Medium | Proposal 2 (depth) | 2nd wave, after P1-P3 land |
| 7 — Delegation depth | Small (MVP) | None | Can do anytime |
| 8 — llm_map / agentic_map | Large | N/A | Defer — runtime concern |
