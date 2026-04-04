"""Hermes plugin example for AMM ambient recall, event capture, and optional curated-memory parity."""

from __future__ import annotations

import hashlib
import json
import os
import subprocess
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from typing import Any
from urllib import error as urlerror
from urllib import request as urlrequest

_AMM_BIN = "AMM_BIN"
_AMM_DB_PATH = "AMM_DB_PATH"
_AMM_PROJECT_ID = "AMM_PROJECT_ID"
_AMM_CURATED_PROJECT_ID = "AMM_HERMES_CURATED_PROJECT_ID"
_AMM_RECALL_LIMIT = "AMM_HERMES_RECALL_LIMIT"
_AMM_API_URL = "AMM_API_URL"
_AMM_API_KEY = "AMM_API_KEY"

_AMM_SYNC_CURATED_MEMORY = "AMM_HERMES_SYNC_CURATED_MEMORY"
_AMM_SYNC_STATE_DIR = "AMM_HERMES_STATE_DIR"
_AMM_SYNC_MEMORY_SCOPE = "AMM_HERMES_MEMORY_SCOPE"
_AMM_SYNC_USER_SCOPE = "AMM_HERMES_USER_SCOPE"
_AMM_SYNC_MEMORY_TYPE = "AMM_HERMES_MEMORY_TYPE"
_AMM_SYNC_USER_TYPE = "AMM_HERMES_USER_TYPE"

_DEFAULT_AMM_BIN = "/usr/local/bin/amm"
_DEFAULT_RECALL_LIMIT = 5
_SOURCE_SYSTEM = "hermes-agent"
_MAX_TIGHT_DESCRIPTION = 120
_MAX_QUEUE_LINES = 500


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


def _env_bool(name: str, default: bool = False) -> bool:
    raw = os.environ.get(name)
    if raw is None:
        return default
    return raw.strip().lower() in {"1", "true", "yes", "on"}


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


def _resolve_curated_project_id(platform: str) -> str:
    explicit = os.environ.get(_AMM_CURATED_PROJECT_ID, "").strip()
    if explicit:
        return explicit
    return _resolve_project_id(platform)


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


def _http_headers() -> dict[str, str]:
    headers = {"Content-Type": "application/json"}
    api_key = os.environ.get(_AMM_API_KEY, "").strip()
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"
    return headers


def _request_json(
    method: str,
    path: str,
    payload: dict[str, Any] | None = None,
    timeout: int = 10,
) -> dict[str, Any]:
    base_url = _api_base_url()
    if not base_url:
        return {}

    body = None
    if payload is not None:
        body = json.dumps(payload).encode("utf-8")

    req = urlrequest.Request(
        f"{base_url}{path}",
        data=body,
        headers=_http_headers(),
        method=method,
    )

    try:
        with urlrequest.urlopen(req, timeout=timeout) as response:
            status_code = getattr(response, "status", 200)
            raw = response.read().decode("utf-8")
    except (urlerror.URLError, TimeoutError, ValueError):
        return {}

    if not raw:
        return {"status": status_code}

    try:
        data = json.loads(raw)
    except json.JSONDecodeError:
        return {"status": status_code}

    if isinstance(data, dict) and "status" not in data:
        data["status"] = status_code
    return data


def _post_json(path: str, payload: dict[str, Any], timeout: int = 10) -> dict[str, Any]:
    return _request_json("POST", path, payload, timeout=timeout)


def _ingest_event(event: dict[str, Any]) -> None:
    if _use_http_api():
        _post_json("/events", event, timeout=5)
        return
    _run_amm(["ingest", "event", "--in", "-"], stdin=json.dumps(event), timeout=5)


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
        "metadata": {k: str(v) for k, v in metadata.items()} if isinstance(metadata, dict) else {},
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


def _curated_sync_enabled() -> bool:
    return _env_bool(_AMM_SYNC_CURATED_MEMORY, default=False)


def _hermes_home() -> Path:
    return Path(os.environ.get("HERMES_HOME", str(Path.home() / ".hermes")))


def _state_dir() -> Path:
    raw = os.environ.get(_AMM_SYNC_STATE_DIR, "").strip()
    if raw:
        return Path(raw).expanduser()
    return _hermes_home() / "state" / "amm-memory"


def _map_path() -> Path:
    return _state_dir() / "curated-memory-map.json"


def _queue_path() -> Path:
    return _state_dir() / "curated-memory-sync-queue.jsonl"


def _atomic_write(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp_path = tempfile.mkstemp(dir=str(path.parent), prefix=path.name + ".", suffix=".tmp")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as handle:
            handle.write(content)
            handle.flush()
            os.fsync(handle.fileno())
        os.replace(tmp_path, path)
    finally:
        if os.path.exists(tmp_path):
            try:
                os.unlink(tmp_path)
            except OSError:
                pass


def _load_sync_state() -> dict[str, Any]:
    path = _map_path()
    if not path.exists():
        return {"records": []}
    try:
        data = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {"records": []}
    if not isinstance(data, dict):
        return {"records": []}
    records = data.get("records")
    if not isinstance(records, list):
        data["records"] = []
    return data


def _save_sync_state(state: dict[str, Any]) -> None:
    _atomic_write(_map_path(), json.dumps(state, ensure_ascii=False, indent=2, sort_keys=True) + "\n")


def _append_sync_queue(operation: dict[str, Any]) -> None:
    path = _queue_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    line = json.dumps(operation, ensure_ascii=False, sort_keys=True)
    lines: list[str] = []
    if path.exists():
        try:
            lines = path.read_text(encoding="utf-8").splitlines()
        except OSError:
            lines = []
    lines.append(line)
    if len(lines) > _MAX_QUEUE_LINES:
        lines = lines[-_MAX_QUEUE_LINES:]
    _atomic_write(path, "\n".join(lines) + "\n")


def _record_fingerprint(target: str, content: str, scope: str, project_id: str) -> str:
    digest = hashlib.sha256()
    digest.update(target.encode("utf-8"))
    digest.update(b"\n")
    digest.update(scope.encode("utf-8"))
    digest.update(b"\n")
    digest.update((project_id if scope == "project" else "").encode("utf-8"))
    digest.update(b"\n")
    digest.update(content.encode("utf-8"))
    return digest.hexdigest()


def _normalize_text(value: str) -> str:
    return "\n".join(line.rstrip() for line in value.strip().splitlines()).strip()


def _tight_description(content: str) -> str:
    lines = [line.strip() for line in content.splitlines() if line.strip()]
    candidate = lines[0] if lines else content.strip()
    candidate = " ".join(candidate.split())
    if len(candidate) <= _MAX_TIGHT_DESCRIPTION:
        return candidate
    return candidate[: _MAX_TIGHT_DESCRIPTION - 1].rstrip() + "…"


def _curated_scope(target: str, project_id: str) -> str:
    if target == "user":
        configured = os.environ.get(_AMM_SYNC_USER_SCOPE, "global").strip().lower()
    else:
        configured = os.environ.get(_AMM_SYNC_MEMORY_SCOPE, "project").strip().lower()
    if configured not in {"global", "project", "session"}:
        configured = "global" if target == "user" else "project"
    if configured == "project" and not project_id:
        return "global"
    return configured


def _curated_type(target: str) -> str:
    if target == "user":
        configured = os.environ.get(_AMM_SYNC_USER_TYPE, "preference").strip().lower()
    else:
        configured = os.environ.get(_AMM_SYNC_MEMORY_TYPE, "fact").strip().lower()
    return configured or ("preference" if target == "user" else "fact")


def _curated_subject(target: str) -> str:
    return "hermes_curated_user" if target == "user" else "hermes_curated_memory"


def _extract_memory_id(result: dict[str, Any]) -> str:
    if not isinstance(result, dict):
        return ""
    direct = result.get("id")
    if isinstance(direct, str) and direct:
        return direct
    nested = result.get("result")
    if isinstance(nested, dict):
        nested_id = nested.get("id")
        if isinstance(nested_id, str) and nested_id:
            return nested_id
    data = result.get("data")
    if isinstance(data, dict):
        data_id = data.get("id")
        if isinstance(data_id, str) and data_id:
            return data_id
    return ""


def _remember_memory(target: str, content: str, project_id: str) -> str:
    scope = _curated_scope(target, project_id)
    payload = {
        "type": _curated_type(target),
        "scope": scope,
        "body": content,
        "tight_description": _tight_description(content),
        "subject": _curated_subject(target),
    }
    if scope == "project" and project_id:
        payload["project_id"] = project_id

    if _use_http_api():
        return _extract_memory_id(_request_json("POST", "/memories", payload, timeout=10))

    command = [
        "remember",
        "--type",
        payload["type"],
        "--scope",
        payload["scope"],
        "--body",
        payload["body"],
        "--tight",
        payload["tight_description"],
        "--subject",
        payload["subject"],
    ]
    if payload.get("project_id"):
        command.extend(["--project", payload["project_id"]])
    return _extract_memory_id(_run_amm(command, timeout=10))


def _update_memory(memory_id: str, target: str, content: str, project_id: str) -> bool:
    scope = _curated_scope(target, project_id)
    payload = {
        "body": content,
        "tight_description": _tight_description(content),
        "type": _curated_type(target),
        "scope": scope,
        "status": "active",
    }

    if _use_http_api():
        result = _request_json("PATCH", f"/memories/{memory_id}", payload, timeout=10)
        return bool(result) and int(result.get("status", 0)) < 400

    command = [
        "memory",
        "update",
        memory_id,
        "--body",
        payload["body"],
        "--tight",
        payload["tight_description"],
        "--type",
        payload["type"],
        "--scope",
        payload["scope"],
        "--status",
        payload["status"],
    ]
    return bool(_run_amm(command, timeout=10))


def _forget_memory(memory_id: str) -> bool:
    if _use_http_api():
        result = _request_json("DELETE", f"/memories/{memory_id}", timeout=10)
        return bool(result) and int(result.get("status", 0)) < 400
    return bool(_run_amm(["forget", memory_id], timeout=10))


def _record_index(
    records: list[dict[str, Any]],
    target: str,
    old_text: str,
    scope: str,
    project_id: str,
) -> int | None:
    normalized_old_text = _normalize_text(old_text)
    target_matches = [
        (idx, record)
        for idx, record in enumerate(records)
        if record.get("target") == target
    ]
    scoped_matches = [
        (idx, record)
        for idx, record in target_matches
        if record.get("scope") == scope
        and (scope != "project" or record.get("project_id", "") == project_id)
    ]

    def _exact(matches: list[tuple[int, dict[str, Any]]]) -> list[int]:
        return [
            idx for idx, record in matches
            if _normalize_text(str(record.get("content", ""))) == normalized_old_text
        ]

    def _substring(matches: list[tuple[int, dict[str, Any]]]) -> list[int]:
        return [
            idx for idx, record in matches
            if normalized_old_text and normalized_old_text in _normalize_text(str(record.get("content", "")))
        ]

    exact_matches = _exact(scoped_matches)
    if len(exact_matches) == 1:
        return exact_matches[0]
    if len(exact_matches) > 1:
        return None

    fallback_exact_matches = _exact(target_matches)
    if len(fallback_exact_matches) == 1:
        return fallback_exact_matches[0]
    if len(fallback_exact_matches) > 1:
        return None

    substring_matches = _substring(scoped_matches)
    if len(substring_matches) == 1:
        return substring_matches[0]
    if len(substring_matches) > 1:
        return None

    fallback_substring_matches = _substring(target_matches)
    if len(fallback_substring_matches) == 1:
        return fallback_substring_matches[0]
    return None


def _queue_failed_sync(action: str, target: str, project_id: str, details: dict[str, Any], error: str) -> None:
    _append_sync_queue(
        {
            "occurred_at": _now_rfc3339(),
            "source_system": _SOURCE_SYSTEM,
            "kind": "curated_memory_sync_failed",
            "action": action,
            "target": target,
            "project_id": project_id,
            "error": error,
            "details": details,
        }
    )


def _sync_curated_memory(args: dict[str, Any], result: Any) -> None:
    if not _curated_sync_enabled():
        return
    if not isinstance(args, dict) or not isinstance(result, str):
        return

    try:
        payload = json.loads(result)
    except json.JSONDecodeError:
        return
    if not payload.get("success"):
        return

    action = str(args.get("action", "")).strip().lower()
    target = str(args.get("target", "memory")).strip().lower()
    if action not in {"add", "replace", "remove"} or target not in {"memory", "user"}:
        return

    platform = str(args.get("platform", "cli") or "cli")
    project_id = _resolve_curated_project_id(platform)
    scope = _curated_scope(target, project_id)
    effective_project_id = project_id if scope == "project" else ""
    state = _load_sync_state()
    records = [record for record in state.get("records", []) if isinstance(record, dict)]

    if action == "add":
        content = _normalize_text(_stringify_content(args.get("content", "")))
        if not content:
            return
        fingerprint = _record_fingerprint(target, content, scope, effective_project_id)
        if any(record.get("fingerprint") == fingerprint for record in records):
            return
        memory_id = _remember_memory(target, content, project_id)
        if not memory_id:
            _queue_failed_sync(action, target, project_id, {"content": content, "scope": scope}, "create_failed")
            return
        records.append(
            {
                "target": target,
                "scope": scope,
                "content": content,
                "fingerprint": fingerprint,
                "amm_memory_id": memory_id,
                "project_id": effective_project_id,
                "updated_at": _now_rfc3339(),
            }
        )
        state["records"] = records
        _save_sync_state(state)
        return

    old_text = _stringify_content(args.get("old_text", "")).strip()
    if not old_text:
        return

    record_idx = _record_index(records, target, old_text, scope, effective_project_id)
    if record_idx is None:
        details = {"old_text": old_text, "scope": scope}
        if effective_project_id:
            details["project_id"] = effective_project_id
        if action == "replace":
            details["content"] = _normalize_text(_stringify_content(args.get("content", "")))
        _queue_failed_sync(action, target, project_id, details, "record_not_found_or_ambiguous")
        return

    record = records[record_idx]
    memory_id = str(record.get("amm_memory_id", "")).strip()
    if not memory_id:
        _queue_failed_sync(action, target, project_id, {"old_text": old_text}, "missing_memory_id")
        return

    if action == "remove":
        if not _forget_memory(memory_id):
            _queue_failed_sync(action, target, project_id, {"old_text": old_text, "amm_memory_id": memory_id}, "delete_failed")
            return
        records.pop(record_idx)
        state["records"] = records
        _save_sync_state(state)
        return

    new_content = _normalize_text(_stringify_content(args.get("content", "")))
    if not new_content:
        return
    if not _update_memory(memory_id, target, new_content, project_id):
        _queue_failed_sync(
            action,
            target,
            project_id,
            {"old_text": old_text, "content": new_content, "amm_memory_id": memory_id},
            "update_failed",
        )
        return

    records[record_idx] = {
        **record,
        "scope": scope,
        "content": new_content,
        "fingerprint": _record_fingerprint(target, new_content, scope, effective_project_id),
        "project_id": effective_project_id,
        "updated_at": _now_rfc3339(),
    }
    state["records"] = records
    _save_sync_state(state)


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


def _post_tool_call(
    *,
    tool_name: str,
    args: dict[str, Any],
    result: Any,
    task_id: str,
    **_: Any,
) -> None:
    if tool_name != "memory":
        return
    _sync_curated_memory(args or {}, result)


def register(ctx: Any) -> None:
    ctx.register_hook("pre_llm_call", _pre_llm_call)
    ctx.register_hook("post_llm_call", _post_llm_call)
    ctx.register_hook("post_tool_call", _post_tool_call)
