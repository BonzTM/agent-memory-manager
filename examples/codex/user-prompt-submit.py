#!/usr/bin/env python3

import json
import os
import subprocess
import sys
from datetime import datetime, timezone


def now_rfc3339() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def run_json(command: list[str], stdin: str | None = None) -> dict:
    amm_bin = os.environ.get("AMM_BIN", "/usr/local/bin/amm")
    db_path = os.environ.get("AMM_DB_PATH", os.path.expanduser("~/.amm/amm.db"))
    proc = subprocess.run(
        [amm_bin, *command],
        input=stdin,
        text=True,
        capture_output=True,
        env={**os.environ, "AMM_DB_PATH": db_path},
        check=False,
    )
    if proc.returncode != 0:
        return {}
    try:
        return json.loads(proc.stdout)
    except json.JSONDecodeError:
        return {}


def run_ingest(event: dict) -> None:
    run_json(["ingest", "event", "--in", "-"], json.dumps(event))


def render_additional_context(recall_result: dict) -> str | None:
    items = recall_result.get("result", {}).get("items", [])
    if not items:
        return None
    lines = ["amm ambient recall:"]
    for item in items[:5]:
        kind = item.get("kind", "item")
        desc = item.get("tight_description", "")
        score = item.get("score", 0)
        lines.append(f"- [{kind}] {desc} (score: {score:.2f})")
    return "\n".join(lines)


def main() -> int:
    payload = json.load(sys.stdin)
    session_id = payload.get("session_id", "")
    turn_id = payload.get("turn_id", "")
    cwd = payload.get("cwd", "")
    transcript_path = payload.get("transcript_path") or ""
    model = payload.get("model", "")
    permission_mode = payload.get("permission_mode", "")
    prompt = payload.get("prompt", "")
    project_id = os.environ.get("AMM_PROJECT_ID", "")

    event = {
        "kind": "message_user",
        "source_system": "codex",
        "session_id": session_id,
        "project_id": project_id,
        "actor_type": "user",
        "content": prompt,
        "metadata": {
            "hook_event": "UserPromptSubmit",
            "turn_id": turn_id,
            "cwd": cwd,
            "transcript_path": transcript_path,
            "model": model,
            "permission_mode": permission_mode,
        },
        "occurred_at": now_rfc3339(),
    }
    run_ingest(event)

    recall = run_json(
        [
            "recall",
            "--mode",
            "ambient",
            "--session",
            session_id,
            "--project",
            project_id,
            prompt,
        ]
    )
    additional_context = render_additional_context(recall)

    output: dict[str, object] = {
        "systemMessage": "amm captured the prompt and checked ambient recall."
    }
    if additional_context:
        output["hookSpecificOutput"] = {
            "hookEventName": "UserPromptSubmit",
            "additionalContext": additional_context,
        }
    print(json.dumps(output))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
