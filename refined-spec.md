Correct. That was bleed-over from the design conversation.

If **AMM is its own project**, the spec should not lean on ACM at all.
At most, other systems can be mentioned generically as possible integrators, not as part of AMM’s identity.

Here’s the revised version, cleaned accordingly.

---

# AMM Specification Draft
## Agent Memory Manager
### A persistent, typed, temporal memory substrate for agents

## Status
Draft v0.4

## Revision note
This revision removes ACM-specific framing so AMM stands fully on its own as an independent project.

Key changes in this revision:
- removed ACM references from product framing
- removed ACM-specific integration/boundary language
- generalized external integrations to “workflow systems,” “agent runtimes,” and “control planes”
- kept AMM focused on its own architecture and responsibilities

---

# 1. Executive summary

## Problem
Most agent memory systems are still primitive.

Common failure modes:
- giant markdown files that become sludge
- vendor/session-local memory that dies with the runtime
- vector-only retrieval that is fuzzy and weak on chronology and truth change
- transcript retention being mistaken for actual memory
- explicit “look things up” UX rather than natural associative recall
- poor repairability and weak provenance

## Thesis
A serious memory system for agents should:
- retain complete raw interaction history
- separate raw history from durable memory
- support expandable summaries over history
- retrieve far less than it stores
- understand scope, time, provenance, and truth change
- support both explicit recall and ambient recall
- remain framework-agnostic
- be inspectable and repairable

## Product
**AMM (Agent Memory Manager)** is a database-backed memory substrate for agents.

It is:
- persistent
- typed
- scoped
- temporal
- provenance-aware
- queryable
- explainable
- ambient-recall capable
- history-preserving

It is not:
- a task workflow engine
- a chat runtime
- a project-governance system
- a markdown convention
- a pure transcript archive
- a vector DB wrapper dressed up as cognition

---

# 2. Design goals

## Primary goals
1. Persistent memory beyond context windows
2. Cross-session continuity
3. Cross-project continuity
4. Scoped memory when desired
5. Raw history preservation
6. Typed durable memories
7. Ambient low-latency associative recall
8. Selective deep recall on demand
9. Low prompt/token overhead
10. Provenance and inspectability
11. Temporal awareness
12. Memory lifecycle management
13. Expandable summaries linked to source history
14. Framework-agnostic integration
15. Repairability and integrity validation

## Secondary goals
1. Support CLI, API, and MCP-style integrations
2. Support local-first deployments
3. Support personal + project memory in one substrate
4. Support entity and relationship modeling
5. Support rebuildable search/index layers
6. Support graph-assisted retrieval later
7. Support read-only/ignored/stateless ingestion modes

## Non-goals
1. Replacing code search or repo exploration
2. Replacing workflow engines
3. Replacing chat/control-plane runtimes
4. Dumping memory wholesale into prompts
5. Building a research cathedral before a useful v0 exists

---

# 3. Core design principles

## 3.1 Store broadly, retrieve narrowly
AMM should store far more than it injects.

## 3.2 Log synchronously, think asynchronously
Writes should be cheap.
Interpretation should mostly happen in workers.

## 3.3 Typed memory beats giant notes
Memory should be durable records and claims, not prose sludge.

## 3.4 History is not memory
A raw transcript is not the same thing as a durable fact, preference, episode, or decision.

## 3.5 Scope is first-class
Memory must know where it belongs.

## 3.6 Time matters
Truth changes. Relevance changes. Context ages.

## 3.7 Ambient recall should feel natural
The agent should have a small halo of relevant memory on every turn.

## 3.8 Canonical memory and derived indexes are not the same thing
Canonical records are truth.
Indexes are disposable.

## 3.9 Summaries must remain expandable
Compressed context should link back to raw source spans.

## 3.10 Provenance matters
A memory without traceability is hard to trust.

## 3.11 Inspectability is mandatory
Users/builders should be able to inspect:
- what exists
- why it surfaced
- what it came from
- what superseded it

## 3.12 Repairability matters
Persistent memory without repair tools eventually rots.

## 3.13 Write-heavy, read-fast is the right trade
Do more work off the hot path so runtime recall stays cheap.

---

# 4. System boundaries

## 4.1 What AMM owns
- raw history/event ingestion
- transcript preservation
- summary compression over history
- typed memory storage
- claim extraction
- episode formation
- scoped recall
- ambient recall
- deep recall
- summary expansion
- entity linking
- temporal memory handling
- supersession/conflict tracking
- index management
- ingestion policies
- maintenance/repair jobs
- recall explainability

## 4.2 What AMM does not own
- task/plan workflows
- verification/review gates
- chat transport
- reminders/cron
- code editing
- repo navigation
- giant static prompt contracts

---

# 5. Relationship to adjacent systems

## 5.1 Agent runtimes and control planes
AMM is designed to integrate with conversational/control-plane systems that:
- receive user input
- run tools
- manage sessions
- render final responses

AMM should act as a memory substrate for those systems:
- ingest interaction history
- return ambient recall packets
- serve deep recall and expansion when needed

### Boundary
- the runtime/control plane handles interaction
- AMM handles memory

## 5.2 Workflow and task systems
AMM may integrate with external workflow systems that emit:
- task updates
- decisions
- incidents
- reviews
- outcomes

AMM may store those as history or canonical memories, but does not become the workflow engine itself.

## 5.3 Transcript/context engines
Transcript-preserving context systems solve an adjacent problem:
- preserving long conversation history
- compressing it without discarding source detail

AMM should learn from them, but remain broader:
- transcript retention is only one layer
- expandable summaries are useful
- durable typed memory is still required

---

# 6. Memory architecture layers

## 6.1 Layer A: Working memory
- ephemeral
- runtime-only
- current turn state
- not canonical by default

## 6.2 Layer B: History layer
- append-only raw events and transcripts
- source of interaction truth
- complete archive
- not prompt material by default

## 6.3 Layer C: Compression layer
- summaries over raw history
- potentially hierarchical
- always linked back to source spans
- supports describe/expand flows

## 6.4 Layer D: Canonical memory layer
- typed durable memory records
- preferences, facts, decisions, episodes, contradictions, open loops, etc.
- authoritative memory substrate

## 6.5 Layer E: Derived retrieval/index layer
- FTS/BM25
- embeddings
- graph projections
- rerank metadata
- disposable/rebuildable

### Principle
**Indexes are disposable; canonical records and source history are authoritative.**

---

# 7. Scope model

## 7.1 Rationale
AMM must support:
- broad continuity across everything
- project-scoped memory
- private memory
- shared memory
- session-local continuity

## 7.2 Two orthogonal axes

Scope and visibility are **separate concerns** and should not be conflated.

- **Scope** answers: *where does this memory belong?* (structural)
- **Privacy level** answers: *who is allowed to see it?* (access control)

A memory can be `global` scope + `private` privacy (your personal preferences across all projects), or `project` scope + `shared` privacy (a team decision about a specific repo).

## 7.3 Core fields
Each event/memory has:
- `scope` — one of: `global`, `project`, `session`
- `project_id` nullable — required when scope is `project`
- `session_id` nullable — required when scope is `session`
- `agent_id` nullable — optional, for agent-specific memory
- `privacy_level` — one of: `private`, `shared`, `public_safe`

## 7.4 Scope types

### `global`
Memory that applies across all projects and sessions. Tied to the user/agent identity, not to any particular workstream.

**Examples:**
- "Josh prefers concise replies by default"
- "Use 4-space indentation unless the project has an existing convention"
- "Josh is a senior engineer with deep Go expertise"
- "Always run tests before committing"

**When to use:** The memory would be useful regardless of which project is active.

### `project`
Memory tied to a specific project, repo, or workstream. Requires `project_id`.

**Examples:**
- "The auth service uses JWT with RS256, not HS256"
- "This repo's CI runs on GitHub Actions, not CircleCI"
- "We decided to use Postgres for this project, not SQLite"
- "The API freeze for v2.0 starts 2026-04-01"

**When to use:** The memory is meaningless or misleading outside the context of this specific project.

### `session`
Short-lived memory that exists for continuity within a single interaction session. Not promoted to durable memory by default.

**Examples:**
- "User is currently debugging the flaky test in auth_test.go"
- "We've been iterating on the schema for the events table"
- "User asked to skip linting for this session"

**When to use:** The memory is useful for maintaining coherence during a conversation but has no value after the session ends. If it turns out to matter longer-term, reflection workers can promote it to `project` or `global`.

## 7.5 Privacy levels (orthogonal to scope)

Privacy is **not** a scope — it controls who can see the memory, regardless of where it belongs.

### `private`
Only visible to the owning user/agent. Default.

### `shared`
Safe to surface in team/multi-agent contexts.

### `public_safe`
Safe for broad visibility (no secrets, credentials, or personal information).

## 7.6 Agent memory

`agent_id` is an **optional field**, not a scope. Any scope can carry an `agent_id` to indicate agent-specific operational memory.

**Example:** A memory scoped `global` with `agent_id: "aldous"` means "this is Aldous's personal operational knowledge, applicable everywhere."

## 7.7 Workspace (deferred)

The original spec included `workspace` as a scope. This is deferred from v0.

**Rationale:** In a single-user local-first deployment, `workspace` has no clear semantics distinct from `global`. It becomes meaningful in multi-user or multi-team deployments (e.g., "the platform team's shared environment"), which is a post-v0 concern. If needed later, it slots in between `global` and `project` with a `workspace_id` field.

---

# 8. Ingestion policy controls

## 8.1 Rationale
Not every session or source should write equally into memory.

## 8.2 Ingestion modes

### `full`
- may read
- may write history
- may write canonical memory

### `read_only`
- may read existing memory/history
- may not write canonical memory
- may optionally skip history persistence

### `ignore`
- excluded entirely from AMM ingestion and recall influence

## 8.3 Policy matching
Policies may match on:
- session id pattern
- source system
- surface
- runtime
- agent id
- project id
- channel type

---

# 9. Processing pipeline

## 9.1 Retain
Capture raw event/history cheaply and faithfully.

## 9.2 Reflect
Interpret retained material into candidate durable structures:
- facts
- preferences
- decisions
- episodes
- open loops
- contradictions
- claims
- entities
- active context

## 9.3 Compress
Build linked summaries over raw history to support efficient recall without dropping source fidelity.

## 9.4 Index
Update derived retrieval layers.

### Principle
**Retain -> Reflect -> Compress -> Index**

---

# 10. History layer

## 10.1 Purpose
Preserve all raw interaction material.

## 10.2 What belongs here
- user/assistant/system messages
- tool calls/results
- file events
- commits
- session lifecycle events
- workflow/task events from integrated systems
- imported transcripts

## 10.3 Important principle
History is:
- immutable or append-only by default
- complete
- traceable
- not the same as memory

---

# 11. Compression layer

## 11.1 Purpose
Support compact, lossless-ish navigation over large histories.

## 11.2 Compression objects
AMM may store:
- leaf summaries over raw event spans
- higher-level summaries over lower-level summaries
- session summaries
- topic summaries
- episode summaries

## 11.3 Requirements
Every summary/compression node must:
- have a unique id
- have a compact description
- link to `source_span`
- be expandable back toward raw source events
- never become an orphan

## 11.4 Design note
AMM does **not** need to make DAG summarization its core identity, but it should support:
- linked summary hierarchies
- drill-down
- source fidelity

---

# 12. Canonical memory model

## 12.1 Purpose
Canonical memory stores durable, typed meaning extracted from history or explicitly committed.

## 12.2 Memory types
Recommended initial types:
- `identity`
- `preference`
- `fact`
- `decision`
- `episode`
- `todo`
- `relationship`
- `procedure`
- `constraint`
- `incident`
- `artifact`
- `summary`
- `active_context`
- `open_loop`
- `assumption`
- `contradiction`

## 12.3 Common fields
- `id`
- `type`
- `scope`
- `project_id` nullable
- `session_id` nullable
- `workspace_id` nullable (deferred to post-v0)
- `agent_id` nullable
- `subject`
- `body`
- `tight_description`
- `confidence`
- `importance`
- `freshness` (computed at query time from temporal fields, not stored)
- `privacy_level`
- `status`
- `created_at`
- `observed_at`
- `updated_at`
- `valid_from`
- `valid_to` nullable
- `last_confirmed_at` nullable
- `supersedes` nullable
- `superseded_by` nullable
- `superseded_at` nullable
- `source_event_ids`
- `source_summary_ids`
- `source_artifact_ids`
- `tags`
- `entity_links`
- `metadata`

## 12.4 Tight description
Mandatory one-line summary for ambient recall and thin retrieval.

---

# 13. Temporal model

## 13.1 Why time matters
Without temporal truth, memory gets stupid.

## 13.2 Core temporal fields
- `observed_at`
- `valid_from`
- `valid_to`
- `last_confirmed_at`
- `superseded_at`

## 13.3 Required behaviors
AMM should support:
- current truth
- historical truth
- stale but historically valid truth
- superseded memories

---

# 14. Claims model

## 14.1 Purpose
Represent structured assertions in addition to prose.

## 14.2 Fields
- `id`
- `memory_id`
- `subject_entity_id`
- `predicate`
- `object_value`
- `object_entity_id`
- `confidence`
- `source_event_id`
- `source_summary_id`
- `observed_at`
- `valid_from`
- `valid_to`

## 14.3 Benefit
Claims support:
- exact retrieval
- contradiction detection
- cleaner supersession
- explainability

---

# 15. Entity model

## 15.1 Purpose
Make memory relational.

## 15.2 Entity types
- `person`
- `project`
- `repo`
- `host`
- `service`
- `topic`
- `artifact`
- `organization`

## 15.3 Role in retrieval
Entity overlap should be a major signal in:
- ambient recall
- project recall
- episode reconstruction

---

# 16. Episode model

## 16.1 Purpose
Narrative continuity over history.

## 16.2 Episode contents
- what happened
- who was involved
- what changed
- key decisions
- unresolved items
- source span
- related summaries
- related entities

## 16.3 Requirements
Episodes should be:
- durable
- linked to source history
- expandable to supporting context

---

# 17. Open loops, contradictions, assumptions

## 17.1 Open loops
Unresolved items worth resurfacing.

## 17.2 Contradictions
Conflicting memory requiring resolution or contextual ranking.

## 17.3 Assumptions
Tentative beliefs that may decay or require confirmation.

## 17.4 Why they matter
A useful memory system tracks not only what it knows, but what remains unsettled.

---

# 18. Retrieval model

## 18.1 Principle
AMM should not have one fuzzy “search.”

## 18.2 Retrieval modes
### `ambient`
Small memory halo for each inbound message.

### `facts`
Durable exact-ish memory.

### `episodes`
Narrative recall.

### `timeline`
Chronological reconstruction.

### `project`
Project-scoped recall.

### `entity`
Entity-linked recall.

### `active`
Open loops, active context, unresolved items.

### `history`
Raw-history-oriented retrieval over transcripts/events/summaries.

### `hybrid`
Weighted fusion of all relevant retrieval strategies.

---

# 19. Retrieval interaction pattern

## 19.1 Find
Locate relevant candidates:
- history nodes
- summaries
- canonical memories
- episodes

## 19.2 Describe
Return thin summaries:
- ids
- types
- tight descriptions
- scores
- scopes

## 19.3 Expand
Fetch richer details for a chosen item:
- full memory
- linked claims
- source summaries
- source events
- related episode
- related entities

### Principle
**Find -> Describe -> Expand** is the standard AMM retrieval flow.

---

# 20. Ambient recall / memory halo

## 20.1 Purpose
Low-latency associative priming on every inbound message.

## 20.2 Flow
1. ingest message event
2. extract cheap entity/topic cues
3. run ambient recall
4. return 3–7 thin hints
5. inject hints into agent turn
6. allow expansion if needed

## 20.3 Packet shape
Each item:
- `id`
- `kind` (`memory|episode|summary|history-node`)
- `type`
- `scope`
- `score`
- `tight_description`
- `confidence` optional
- `observed_at` optional

---

# 21. Retrieval ranking

## 21.1 Hybrid signals
AMM should blend:
- lexical/FTS
- semantic similarity
- entity overlap
- current topic continuity
- scope fit
- recency
- importance
- temporal validity
- active-thread relevance
- repetition suppression
- structural/source proximity

## 21.2 Anti-hub logic
Common/high-degree entities or generic memories should not dominate.

---

# 22. Explainability

## 22.1 Need
Recall without explanation becomes spooky nonsense.

## 22.2 Explain-recall capability
AMM should be able to explain:
- why an item surfaced
- what signals contributed
- whether it came from canonical memory, summary layer, or raw history
- whether it was boosted/suppressed by recency, repetition, scope, etc.

---

# 23. Canonical vs derived storage

## 23.1 Canonical storage
Authoritative:
- events/history
- summaries
- memories
- claims
- episodes
- entities
- relationships

## 23.2 Derived storage
Rebuildable:
- FTS indexes
- embeddings
- graph projections
- rerank caches
- salience caches

## 23.3 Principle
If derived storage is destroyed, AMM should be able to rebuild from canonical stores.

---

# 24. Ingestion architecture

## 24.1 Intake channels
- CLI
- HTTP API
- hooks/webhooks
- log tailers
- transcript imports
- explicit memory commits
- integrations from agent runtimes and external systems

## 24.2 Input categories
- raw event append
- transcript import
- explicit memory commit
- artifact import
- summary import
- external system event import

---

# 25. Hooks and workers

## 25.1 Hooks
Hooks should:
- capture
- annotate
- queue work
- honor ingestion policies

## 25.2 Workers
Workers should:
- reflect durable memory
- compress history
- consolidate episodes
- detect contradictions
- dedupe
- update indexes
- repair references

## 25.3 Rule
**Hooks capture. Workers think.**

---

# 26. Maintenance scheduler

## 26.1 Purpose
Memory needs housekeeping.

## 26.2 Jobs
- `consolidate_sessions`
- `compress_history`
- `merge_duplicates`
- `detect_contradictions`
- `decay_stale_memory`
- `refresh_summaries`
- `rebuild_indexes`
- `repair_links`
- `promote_high_value_memories`
- `archive_low_salience_session_traces`

---

# 27. Integrity and repair

## 27.1 Why it matters
Persistent systems rot without repair tools.

## 27.2 Repair/integrity requirements
AMM should be able to validate:
- summary -> source links
- memory -> source links
- supersession chains
- entity link integrity
- orphaned summaries
- orphaned memories
- dangling artifact refs
- broken index state

## 27.3 Repair actions
- relink source spans where possible
- mark damaged nodes
- rebuild derived indexes
- quarantine malformed summaries
- repair or flag supersession cycles

---

# 28. CLI specification

## 28.1 Core commands
- `amm init`
- `amm ingest`
- `amm remember`
- `amm recall`
- `amm history`
- `amm describe`
- `amm expand`
- `amm memory`
- `amm episode`
- `amm entity`
- `amm jobs`
- `amm status`
- `amm explain-recall`
- `amm repair`

---

# 29. API specification

## 29.1 Core endpoints
- `POST /events`
- `POST /transcripts`
- `POST /memories`
- `POST /recall`
- `POST /describe`
- `POST /expand`
- `POST /history`
- `GET /memories/:id`
- `GET /summaries/:id`
- `GET /episodes/:id`
- `GET /entities/:id`
- `POST /jobs/run`
- `POST /repair`
- `POST /explain-recall`
- `GET /status`

---

# 30. Minimal data schema

## 30.1 Canonical tables
- `events`
- `summaries`
- `summary_edges` optional
- `memories`
- `claims`
- `entities`
- `memory_entities`
- `episodes`
- `projects`
- `relationships`
- `jobs`
- `artifacts`

## 30.2 Derived/index tables
- `memories_fts`
- `episodes_fts`
- `events_fts`
- `summaries_fts`
- `embeddings`
- `retrieval_cache`
- `recall_history`
- optional `graph_projection`

---

# 31. Storage backend

## 31.1 Start with SQLite
Still the right move.

## 31.2 Later add Postgres
For shared/multi-agent service mode.

## 31.3 Embeddings
Derived layer only, not product identity.

---

# 32. Privacy and safety

## 32.1 Privacy levels
- `private`
- `shared`
- `public_safe`

## 32.2 Surface-aware filtering
Callers must pass enough context for safe retrieval.

## 32.3 Poisoning and corruption
AMM should eventually support:
- source trust weighting
- user-controlled forgetting
- contradiction resolution
- correction/supersession workflows
- quarantining suspect records

---

# 33. Integration flows

## 33.1 Agent runtime integration
### Per inbound message
1. runtime logs raw event
2. runtime requests ambient recall
3. AMM returns thin hints from memory/history/summary layers
4. runtime injects compact hints
5. agent replies
6. reply logged
7. workers later reflect/compress/index

### Deep recall
If needed:
- `describe` for thin record
- `expand` for richer details
- `history` if transcript context matters

## 33.2 External system integration
External systems may emit:
- decisions
- incidents
- task results
- summaries
- artifacts

AMM may store them as history or canonical memory, depending on policy.

---

# 34. Product positioning

## 34.1 Positioning line
**AMM is a framework-agnostic, typed, temporal, explainable memory substrate for agents — built for ambient recall, expandable summaries, and durable continuity beyond the context window.**

## 34.2 What it is not
- not a transcript DB alone
- not a vector-only memory wrapper
- not a workflow governor
- not a markdown convention
- not a graph maximalist science fair project

---

# 35. Implementation phases

## Phase 0
- SQLite backend
- raw event ingestion
- explicit memory commits
- basic recall
- project/global scope

## Phase 1
- summaries as first-class objects
- ambient recall packet
- describe/expand flow
- episodes
- provenance links

## Phase 2
- hybrid ranking
- temporal validity
- contradiction handling
- ingestion policy controls
- maintenance jobs
- integrity checks

## Phase 3
- Postgres support
- graph-assisted retrieval
- stronger repair tooling
- richer adapters for runtimes and external systems
- better explainability

---

# 36. Current conclusions from our discussion

1. A single `MEMORY.md` is not enough.
2. Database-backed memory is the right direction.
3. AMM should stand as its own project.
4. `project_id` scoping should exist but not be required.
5. Without `project_id`, memory should support broad continuity.
6. Hooks are intake valves, not the whole brain.
7. Background workers should do most heavy lifting.
8. Ambient recall is a core differentiator.
9. Ambient recall should return terse IDs/descriptions.
10. Canonical memory and derived indexes must be separated.
11. Time, supersession, and contradictions must be first-class.
12. Open loops and assumptions matter.
13. Hybrid retrieval beats vector-only retrieval.
14. Explainability should be built in.
15. Raw transcript history is valuable but not sufficient.
16. Summaries should be expandable back to source spans.
17. Retrieval should follow find -> describe -> expand.
18. Ingestion policy control is important.
19. Repairability/integrity should be first-class.

---

If you want, I can next do the same cleanup pass on the **technical blueprint** and give you a **v0.4 blueprint with zero ACM references** too.