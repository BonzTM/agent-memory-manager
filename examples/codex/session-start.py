#!/usr/bin/env python3

import json
import os
import subprocess
import sys
from datetime import datetime, timezone


def now_rfc3339() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def run_amm_ingest(event: dict) -> None:
    amm_bin = os.environ.get("AMM_BIN", "/usr/local/bin/amm")
    db_path = os.environ.get("AMM_DB_PATH", os.path.expanduser("~/.amm/amm.db"))
    subprocess.run(
        [amm_bin, "ingest", "event", "--in", "-"],
        input=json.dumps(event),
        text=True,
        env={**os.environ, "AMM_DB_PATH": db_path},
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        check=False,
    )


def main() -> int:
    payload = json.load(sys.stdin)
    session_id = payload.get("session_id", "")
    cwd = payload.get("cwd", "")
    transcript_path = payload.get("transcript_path") or ""
    model = payload.get("model", "")
    permission_mode = payload.get("permission_mode", "")
    source = payload.get("source", "")
    project_id = os.environ.get("AMM_PROJECT_ID", "")

    event = {
        "kind": "session_start",
        "source_system": "codex",
        "session_id": session_id,
        "project_id": project_id,
        "content": f"Codex session started in {cwd or 'unknown cwd'}",
        "metadata": {
            "hook_event": "SessionStart",
            "cwd": cwd,
            "transcript_path": transcript_path,
            "model": model,
            "permission_mode": permission_mode,
            "source": source,
        },
        "occurred_at": now_rfc3339(),
    }
    run_amm_ingest(event)

    print(json.dumps({"systemMessage": "AMM recorded Codex session start."}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
