# HTTP API Reference

AMM provides a RESTful HTTP API for integrating with agent runtimes that prefer network-based communication over CLI or MCP.

## Overview

- **Base URL**: `http://localhost:8080/v1` (default)
- **Content-Type**: `application/json`
- **Authentication**: When `AMM_API_KEY` is set, requests must include the key via `Authorization: Bearer <key>` or `X-API-Key: <key>` header.

## Authentication

When the `AMM_API_KEY` environment variable is set on the `amm-http` server, all API requests require authentication.

**Exempt Endpoints**
The following endpoints do not require an API key:
- `GET /healthz`
- `GET /v1/status`
- `GET /openapi.json`
- `GET /swagger/`

**Passing the Key**
Include the key in your requests using one of these headers:
- `Authorization: Bearer <your-api-key>`
- `X-API-Key: <your-api-key>`

## OpenAPI & Swagger

- **OpenAPI 3.0 Specification**: Available at `GET /openapi.json`.
- **Swagger UI**: Interactive API documentation is available at `GET /swagger/`.

## Response Envelope

All successful responses are wrapped as:

```json
{"data": <payload>}
```

All error responses are wrapped as:

```json
{"error": {"code": "<error_code>", "message": "<message>"}}
```

## MCP-over-HTTP

The `/v1/mcp` endpoint exposes all AMM tools via the MCP Streamable HTTP protocol. This allows MCP-compatible clients to connect to AMM over a network instead of using stdio.

**Example Configuration (Claude Code)**
```json
{
  "mcpServers": {
    "amm": {
      "url": "http://localhost:8080/v1/mcp"
    }
  }
}
```

If `AMM_API_KEY` is set on the server, configure the MCP client to send the same bearer token or API-key header it uses for the REST API.

---

## Route Table

| Method | Path | Description |
|--------|------|-------------|
| GET | `/healthz` | System liveness check (no auth) |
| GET | `/openapi.json` | OpenAPI 3.0 specification (no auth) |
| GET | `/swagger/` | Swagger UI documentation (no auth) |
| POST | `/v1/mcp` | MCP-over-HTTP streamable endpoint |
| GET | `/v1/status` | System statistics (no auth) |
| POST | `/v1/events` | Ingest a single event |
| POST | `/v1/transcripts` | Bulk ingest events |
| POST | `/v1/memories` | Create a durable memory |
| GET | `/v1/memories/{id}` | Get a memory by ID |
| PATCH | `/v1/memories/{id}` | Update a memory |
| DELETE | `/v1/memories/{id}` | Retract a memory |
| PATCH | `/v1/memories/{id}/share` | Update privacy level |
| POST | `/v1/recall` | Search memories |
| POST | `/v1/explain-recall` | Breakdown of recall signals |
| GET | `/v1/expand/{id}` | Fetch full item details |
| GET | `/v1/grep` | Search raw events and group matches by covering summary |
| GET | `/v1/context-window` | Assemble formatted context window |
| POST | `/v1/describe` | Get metadata for multiple IDs |
| POST | `/v1/projects` | Register a project |
| GET | `/v1/projects` | List projects |
| GET | `/v1/projects/{id}` | Get a project |
| DELETE | `/v1/projects/{id}` | Remove a project |
| POST | `/v1/relationships` | Create an entity relationship |
| GET | `/v1/relationships` | List relationships |
| GET | `/v1/relationships/{id}` | Get a relationship |
| DELETE | `/v1/relationships/{id}` | Remove a relationship |
| GET | `/v1/summaries/{id}` | Get a summary |
| GET | `/v1/episodes/{id}` | Get an episode |
| GET | `/v1/entities/{id}` | Get an entity |
| POST | `/v1/jobs/{kind}` | Trigger a maintenance job |
| POST | `/v1/init` | Initialize database |
| POST | `/v1/history` | Query raw history |
| GET | `/v1/policies` | List ingestion policies |
| POST | `/v1/policies` | Add an ingestion policy |
| DELETE | `/v1/policies/{id}` | Remove a policy |
| POST | `/v1/repair` | Run integrity checks |
| POST | `/v1/reset-derived` | Purge derived data |

---

## Health & System


### GET /healthz
Check if the server is alive.

**Response**
- `data.status`: `"ok"`

Example:

```json
{"data":{"status":"ok"}}
```

### GET /v1/status
Get system statistics and status.

**Response**
- `data.db_path`: Path to the database file.
- `data.initialized`: Boolean status.
- `data.event_count`: Total events ingested.
- `data.memory_count`: Total durable memories.
- `data.summary_count`: Total summaries.
- `data.episode_count`: Total episodes.
- `data.entity_count`: Total entities in graph.

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
  - `mode`: `ambient`, `facts`, `episodes`, `sessions`, `timeline`, `project`, `entity`, `active`, `history`, `contradictions`, `hybrid`.
  - `limit`: Max items to return.
  - `project_id`: Filter by project.
  - `after`: RFC3339 timestamp — filter results to after this time.
  - `before`: RFC3339 timestamp — filter results to before this time.
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
- `delegation_depth`: (Optional) Max recursive delegation depth for linked content.
- `max_depth`: (Optional) Recursively expand child summaries up to N levels deep (0–5, default 0). When >0, populates `expanded_children` in the response.

**Errors**
- `403 EXPANSION_RECURSION_BLOCKED`: Expansion depth limit reached.

### GET /v1/grep
Search raw events for a text pattern, then group matches by covering summary.

**Query Parameters**
- `pattern`: (Required) Regex or text pattern to search for.
- `project_id`: (Optional) Filter to a project.
- `session_id`: (Optional) Filter to a session.
- `max_group_depth`: (Optional) Max summary depth when finding a covering summary.
- `group_limit`: (Optional) Max groups to return.
- `matches_per_group`: (Optional) Max matches to keep per group.

### GET /v1/context-window
Assemble and format a context window for an agent based on recent activity and relevant memories.

**Query Parameters**
- `project_id`: (Optional) Project identifier.
- `session_id`: (Optional) Session identifier.
- `fresh_tail_count`: (Optional) Number of fresh events to include.
- `max_summary_depth`: (Optional) Max summary depth to include.
- `include_parent_refs`: (Optional) Include parent summary references.

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
Trigger a background maintenance job. 26 job kinds are available, including Phase 7 trim and compaction jobs.

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
- `pattern_type`: `kind`, `session`, `source`, `surface`, `agent`, `project`, or `runtime`.
- `pattern`: Pattern string to match.
- `mode`: `full`, `read_only`, or `ignore`.
- `match_mode`: (Optional) `exact`, `glob`, or `regex`.
- `priority`: (Optional) Integer priority (higher wins).

**Recommended policies** (add after initialization for all deployments):
```bash
# Ignore tool events to prevent tool JSON from polluting extracted memories
curl -X POST http://localhost:8080/v1/policies \
  -H "Content-Type: application/json" \
  -d '{"pattern_type":"kind","pattern":"tool_call","mode":"ignore","match_mode":"exact","priority":100}'

curl -X POST http://localhost:8080/v1/policies \
  -H "Content-Type: application/json" \
  -d '{"pattern_type":"kind","pattern":"tool_result","mode":"ignore","match_mode":"exact","priority":100}'
```

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
