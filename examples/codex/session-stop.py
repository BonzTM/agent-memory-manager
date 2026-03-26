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
    amm_bin = os.environ.get("AMM_BIN", "/usr/local/bin/amm")
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
def run_amm_json(command: list[str], stdin: str | None = None) -> dict:
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


def joined_message_text(content: object) -> str:
    if isinstance(content, str):
        return content.strip()
    if not isinstance(content, list):
        return ""
    parts: list[str] = []
    for item in content:
        if not isinstance(item, dict):
            continue
        text = item.get("text")
        if isinstance(text, str) and text.strip():
            parts.append(text.strip())
    return "\n".join(parts).strip()


def transcript_events(transcript_path: str, session_id: str, project_id: str) -> list[dict]:
    if not transcript_path:
        return []

    path = pathlib.Path(transcript_path)
    if not path.exists() or not path.is_file():
        return []

    call_metadata: dict[str, dict[str, str]] = {}
    events: list[dict] = []

    try:
        lines = path.read_text(encoding="utf-8").splitlines()
    except OSError:
        return []

    for raw_line in lines:
        if not raw_line.strip():
            continue
        try:
            record = json.loads(raw_line)
        except json.JSONDecodeError:
            continue

        timestamp = record.get("timestamp") or now_rfc3339()
        record_type = record.get("type")
        payload = record.get("payload")
        if record_type != "response_item" or not isinstance(payload, dict):
            continue

        payload_type = payload.get("type")

        if payload_type == "function_call":
            tool_name = str(payload.get("name") or "")
            tool_input = str(payload.get("arguments") or "")
            call_id = payload.get("call_id")
            events.append(
                {
                    "kind": "tool_call",
                    "source_system": "codex",
                    "session_id": session_id,
                    "project_id": project_id,
                    "actor_type": "tool",
                    "content": f"{tool_name}: {tool_input}",
                    "metadata": {
                        "hook_event": "StopTranscriptImport",
                        "tool_name": tool_name,
                        "tool_input": tool_input,
                        "call_id": call_id,
                        "transcript_path": transcript_path,
                    },
                    "occurred_at": timestamp,
                }
            )
            if isinstance(call_id, str) and call_id:
                call_metadata[call_id] = {
                    "tool_name": tool_name,
                    "tool_input": tool_input,
                }
            continue

        if payload_type == "function_call_output":
            output = payload.get("output")
            if not isinstance(output, str) or not output.strip():
                continue
            call_id = payload.get("call_id")
            metadata = call_metadata.get(call_id if isinstance(call_id, str) else "", {})
            events.append(
                {
                    "kind": "tool_result",
                    "source_system": "codex",
                    "session_id": session_id,
                    "project_id": project_id,
                    "actor_type": "tool",
                    "content": output,
                    "metadata": {
                        "hook_event": "StopTranscriptImport",
                        "tool_name": metadata.get("tool_name", ""),
                        "tool_input": metadata.get("tool_input", ""),
                        "call_id": call_id,
                        "transcript_path": transcript_path,
                    },
                    "occurred_at": timestamp,
                }
            )
            continue

        if payload_type == "message" and payload.get("role") == "assistant":
            text = joined_message_text(payload.get("content"))
            if not text:
                continue
            events.append(
                {
                    "kind": "message_assistant",
                    "source_system": "codex",
                    "session_id": session_id,
                    "project_id": project_id,
                    "actor_type": "assistant",
                    "content": text,
                    "metadata": {
                        "hook_event": "StopTranscriptImport",
                        "phase": str(payload.get("phase") or ""),
                        "transcript_path": transcript_path,
                    },
                    "occurred_at": timestamp,
                }
            )

    return events


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

    imported_events = transcript_events(transcript_path, session_id, project_id)
    imported_count = 0
    for imported_event in imported_events:
        ingest_result = run_amm_json(["ingest", "event", "--in", "-"], json.dumps(imported_event))
        if ingest_result.get("ok", True) is not False or ingest_result.get("result"):
            imported_count += 1

    has_assistant_event = any(event.get("kind") == "message_assistant" for event in imported_events)
    if last_assistant_message and not has_assistant_event:
        run_amm(
            ["ingest", "event", "--in", "-"],
            json.dumps(
                {
                    "kind": "message_assistant",
                    "source_system": "codex",
                    "session_id": session_id,
                    "project_id": project_id,
                    "actor_type": "assistant",
                    "content": last_assistant_message,
                    "metadata": {
                        "hook_event": "StopFallbackAssistantMessage",
                        "turn_id": turn_id,
                        "cwd": cwd,
                        "transcript_path": transcript_path,
                        "model": model,
                        "permission_mode": permission_mode,
                    },
                    "occurred_at": now_rfc3339(),
                }
            ),
        )

    event = {
        "kind": "session_stop",
        "source_system": "codex",
        "session_id": session_id,
        "project_id": project_id,
        "content": f"Codex session stopped in {cwd or 'unknown cwd'}.",
        "metadata": {
            "hook_event": "Stop",
            "turn_id": turn_id,
            "cwd": cwd,
            "transcript_path": transcript_path,
            "model": model,
            "permission_mode": permission_mode,
            "stop_hook_active": str(stop_hook_active),
            "transcript_event_count": str(imported_count),
            "fallback_assistant_message_used": str(bool(last_assistant_message and not has_assistant_event)).lower(),
        },
        "occurred_at": now_rfc3339(),
    }
    run_amm(["ingest", "event", "--in", "-"], json.dumps(event))

    for job in ["reflect", "rebuild_indexes", "compress_history", "consolidate_sessions"]:
        run_amm(["jobs", "run", job])

    print(json.dumps({"systemMessage": "amm recorded Codex session stop and ran maintenance jobs."}))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
