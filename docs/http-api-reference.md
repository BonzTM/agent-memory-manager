# HTTP API Reference

AMM provides a RESTful HTTP API for integrating with agent runtimes that prefer network-based communication over CLI or MCP.

## Overview

- **Base URL**: `http://localhost:8080/v1` (default)
- **Content-Type**: `application/json`
- **Authentication**: None (currently designed for local/sidecar use)

### Response Envelope

All successful responses are wrapped in a `data` object:

```json
{
  "data": {
    "id": "mem_123",
    "type": "fact",
    "body": "User prefers Go"
  }
}
```

Errors are returned in an `error` object with a stable code and descriptive message:

```json
{
  "error": {
    "code": "not_found",
    "message": "memory with id mem_123 not found"
  }
}
```

### Status Codes

| Status | Meaning |
|--------|---------|
| 200 OK | Request succeeded. |
| 201 Created | Resource created successfully. |
| 204 No Content | Request succeeded, no response body (e.g., DELETE). |
| 400 Bad Request | Invalid input or malformed JSON. |
| 404 Not Found | Resource does not exist. |
| 415 Unsupported Media Type | Content-Type header is not `application/json`. |
| 500 Internal Server Error | An unexpected error occurred on the server. |

---

## Health & System

### GET /healthz
Check if the server is alive.

**Response**
- `status`: "ok"

### GET /v1/status
Get system statistics and status.

**Response**
- `db_path`: Path to the database file.
- `initialized`: Boolean status.
- `event_count`: Total events ingested.
- `memory_count`: Total durable memories.
- `summary_count`: Total summaries.
- `episode_count`: Total episodes.
- `entity_count`: Total entities in graph.

---

## Events & Transcripts

### POST /v1/events
Ingest a single raw interaction event.

**Request Body**
- `kind`: (Required) e.g., `message_user`, `message_assistant`, `tool_call`.
- `source_system`: (Required) Name of the originating system.
- `content`: (Required) The raw text or data.
- `session_id`: (Optional) Group events by session.
- `project_id`: (Optional) Scope event to a project.
- `agent_id`: (Optional) Identifier for the acting agent.
- `metadata`: (Optional) Key-value pairs.

**Example**
```bash
curl -X POST http://localhost:8080/v1/events \
  -H "Content-Type: application/json" \
  -d '{"kind":"message_user", "source_system":"cli", "content":"hello"}'
```

### POST /v1/transcripts
Bulk ingest a sequence of events.

**Request Body**
- `events`: Array of event objects.

---

## Memories

### POST /v1/memories
Explicitly commit a durable memory.

**Request Body**
- `type`: `fact`, `preference`, `decision`, etc.
- `scope`: `global`, `project`, or `session`.
- `body`: Full memory content.
- `tight_description`: Short summary for retrieval.
- `subject`: (Optional) Subject of the memory.
- `project_id`: (Optional) Required if scope is `project`.

### GET /v1/memories/{id}
Retrieve a single memory by ID.

### PATCH /v1/memories/{id}
Update an existing memory.

### DELETE /v1/memories/{id}
Retract (forget) a memory.

### PATCH /v1/memories/{id}/share
Update privacy level.

**Request Body**
- `privacy`: `private`, `shared`, or `public_safe`.

---

## Recall & Exploration

### POST /v1/recall
Perform associative retrieval.

**Request Body**
- `query`: The search string.
- `opts`:
  - `mode`: `ambient`, `facts`, `episodes`, `timeline`, `project`, `entity`, `active`, `history`, `hybrid`.
  - `limit`: Max items to return.
  - `project_id`: Filter by project.
  - `explain`: Include scoring signals in response.

### POST /v1/explain-recall
Get a detailed breakdown of why an item surfaced for a specific query.

**Request Body**
- `query`: The query string.
- `item_id`: The ID of the item to explain.

### GET /v1/expand/{id}
Fetch full details (claims, source events, children) for a memory, summary, or episode.

**Query Parameters**
- `kind`: `memory`, `summary`, or `episode`.

### POST /v1/describe
Fetch thin descriptions for multiple IDs at once.

**Request Body**
- `ids`: Array of strings.

---

## Knowledge Graph

### POST /v1/projects
Register a new project.

### GET /v1/projects
List all registered projects.

### POST /v1/relationships
Create a directed relationship between entities.

**Request Body**
- `from_entity_id`: Source entity ID.
- `to_entity_id`: Target entity ID.
- `relationship_type`: e.g., `member_of`, `depends_on`.

---

## Maintenance

### POST /v1/jobs/{kind}
Trigger a background maintenance job. 25 job kinds are available, including Phase 7 trim and compaction jobs.

**Path Parameters**
- `kind`: `reflect`, `rebuild_indexes`, `compress_history`, `purge_old_events`, `vacuum_analyze`, etc.

### POST /v1/init
Initialize the database. Safe to call multiple times.

### POST /v1/history
Query raw interaction history.

**Request Body**
- `query`: (Optional) Search string.
- `opts`:
  - `session_id`: Filter by session.
  - `project_id`: Filter by project.
  - `limit`: Max items to return.

---

## Policies

### GET /v1/policies
List all ingestion policies.

### POST /v1/policies
Add an ingestion policy.

**Request Body**
- `pattern_type`: `session`, `source`, `surface`, `agent`, `project`, or `runtime`.
- `pattern`: Pattern string to match.
- `mode`: `full`, `read_only`, or `ignore`.
- `match_mode`: (Optional) `exact`, `glob`, or `regex`.
- `priority`: (Optional) Integer priority (higher wins).

### DELETE /v1/policies/{id}
Remove a policy by ID.

---

## Projects (continued)

### GET /v1/projects/{id}
Get a single project by ID.

### DELETE /v1/projects/{id}
Remove a project by ID.

---

## Relationships (continued)

### GET /v1/relationships/{id}
Get a single relationship by ID.

### GET /v1/relationships
List all relationships. Supports query parameter filtering.

### DELETE /v1/relationships/{id}
Remove a relationship by ID.

---

## Knowledge Graph Retrieval

### GET /v1/summaries/{id}
Get a summary by ID.

### GET /v1/episodes/{id}
Get an episode by ID.

### GET /v1/entities/{id}
Get an entity by ID.

---

## Maintenance (continued)

### POST /v1/repair
Run integrity checks and repairs.

**Request Body**
- `check`: Boolean.
- `fix`: Name of fix to apply (e.g., `indexes`, `links`).

### POST /v1/reset-derived
Purge all derived data while preserving raw events.

**Request Body**
- `confirm`: Must be `true` to proceed. Returns 400 if omitted or false.
