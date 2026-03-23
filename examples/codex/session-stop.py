#!/usr/bin/env python3

import json
import os
import pathlib
import subprocess
import sys
from datetime import datetime, timezone


def now_rfc3339() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def run_amm(command: list[str], stdin: str | None = None) -> None:
    amm_bin = os.environ.get("AMM_BIN", "amm")
    db_path = os.environ.get("AMM_DB_PATH", os.path.expanduser("~/.amm/amm.db"))
    subprocess.run(
        [amm_bin, *command],
        input=stdin,
        text=True,
        env={**os.environ, "AMM_DB_PATH": db_path},
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        check=False,
    )


def read_transcript(transcript_path: str) -> str:
    if not transcript_path:
        return ""
    path = pathlib.Path(transcript_path)
    if not path.exists() or not path.is_file():
        return ""
    try:
        return path.read_text(encoding="utf-8")
    except OSError:
        return ""


def main() -> int:
    payload = json.load(sys.stdin)
    session_id = payload.get("session_id", "")
    turn_id = payload.get("turn_id", "")
    cwd = payload.get("cwd", "")
    transcript_path = payload.get("transcript_path") or ""
    model = payload.get("model", "")
    permission_mode = payload.get("permission_mode", "")
    last_assistant_message = payload.get("last_assistant_message") or ""
    stop_hook_active = payload.get("stop_hook_active")
    project_id = os.environ.get("AMM_PROJECT_ID", "")

    transcript_text = read_transcript(transcript_path)
    content = transcript_text or last_assistant_message or f"Codex session stopped in {cwd or 'unknown cwd'}"

    event = {
        "kind": "session_stop",
        "source_system": "codex",
        "session_id": session_id,
        "project_id": project_id,
        "content": content,
        "metadata": {
            "hook_event": "Stop",
            "turn_id": turn_id,
            "cwd": cwd,
            "transcript_path": transcript_path,
            "model": model,
            "permission_mode": permission_mode,
            "stop_hook_active": str(stop_hook_active),
        },
        "occurred_at": now_rfc3339(),
    }
    run_amm(["ingest", "event", "--in", "-"], json.dumps(event))

    for job in ["reflect", "compress_history", "consolidate_sessions"]:
        run_amm(["jobs", "run", job])

    print(json.dumps({"systemMessage": "AMM recorded Codex session stop and ran maintenance jobs."}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
