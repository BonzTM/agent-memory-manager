# Codex Integration Guide

This guide shows how to use amm with Codex without pretending amm owns the Codex runtime. The integration boundary is simple:

- Codex owns prompt execution, hook registration, and context injection.
- amm owns durable storage, recall, and maintenance jobs through `amm` and `amm-mcp`.
- Background workers can stay completely out-of-band as `amm jobs run <kind>` calls against the same SQLite database.

That means you do **not** need an amm daemon inside Codex to get value. Hooks improve the hot path, but the workers are still just external binary calls.

## Recommended Shape

Use four pieces together:

1. **MCP** for explicit memory operations (`amm_recall`, `amm_expand`, `amm_remember`, `amm_jobs_run`)
2. **Codex hooks** for automatic event capture and ambient recall
3. **Repo instructions** for when the agent should consult or write memory deliberately
4. **External worker scheduling** for `reflect`, `compress_history`, and the heavier maintenance jobs

## Default operator contract

Codex operators should keep the hot path simple:

- ask AMM for ambient recall at task start, repo switch, or resume
- expand only the items needed for the current task
- explicitly remember only stable, high-confidence knowledge
- let hooks capture prompts/session metadata/tool history where Codex actually exposes those surfaces
- keep `amm jobs run <kind>` external to the Codex runtime

If the repo also uses ACM, ACM owns task workflow and AMM owns durable memory.

## 1. Build and Initialize amm

```bash
mkdir -p /tmp/amm-build

CGO_ENABLED=1 go build -tags fts5 -o /tmp/amm-build/amm ./cmd/amm
CGO_ENABLED=1 go build -tags fts5 -o /tmp/amm-build/amm-mcp ./cmd/amm-mcp

sudo install -m 755 /tmp/amm-build/amm /usr/local/bin/amm
sudo install -m 755 /tmp/amm-build/amm-mcp /usr/local/bin/amm-mcp

AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm init
AMM_DB_PATH=~/.amm/amm.db /usr/local/bin/amm status
```

## 2. Register amm as an MCP Server

Codex reads user-level configuration from `~/.codex/config.toml` and discovers a separate `~/.codex/hooks.json` file for hook handlers. Project overrides can live under `.codex/` in the repo, but the global/user setup is the cleanest place to start.

```toml
[mcp_servers.amm]
command = "/usr/local/bin/amm-mcp"

[mcp_servers.amm.env]
AMM_DB_PATH = "/home/you/.amm/amm.db"

[features]
codex_hooks = true
```

That gives Codex direct access to the amm MCP tools while keeping the public amm surface exactly the same as every other runtime: stdio MCP plus the CLI.

The repo ships a matching example at [`examples/codex/config.toml`](../examples/codex/config.toml).

## 3. Add Hook-Based Capture

Codex currently exposes three confirmed lifecycle hooks for amm capture:

| Hook | Best amm use |
|---|---|
| `SessionStart` | Record session metadata and establish project/session identity |
| `UserPromptSubmit` | Ingest the user prompt and return thin ambient recall hints |
| `Stop` | Backfill assistant/tool history from `transcript_path` when available, then record session closeout and trigger maintenance jobs |

The example files in [`examples/codex/`](../examples/codex/) show one grounded pattern:

- `session-start.py` logs a lightweight session-start event
- `user-prompt-submit.py` ingests the user prompt and turns amm recall results into Codex hook `additionalContext`
- `session-stop.py` imports assistant/tool history from `transcript_path` when possible, falls back to `last_assistant_message` when not, then records concise session metadata and runs maintenance jobs
- `config.toml` enables `codex_hooks` and registers `amm-mcp`
- `hooks.json` is the separate Codex hook manifest that wires those scripts into Codex

### Install the Hook Scripts

The simplest global layout is to keep the executable hook scripts under `~/.amm/hooks/` alongside your Claude hook scripts:

```bash
mkdir -p ~/.amm/hooks

cp examples/codex/session-start.py ~/.amm/hooks/codex-session-start.py
cp examples/codex/user-prompt-submit.py ~/.amm/hooks/codex-user-prompt-submit.py
cp examples/codex/session-stop.py ~/.amm/hooks/codex-stop.py

chmod +x ~/.amm/hooks/codex-session-start.py
chmod +x ~/.amm/hooks/codex-user-prompt-submit.py
chmod +x ~/.amm/hooks/codex-stop.py
```

### Example `~/.codex/hooks.json`

```json
{
  "hooks": {
    "SessionStart": [
      {
        "matcher": "startup|resume|clear",
        "hooks": [
          {
            "type": "command",
            "command": "python3 /home/you/.amm/hooks/codex-session-start.py",
            "timeoutSec": 10,
            "statusMessage": "recording Codex session start in amm"
          }
        ]
      }
    ],
    "UserPromptSubmit": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "python3 /home/you/.amm/hooks/codex-user-prompt-submit.py",
            "timeoutSec": 10,
            "statusMessage": "capturing prompt and loading amm recall"
          }
        ]
      }
    ],
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "python3 /home/you/.amm/hooks/codex-stop.py",
            "timeoutSec": 20,
            "statusMessage": "closing out Codex session in amm"
          }
        ]
      }
    ]
  }
}
```

Codex does **not** load hooks from `config.toml`; the hooks live in `hooks.json` next to the config file. The repo example manifest is at [`examples/codex/hooks.json`](../examples/codex/hooks.json).

## 4. Add an Agent Instructions Snippet

If you want a Codex-specific instruction block, add something like this to your repo instructions or keep it in a companion doc for copy/paste:

```md
## amm memory usage

- Treat amm as the durable memory system for this repository.
- At task start, repo switch, or resume after interruption, consult amm via `amm_recall` or `amm recall --mode ambient`.
- If amm returns thin recall items, expand only the items you actually need before acting.
- Record only stable, high-confidence memories explicitly with `amm_remember`; let background workers extract the rest from history.
- Do not assume amm runs its own scheduler. Maintenance jobs run externally via `amm jobs run <kind>`.
```

## 5. Keep Workers Out-of-Band

This is the important operational point: **the workers do not need to live inside Codex**.

Because SQLite supports only one concurrent writer, we recommend running maintenance jobs sequentially. Use cron, systemd, or a wrapper script to invoke the **conservative baseline** sequence in a deterministic order:

```bash
# Recommended: Serialized Baseline Runner
/home/you/src/agent-memory-manager/examples/scripts/run-workers.sh
```

The baseline runner covers the full maintenance sequence (`reflect`, `compress_history`, `consolidate_sessions`, `merge_duplicates`, `extract_claims`, `form_episodes`, `detect_contradictions`, `decay_stale_memory`, `promote_high_value`, `archive_session_traces`, `rebuild_indexes`, `cleanup_recall_history`). Structural repairs (`repair_links`) should be run separately as needed.

The existing shared runner in [`examples/scripts/run-workers.sh`](../examples/scripts/run-workers.sh) is the preferred model for the cold-path baseline.

## Suggested Runtime Pattern

Use a split hot-path / warm-path / cold-path model to manage SQLite's single-writer constraint:

- **Hot path**: `UserPromptSubmit` ingests the prompt and asks for `ambient` recall
- **Warm path**: `Stop` runs the repo's warm-path maintenance sequence (`reflect`, `compress_history`, `consolidate_sessions`) serially
- **Cold path**: external serialized jobs run the **baseline** maintenance sequence (consolidate_sessions, extract_claims, etc.) via the shared runner
That keeps the Codex interaction fast while still letting amm build structure from the accumulated history.

## Notes on Transcript Capture

Codex hook payloads expose `session_id` and optional `transcript_path`, and `UserPromptSubmit` also includes `turn_id` and `prompt`. The examples in this repo use those fields conservatively:

- prompt capture happens immediately in `UserPromptSubmit`
- session metadata is captured in `SessionStart`
- `Stop` tries to import structured assistant/tool history from the transcript file before it emits a concise `session_stop`
- if transcript import yields no assistant message, `Stop` falls back to `last_assistant_message`

This repo still does **not** claim that Codex exposes a public tool-result hook. The richer tool history comes from local stop-time transcript parsing, not from a documented Codex lifecycle hook.

## Verification Checklist

- `/usr/local/bin/amm-mcp` starts successfully with the configured `AMM_DB_PATH`
- Codex can see the `amm` MCP server
- Codex loads `~/.codex/hooks.json` and the three hook handlers
- `UserPromptSubmit` writes an event and returns recall hints when amm has relevant data
- `Stop` backfills assistant/tool history from `transcript_path` or falls back to `last_assistant_message`
- `Stop` can run `amm jobs run reflect`, `amm jobs run compress_history`, and `amm jobs run consolidate_sessions`
- Your external scheduler can run the heavier jobs against the same database

## What This Repo Does Not Promise

- a built-in amm scheduler or daemon
- a Codex-native amm plugin package
- a public Codex tool-result hook surface
- automatic consumption of `maintenance.auto_*` flags by a running worker loop
- a one-size-fits-all Codex transcript importer
