# Codex Integration Guide

This guide shows how to use AMM with Codex without pretending AMM owns the Codex runtime. The integration boundary is simple:

- Codex owns prompt execution, hook registration, and context injection.
- AMM owns durable storage, recall, and maintenance jobs through `amm` and `amm-mcp`.
- Background workers can stay completely out-of-band as `amm jobs run <kind>` calls against the same SQLite database.

That means you do **not** need an AMM daemon inside Codex to get value. Hooks improve the hot path, but the workers are still just external binary calls.

## Recommended Shape

Use four pieces together:

1. **MCP** for explicit memory operations (`amm_recall`, `amm_expand`, `amm_remember`, `amm_jobs_run`)
2. **Codex hooks** for automatic event capture and ambient recall
3. **Repo instructions** for when the agent should consult or write memory deliberately
4. **External worker scheduling** for `reflect`, `compress_history`, and the heavier maintenance jobs

## 1. Build and Initialize AMM

```bash
CGO_ENABLED=1 go build -tags fts5 -o ~/.local/bin/amm ./cmd/amm
CGO_ENABLED=1 go build -tags fts5 -o ~/.local/bin/amm-mcp ./cmd/amm-mcp

AMM_DB_PATH=~/.amm/amm.db ~/.local/bin/amm init
AMM_DB_PATH=~/.amm/amm.db ~/.local/bin/amm status
```

## 2. Register AMM as an MCP Server

Codex reads user-level configuration from `~/.codex/config.toml` and can also load project overrides from `.codex/config.toml`.

```toml
[mcp_servers.amm]
command = "/home/you/.local/bin/amm-mcp"
env = { AMM_DB_PATH = "/home/you/.amm/amm.db" }
required = false

[features]
codex_hooks = true
```

That gives Codex direct access to the AMM MCP tools while keeping the public AMM surface exactly the same as every other runtime: stdio MCP plus the CLI.

## 3. Add Hook-Based Capture

Codex now exposes three especially useful hook points for AMM capture:

| Hook | Best AMM use |
|---|---|
| `SessionStart` | Record session metadata and establish project/session identity |
| `UserPromptSubmit` | Ingest the user prompt and return thin ambient recall hints |
| `Stop` | Record closeout/transcript metadata and trigger maintenance jobs |

The example files in [`examples/codex/`](../examples/codex/) show one grounded pattern:

- `session-start.py` logs a lightweight session-start event
- `user-prompt-submit.py` ingests the user prompt and turns AMM recall results into Codex hook `additionalContext`
- `session-stop.py` records transcript/session metadata and runs maintenance jobs
- `hooks.json` wires those scripts into Codex

### Example `hooks.json`

```json
{
  "hooks": {
    "SessionStart": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "python3 examples/codex/session-start.py",
            "timeoutSec": 10,
            "statusMessage": "recording Codex session start in AMM"
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "python3 examples/codex/user-prompt-submit.py",
            "timeoutSec": 10,
            "statusMessage": "capturing prompt and loading AMM recall"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "python3 examples/codex/session-stop.py",
            "timeoutSec": 20,
            "statusMessage": "closing out Codex session in AMM"
          }
        ]
      }
    ]
  }
}
```

## 4. Add an Agent Instructions Snippet

If you want a Codex-specific instruction block, add something like this to your repo instructions or keep it in a companion doc for copy/paste:

```md
## AMM memory usage

- Treat AMM as the durable memory system for this repository.
- At task start, repo switch, or resume after interruption, consult AMM via `amm_recall` or `amm recall --mode ambient`.
- If AMM returns thin recall items, expand only the items you actually need before acting.
- Record only stable, high-confidence memories explicitly with `amm_remember`; let background workers extract the rest from history.
- Do not assume AMM runs its own scheduler. Maintenance jobs run externally via `amm jobs run <kind>`.
```

## 5. Keep Workers Out-of-Band

This is the important operational point: **the workers do not need to live inside Codex**.

Use cron, systemd, a wrapper script, or any other local scheduler to invoke the same AMM database out-of-band:

```bash
AMM_DB_PATH=~/.amm/amm.db ~/.local/bin/amm jobs run reflect
AMM_DB_PATH=~/.amm/amm.db ~/.local/bin/amm jobs run compress_history
AMM_DB_PATH=~/.amm/amm.db ~/.local/bin/amm jobs run consolidate_sessions
```

The existing shared runner in [`examples/scripts/run-workers.sh`](../examples/scripts/run-workers.sh) is already a truthful model for this.

## Suggested Runtime Pattern

Use a split hot-path / warm-path / cold-path model:

- **Hot path**: `UserPromptSubmit` ingests the prompt and asks for `ambient` recall
- **Warm path**: `Stop` runs `reflect` and `compress_history`
- **Cold path**: cron or systemd runs `consolidate_sessions`, `extract_claims`, `form_episodes`, `detect_contradictions`, `merge_duplicates`, and `cleanup_recall_history`

That keeps the Codex interaction fast while still letting AMM build structure from the accumulated history.

## Notes on Transcript Capture

Codex hook payloads expose `session_id` and optional `transcript_path`, and `UserPromptSubmit` also includes `turn_id` and `prompt`. The examples in this repo use those fields conservatively:

- prompt capture happens immediately in `UserPromptSubmit`
- session metadata is captured in `SessionStart`
- session closeout and maintenance happen in `Stop`

If you want full transcript archival, you can extend the stop hook to ingest the transcript contents or build a separate importer for the transcript format you use locally.

## Verification Checklist

- `amm-mcp` starts successfully with the configured `AMM_DB_PATH`
- Codex can see the `amm` MCP server
- `UserPromptSubmit` writes an event and returns recall hints when AMM has relevant data
- `Stop` can run `amm jobs run reflect` and `amm jobs run compress_history`
- Your external scheduler can run the heavier jobs against the same database

## What This Repo Does Not Promise

- a built-in AMM scheduler or daemon
- a Codex-native AMM plugin package
- automatic consumption of `maintenance.auto_*` flags by a running worker loop
- a one-size-fits-all Codex transcript importer
