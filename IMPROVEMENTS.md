# AMM Improvements Summary

This document summarizes all improvements made to AMM to close gaps with ACM standards and enhance overall quality.

## Files Created

### 1. Documentation of AMM Advantages for ACM
- **`amm-advantages-for-acm.md`** - Documents 10 patterns from AMM that ACM could adopt

### 2. Formal API Schemas (spec/v1/)
- **`spec/v1/README.md`** - Specification overview
- **`spec/v1/cli.command.schema.json`** - Complete JSON Schema for CLI command envelopes
- **`spec/v1/mcp.tools.v1.json`** - MCP tool definitions schema

### 3. Getting Started Guide
- **`docs/getting-started.md`** - Comprehensive walkthrough from installation to advanced usage

### 4. Architecture Diagrams
- **`docs/architecture-diagrams.md`** - Mermaid diagrams showing:
  - System overview
  - Data flow sequences
  - Memory layer architecture
  - Service layer detail
  - Command dispatch flow
  - MCP protocol flow
  - Component relationships

### 5. Example Requests
- **`docs/examples/README.md`** - Documentation for example files
- **`docs/examples/ingest-event.json`** - Single event ingestion example
- **`docs/examples/ingest-transcript.json`** - Bulk event ingestion example
- **`docs/examples/remember.json`** - Memory storage example
- **`docs/examples/recall.json`** - Memory retrieval example
- **`docs/examples/run-job.json`** - Maintenance job example

## Files Modified

### 1. Command Catalog
**`internal/contracts/v1/commands.go`**
- Added `CmdRun` and `CmdValidate` constants
- Added command registry entries for "run" and "validate"

### 2. CLI Runner
**`internal/adapters/cli/runner.go`**
- Added `Version` variable for build-time injection
- Added `run` command support with `runEnvelope()` function
- Added `validate` command support with `validateEnvelope()` function
- Added `version` command with `printVersion()` function
- Added `CommandEnvelope` struct for JSON envelope support
- Added `dispatchEnvelope()` function to handle all commands via envelope
- Updated `printUsage()` with new command categories:
  - Core Commands
  - Automation Commands
  - Info Commands
- All 17 commands available via envelope dispatch:
  - init, ingest_event, ingest_transcript
  - remember, recall, describe, expand, history
  - get_memory, update_memory
  - policy_list, policy_add, policy_remove
  - run_job, explain_recall, repair, status

### 3. CLI Reference Documentation
**`docs/cli-reference.md`**
- Added documentation for `run` command
- Added documentation for `validate` command
- Added documentation for `version` command
- Added documentation for `help` command

### 4. README
**`README.md`**
- Added mermaid architecture diagram at top
- Added Documentation section with organized links:
  - Getting Started
  - Reference Documentation (CLI, MCP, Architecture, Configuration)
  - Integration Guides (Generic, Codex, OpenCode, Hermes, OpenClaw)
  - For Agents
  - Specification
- Added Automation Mode section for CI/CD usage
- Improved structure with clear navigation paths

## CLI/MCP Parity Status

### Complete Parity (17 commands)

| CLI Command | MCP Tool | Status |
|-------------|----------|--------|
| `init` | `amm_init` | ✅ |
| `ingest event` | `amm_ingest_event` | ✅ |
| `ingest transcript` | `amm_ingest_transcript` | ✅ |
| `remember` | `amm_remember` | ✅ |
| `recall` | `amm_recall` | ✅ |
| `describe` | `amm_describe` | ✅ |
| `expand` | `amm_expand` | ✅ |
| `history` | `amm_history` | ✅ |
| `memory show` | `amm_get_memory` | ✅ |
| `memory update` | `amm_update_memory` | ✅ |
| `policy list` | `amm_policy_list` | ✅ |
| `policy add` | `amm_policy_add` | ✅ |
| `policy remove` | `amm_policy_remove` | ✅ |
| `jobs run` | `amm_jobs_run` | ✅ |
| `explain-recall` | `amm_explain_recall` | ✅ |
| `repair` | `amm_repair` | ✅ |
| `status` | `amm_status` | ✅ |

### CLI-Only Commands (4)

These are intentionally CLI-only as they're meta-commands or protocol-level:

| Command | Purpose |
|---------|---------|
| `run` | Execute JSON envelope (CI/automation) |
| `validate` | Validate JSON envelope |
| `version` / `--version` / `-v` | Show version |
| `help` / `--help` / `-h` | Show help |

## New Features Added

### 1. Full JSON Envelope Support
```bash
amm run --in request.json
```
Supports all 17 commands via standardized envelope format:
```json
{
  "version": "amm.v1",
  "command": "<command>",
  "request_id": "...",
  "payload": { ... }
}
```

### 2. Envelope Validation
```bash
amm validate --in request.json
```
Validates without executing - useful for CI pipelines.

### 3. Version Flag
```bash
amm --version
amm -v
```
Shows version information (injected at build time).

### 4. Enhanced Help
- Reorganized command categories
- Clearer usage instructions
- Link to detailed documentation

## Build Instructions

With version injection:
```bash
CGO_ENABLED=1 go build -tags fts5 \
  -ldflags "-X github.com/bonztm/agent-memory-manager/internal/adapters/cli.Version=0.1.0" \
  -o amm ./cmd/amm

CGO_ENABLED=1 go build -tags fts5 \
  -o amm-mcp ./cmd/amm-mcp
```

## Documentation Structure

```
README.md                              # Product overview with architecture diagram
├── docs/
│   ├── getting-started.md             # Complete walkthrough
│   ├── architecture-diagrams.md       # Visual architecture reference
│   ├── cli-reference.md               # All CLI commands
│   ├── mcp-reference.md               # MCP tool definitions
│   ├── architecture.md                # Detailed architecture
│   ├── configuration.md               # Settings and env vars
│   ├── integration.md                 # Generic integration
│   ├── codex-integration.md           # Codex-specific
│   ├── opencode-integration.md        # OpenCode-specific
│   ├── openclaw-integration.md        # OpenClaw-specific
│   ├── hermes-agent-integration.md    # Hermes-specific
│   ├── agent-onboarding.md            # Guide for agents
│   └── examples/                      # Request examples
│       ├── README.md
│       ├── ingest-event.json
│       ├── ingest-transcript.json
│       ├── remember.json
│       ├── recall.json
│       └── run-job.json
├── spec/v1/                           # Formal API schemas
│   ├── README.md
│   ├── cli.command.schema.json
│   └── mcp.tools.v1.json
└── amm-advantages-for-acm.md          # Patterns for ACM retrofit
```

## Standards Compliance

| Standard | Status | Notes |
|----------|--------|-------|
| API-first service layer | ✅ | Single Service interface |
| Thin main pattern | ✅ | 15-line mains |
| CLI/MCP parity | ✅ | 17 shared commands |
| JSON envelope support | ✅ | run/validate commands |
| Version flag | ✅ | --version / -v |
| Command catalog | ✅ | contracts/v1/commands.go |
| JSON schemas | ✅ | spec/v1/ directory |
| Architecture diagrams | ✅ | Mermaid diagrams |
| Getting started guide | ✅ | docs/getting-started.md |
| Example requests | ✅ | docs/examples/ |
| Separate CLI/MCP docs | ✅ | cli-reference.md, mcp-reference.md |

## Next Steps for ACM Retrofit

The `amm-advantages-for-acm.md` file contains detailed retrofit instructions for:
1. Ultra-thin main pattern
2. JSON-RPC 2.0 MCP protocol
3. Separate CLI/MCP documentation
4. Integration guide architecture
5. Architecture documentation depth
6. Command catalog pattern
7. Schema-first design
8. Ultra-minimal binary entrypoints
9. Consistent error envelope pattern
10. Runtime-specific integration guides
