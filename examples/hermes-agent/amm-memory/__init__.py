"""Hermes plugin example for AMM ambient recall and event capture."""

from __future__ import annotations

import json
import os
import subprocess
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
from urllib import error as urlerror
from urllib import request as urlrequest

_AMM_BIN = "AMM_BIN"
_AMM_DB_PATH = "AMM_DB_PATH"
_AMM_PROJECT_ID = "AMM_PROJECT_ID"
_AMM_RECALL_LIMIT = "AMM_HERMES_RECALL_LIMIT"
_AMM_API_URL = "AMM_API_URL"
_AMM_API_KEY = "AMM_API_KEY"

_DEFAULT_AMM_BIN = "/usr/local/bin/amm"
_DEFAULT_RECALL_LIMIT = 5
_SOURCE_SYSTEM = "hermes-agent"


def _now_rfc3339() -> str:
    return (
        datetime.now(timezone.utc)
        .replace(microsecond=0)
        .isoformat()
        .replace("+00:00", "Z")
    )


def _amm_bin() -> str:
    return os.environ.get(_AMM_BIN, _DEFAULT_AMM_BIN)


def _amm_db_path() -> str:
    return os.environ.get(
        _AMM_DB_PATH,
        str(Path.home() / ".amm" / "amm.db"),
    )


def _api_base_url() -> str:
    raw = os.environ.get(_AMM_API_URL, "").strip().rstrip("/")
    if not raw:
        return ""
    return raw if raw.endswith("/v1") else f"{raw}/v1"


def _use_http_api() -> bool:
    return bool(_api_base_url())


def _recall_limit() -> int:
    raw = os.environ.get(_AMM_RECALL_LIMIT, "")
    try:
        value = int(raw)
    except (TypeError, ValueError):
        return _DEFAULT_RECALL_LIMIT
    return max(1, value)


def _stringify_content(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, str):
        return value
    try:
        return json.dumps(value, ensure_ascii=False)
    except (TypeError, ValueError):
        return str(value)


def _resolve_project_id(platform: str) -> str:
    explicit = os.environ.get(_AMM_PROJECT_ID, "").strip()
    if explicit:
        return explicit

    cwd = os.environ.get("TERMINAL_CWD", "").strip()
    if not cwd and platform == "cli":
        cwd = os.environ.get("PWD", "").strip()
    if not cwd and platform == "cli":
        try:
            cwd = os.getcwd()
        except OSError:
            cwd = ""
    if not cwd:
        return ""

    try:
        return Path(cwd).expanduser().resolve().name
    except OSError:
        return Path(cwd).expanduser().name


def _run_amm(command: list[str], stdin: str | None = None, timeout: int = 10) -> dict[str, Any]:
    try:
        proc = subprocess.run(
            [_amm_bin(), *command],
            input=stdin,
            text=True,
            capture_output=True,
            env={**os.environ, _AMM_DB_PATH: _amm_db_path()},
            check=False,
            timeout=timeout,
        )
    except (FileNotFoundError, subprocess.SubprocessError):
        return {}

    if proc.returncode != 0 or not proc.stdout:
        return {}

    try:
        return json.loads(proc.stdout)
    except json.JSONDecodeError:
        return {}


def _ingest_event(event: dict[str, Any]) -> None:
    if _use_http_api():
        _post_json("/events", event, timeout=5)
        return
    _run_amm(["ingest", "event", "--in", "-"], stdin=json.dumps(event), timeout=5)


def _http_headers() -> dict[str, str]:
    headers = {"Content-Type": "application/json"}
    api_key = os.environ.get(_AMM_API_KEY, "").strip()
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"
    return headers


def _post_json(path: str, payload: dict[str, Any], timeout: int = 10) -> dict[str, Any]:
    base_url = _api_base_url()
    if not base_url:
        return {}

    body = json.dumps(payload).encode("utf-8")
    req = urlrequest.Request(
        f"{base_url}{path}",
        data=body,
        headers=_http_headers(),
        method="POST",
    )

    try:
        with urlrequest.urlopen(req, timeout=timeout) as response:
            raw = response.read().decode("utf-8")
    except (urlerror.URLError, TimeoutError, ValueError):
        return {}

    if not raw:
        return {}

    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        return {}


def _build_event(
    *,
    kind: str,
    actor_type: str,
    session_id: str,
    project_id: str,
    content: str,
    metadata: dict[str, Any],
) -> dict[str, Any]:
    return {
        "kind": kind,
        "source_system": _SOURCE_SYSTEM,
        "session_id": session_id,
        "project_id": project_id,
        "actor_type": actor_type,
        "content": content,
        "metadata": json.dumps(metadata) if isinstance(metadata, dict) else str(metadata),
        "occurred_at": _now_rfc3339(),
    }


def _render_recall(recall_result: dict[str, Any]) -> str | None:
    items = []
    if "result" in recall_result:
        items = recall_result.get("result", {}).get("items", [])
    elif "data" in recall_result:
        items = recall_result.get("data", {}).get("items", [])
    if not items:
        return None

    lines = ["amm ambient recall:"]
    for item in items[: _recall_limit()]:
        kind = item.get("kind", "item")
        desc = item.get("tight_description", "")
        score = item.get("score", 0)
        if not desc:
            continue
        try:
            score_text = f"{float(score):.2f}"
        except (TypeError, ValueError):
            score_text = "0.00"
        lines.append(f"- [{kind}] {desc} (score: {score_text})")

    return "\n".join(lines) if len(lines) > 1 else None


def _pre_llm_call(
    *,
    session_id: str,
    user_message: Any,
    conversation_history: list[dict[str, Any]],
    is_first_turn: bool,
    model: str,
    platform: str,
    **_: Any,
) -> dict[str, str] | None:
    prompt = _stringify_content(user_message)
    if not prompt.strip():
        return None

    project_id = _resolve_project_id(platform)
    _ingest_event(
        _build_event(
            kind="message_user",
            actor_type="user",
            session_id=session_id,
            project_id=project_id,
            content=prompt,
            metadata={
                "hook_event": "pre_llm_call",
                "is_first_turn": is_first_turn,
                "model": model,
                "platform": platform,
                "conversation_length": len(conversation_history),
            },
        )
    )

    command = ["recall", "--mode", "ambient", "--session", session_id]
    if _use_http_api():
        recall = _post_json(
            "/recall",
            {
                "query": prompt,
                "opts": {
                    "mode": "ambient",
                    "limit": _recall_limit(),
                    "session_id": session_id,
                    "project_id": project_id,
                },
            },
        )
    else:
        if project_id:
            command.extend(["--project", project_id])
        command.append(prompt)
        recall = _run_amm(command)
    context = _render_recall(recall)
    return {"context": context} if context else None


def _post_llm_call(
    *,
    session_id: str,
    assistant_response: Any,
    conversation_history: list[dict[str, Any]],
    model: str,
    platform: str,
    **_: Any,
) -> None:
    response = _stringify_content(assistant_response)
    if not response.strip():
        return

    project_id = _resolve_project_id(platform)
    _ingest_event(
        _build_event(
            kind="message_assistant",
            actor_type="assistant",
            session_id=session_id,
            project_id=project_id,
            content=response,
            metadata={
                "hook_event": "post_llm_call",
                "model": model,
                "platform": platform,
                "conversation_length": len(conversation_history),
            },
        )
    )


def register(ctx: Any) -> None:
    ctx.register_hook("pre_llm_call", _pre_llm_call)
    ctx.register_hook("post_llm_call", _post_llm_call)
