# AMM Request Examples

This directory contains example JSON request envelopes for use with `amm run --in <file>`.

## Files

- `ingest-event.json` - Single event ingestion
- `ingest-transcript.json` - Bulk event ingestion
- `remember.json` - Store a durable memory
- `recall.json` - Retrieve memories
- `run-job.json` - Execute a maintenance job (reflect)
- `run-job-compress.json` - Run the compress job (builds hierarchical summaries with three-level escalation)

## Usage

```bash
# Execute any example
amm run --in docs/examples/remember.json

# Validate without executing
amm validate --in docs/examples/remember.json
```

## Creating Your Own

Use these examples as templates. All envelopes follow this structure:

```json
{
  "version": "amm.v1",
  "command": "<command_name>",
  "request_id": "<optional-id>",
  "payload": { ... }
}
```

See the [CLI Reference](../cli-reference.md) for all available commands and their payloads.
