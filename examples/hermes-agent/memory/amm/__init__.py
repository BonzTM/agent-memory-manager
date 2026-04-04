"""AMM memory provider example for Hermes external-memory architecture.

Provides ambient recall injection, turn sync into AMM events, and best-effort
mirroring of Hermes built-in curated memory into AMM durable memories.

This provider is designed for Hermes' MemoryProvider interface. It keeps the
legacy hook plugin as a fallback for older Hermes releases, but new Hermes
installs should prefer this provider shape.
"""

from __future__ import annotations

import fcntl
import hashlib
import json
import logging
import os
import shutil
import subprocess
import tempfile
import threading
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict, List
from urllib import error as urlerror
from urllib import request as urlrequest

from agent.memory_provider import MemoryProvider

logger = logging.getLogger(__name__)

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
_ENTRY_DELIMITER = "\n§\n"


class AMMMemoryProvider(MemoryProvider):
    """AMM-backed Hermes memory provider example."""

    def __init__(self) -> None:
        self._session_id = ""
        self._hermes_home = Path.home() / ".hermes"
        self._platform = "cli"
        self._active = False
        self._sync_thread: threading.Thread | None = None
        self._snapshot: Dict[str, List[str]] = {"memory": [], "user": []}

    @property
    def name(self) -> str:
        return "amm"

    def is_available(self) -> bool:
        api_url = os.environ.get(_AMM_API_URL, "").strip()
        if api_url:
            return True
        amm_bin = os.environ.get(_AMM_BIN, _DEFAULT_AMM_BIN).strip() or _DEFAULT_AMM_BIN
        return Path(amm_bin).exists() or shutil.which(amm_bin) is not None

    def initialize(self, session_id: str, **kwargs) -> None:
        self._session_id = session_id
        self._platform = str(kwargs.get("platform", "cli") or "cli")
        self._hermes_home = Path(str(kwargs.get("hermes_home") or (Path.home() / ".hermes"))).expanduser()
        agent_context = str(kwargs.get("agent_context", "") or "")
        if agent_context in {"cron", "flush", "subagent"} or self._platform == "cron":
            self._active = False
            return
        self._active = self.is_available()
        self._snapshot = {
            "memory": self._read_curated_entries("memory"),
            "user": self._read_curated_entries("user"),
        }

    def system_prompt_block(self) -> str:
        return ""

    def prefetch(self, query: str, *, session_id: str = "") -> str:
        if not self._active or not query.strip():
            return ""
        active_session = session_id or self._session_id
        project_id = self._resolve_project_id(self._platform)
        if self._use_http_api():
            recall = self._post_json(
                "/recall",
                {
                    "query": query,
                    "opts": {
                        "mode": "ambient",
                        "limit": self._recall_limit(),
                        "session_id": active_session,
                        "project_id": project_id,
                    },
                },
            )
        else:
            command = ["recall", "--mode", "ambient", "--session", active_session]
            if project_id:
                command.extend(["--project", project_id])
            command.append(query)
            recall = self._run_amm(command)
        return self._render_recall(recall)

    def queue_prefetch(self, query: str, *, session_id: str = "") -> None:
        return None

    def sync_turn(self, user_content: str, assistant_content: str, *, session_id: str = "") -> None:
        if not self._active:
            return
        active_session = session_id or self._session_id
        project_id = self._resolve_project_id(self._platform)

        def _sync() -> None:
            self._ingest_event(
                self._build_event(
                    kind="message_user",
                    actor_type="user",
                    session_id=active_session,
                    project_id=project_id,
                    content=user_content,
                    metadata={"source": "memory_provider.sync_turn"},
                )
            )
            self._ingest_event(
                self._build_event(
                    kind="message_assistant",
                    actor_type="assistant",
                    session_id=active_session,
                    project_id=project_id,
                    content=assistant_content,
                    metadata={"source": "memory_provider.sync_turn"},
                )
            )

        if self._sync_thread and self._sync_thread.is_alive():
            self._sync_thread.join(timeout=5.0)
        self._sync_thread = threading.Thread(target=_sync, daemon=True, name="amm-sync-turn")
        self._sync_thread.start()

    def get_tool_schemas(self) -> List[Dict[str, Any]]:
        return []

    def handle_tool_call(self, tool_name: str, args: Dict[str, Any], **kwargs) -> str:
        return json.dumps({"error": f"AMM memory provider exposes no tools ({tool_name})"})

    def get_config_schema(self) -> List[Dict[str, Any]]:
        return []

    def save_config(self, values: Dict[str, Any], hermes_home: str) -> None:
        return None

    def on_memory_write(self, action: str, target: str, content: str) -> None:
        if not self._active:
            return
        if not self._env_bool(_AMM_SYNC_CURATED_MEMORY, default=False):
            return
        if action not in {"add", "replace"}:
            return
        if target not in {"memory", "user"}:
            return
        self._reconcile_curated_target(target)

    def on_session_end(self, messages: List[Dict[str, Any]]) -> None:
        if not self._active:
            return
        if self._sync_thread and self._sync_thread.is_alive():
            self._sync_thread.join(timeout=10.0)
        if self._env_bool(_AMM_SYNC_CURATED_MEMORY, default=False):
            self._reconcile_curated_target("memory")
            self._reconcile_curated_target("user")

    def shutdown(self) -> None:
        if self._sync_thread and self._sync_thread.is_alive():
            self._sync_thread.join(timeout=10.0)

    # --- transport helpers -------------------------------------------------

    def _now_rfc3339(self) -> str:
        return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")

    def _amm_bin(self) -> str:
        return os.environ.get(_AMM_BIN, _DEFAULT_AMM_BIN)

    def _amm_db_path(self) -> str:
        return os.environ.get(_AMM_DB_PATH, str(Path.home() / ".amm" / "amm.db"))

    def _api_base_url(self) -> str:
        raw = os.environ.get(_AMM_API_URL, "").strip().rstrip("/")
        if not raw:
            return ""
        return raw if raw.endswith("/v1") else f"{raw}/v1"

    def _use_http_api(self) -> bool:
        return bool(self._api_base_url())

    def _http_headers(self) -> Dict[str, str]:
        headers = {"Content-Type": "application/json"}
        api_key = os.environ.get(_AMM_API_KEY, "").strip()
        if api_key:
            headers["Authorization"] = f"Bearer {api_key}"
        return headers

    def _request_json(self, method: str, path: str, payload: Dict[str, Any] | None = None, timeout: int = 10) -> Dict[str, Any]:
        base_url = self._api_base_url()
        if not base_url:
            return {}
        body = json.dumps(payload).encode("utf-8") if payload is not None else None
        req = urlrequest.Request(f"{base_url}{path}", data=body, headers=self._http_headers(), method=method)
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
        if isinstance(data, dict):
            data.setdefault("status", status_code)
            return data
        return {"status": status_code, "result": data}

    def _post_json(self, path: str, payload: Dict[str, Any], timeout: int = 10) -> Dict[str, Any]:
        return self._request_json("POST", path, payload, timeout=timeout)

    def _run_amm(self, command: List[str], stdin: str | None = None, timeout: int = 10) -> Dict[str, Any]:
        try:
            proc = subprocess.run(
                [self._amm_bin(), *command],
                input=stdin,
                text=True,
                capture_output=True,
                env={**os.environ, _AMM_DB_PATH: self._amm_db_path()},
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

    # --- event sync --------------------------------------------------------

    def _build_event(self, *, kind: str, actor_type: str, session_id: str, project_id: str, content: str, metadata: Dict[str, Any] | None = None) -> Dict[str, Any]:
        event: Dict[str, Any] = {
            "source_system": _SOURCE_SYSTEM,
            "kind": kind,
            "surface": "hermes-agent",
            "agent_id": actor_type,
            "session_id": session_id,
            "content": content,
            "occurred_at": self._now_rfc3339(),
        }
        if project_id:
            event["project_id"] = project_id
        if metadata:
            event["metadata"] = metadata
        return event

    def _ingest_event(self, event: Dict[str, Any]) -> None:
        if self._use_http_api():
            self._post_json("/events", event, timeout=10)
            return
        command = [
            "ingest",
            "--kind", str(event.get("kind", "message_unknown")),
            "--source", _SOURCE_SYSTEM,
            "--surface", str(event.get("surface", "hermes-agent")),
            "--agent", str(event.get("agent_id", "agent")),
            "--occurred-at", str(event.get("occurred_at", self._now_rfc3339())),
            "--content-file",
        ]
        with tempfile.NamedTemporaryFile("w", delete=False, encoding="utf-8") as handle:
            handle.write(str(event.get("content", "")))
            content_path = handle.name
        command.append(content_path)
        if event.get("session_id"):
            command.extend(["--session", str(event["session_id"])])
        if event.get("project_id"):
            command.extend(["--project", str(event["project_id"])])
        if isinstance(event.get("metadata"), dict):
            command.extend(["--metadata", json.dumps(event["metadata"], ensure_ascii=False)])
        try:
            self._run_amm(command, timeout=10)
        finally:
            try:
                os.unlink(content_path)
            except OSError:
                pass

    def _render_recall(self, data: Dict[str, Any]) -> str:
        items = data.get("items") if isinstance(data, dict) else None
        if not isinstance(items, list) or not items:
            return ""
        lines = ["amm ambient recall:"]
        for item in items:
            if not isinstance(item, dict):
                continue
            kind = item.get("kind", "memory")
            text = item.get("tight_description") or item.get("body") or item.get("summary") or ""
            if not text:
                continue
            score = item.get("score")
            if score is None:
                lines.append(f"- [{kind}] {text}")
            else:
                try:
                    lines.append(f"- [{kind}] {text} (score: {float(score):.2f})")
                except (TypeError, ValueError):
                    lines.append(f"- [{kind}] {text}")
        return "\n".join(lines) if len(lines) > 1 else ""

    # --- project resolution -------------------------------------------------

    def _recall_limit(self) -> int:
        raw = os.environ.get(_AMM_RECALL_LIMIT, "")
        try:
            value = int(raw)
        except (TypeError, ValueError):
            return _DEFAULT_RECALL_LIMIT
        return max(1, value)

    def _resolve_project_id(self, platform: str) -> str:
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

    def _resolve_curated_project_id(self, platform: str) -> str:
        explicit = os.environ.get(_AMM_CURATED_PROJECT_ID, "").strip()
        if explicit:
            return explicit
        return self._resolve_project_id(platform)

    def _env_bool(self, name: str, default: bool = False) -> bool:
        raw = os.environ.get(name)
        if raw is None:
            return default
        return raw.strip().lower() in {"1", "true", "yes", "on"}

    # --- curated memory mirroring ------------------------------------------

    def _state_dir(self) -> Path:
        raw = os.environ.get(_AMM_SYNC_STATE_DIR, "").strip()
        if raw:
            return Path(raw).expanduser()
        return self._hermes_home / "state" / "amm-memory-provider"

    def _map_path(self) -> Path:
        return self._state_dir() / "curated-memory-id-map.json"

    def _queue_path(self) -> Path:
        return self._state_dir() / "curated-memory-sync-queue.jsonl"

    def _atomic_write(self, path: Path, content: str) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        with tempfile.NamedTemporaryFile("w", delete=False, dir=path.parent, encoding="utf-8") as handle:
            handle.write(content)
            temp_path = Path(handle.name)
        os.replace(temp_path, path)

    def _load_sync_state(self) -> Dict[str, Any]:
        path = self._map_path()
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

    def _save_sync_state(self, state: Dict[str, Any]) -> None:
        self._atomic_write(self._map_path(), json.dumps(state, ensure_ascii=False, indent=2, sort_keys=True) + "\n")

    def _append_sync_queue(self, operation: Dict[str, Any]) -> None:
        path = self._queue_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        lock_path = path.with_suffix(path.suffix + ".lock")
        line = json.dumps(operation, ensure_ascii=False, sort_keys=True)
        with lock_path.open("a+", encoding="utf-8") as lock_handle:
            fcntl.flock(lock_handle.fileno(), fcntl.LOCK_EX)
            lines: List[str] = []
            if path.exists():
                try:
                    lines = path.read_text(encoding="utf-8").splitlines()
                except (OSError, UnicodeDecodeError):
                    lines = []
            lines.append(line)
            if len(lines) > _MAX_QUEUE_LINES:
                lines = lines[-_MAX_QUEUE_LINES:]
            self._atomic_write(path, "\n".join(lines) + "\n")
            fcntl.flock(lock_handle.fileno(), fcntl.LOCK_UN)

    def _normalize_text(self, value: str) -> str:
        return "\n".join(line.rstrip() for line in value.strip().splitlines()).strip()

    def _record_fingerprint(self, target: str, content: str, scope: str, project_id: str) -> str:
        digest = hashlib.sha256()
        digest.update(target.encode("utf-8"))
        digest.update(b"\n")
        digest.update(scope.encode("utf-8"))
        digest.update(b"\n")
        digest.update((project_id if scope == "project" else "").encode("utf-8"))
        digest.update(b"\n")
        digest.update(content.encode("utf-8"))
        return digest.hexdigest()

    def _tight_description(self, content: str) -> str:
        lines = [line.strip() for line in content.splitlines() if line.strip()]
        candidate = lines[0] if lines else content.strip()
        candidate = " ".join(candidate.split())
        if len(candidate) <= _MAX_TIGHT_DESCRIPTION:
            return candidate
        return candidate[: _MAX_TIGHT_DESCRIPTION - 1].rstrip() + "…"

    def _curated_subject(self, target: str) -> str:
        return "Hermes user profile" if target == "user" else "Hermes memory"

    def _curated_scope(self, target: str, project_id: str) -> str:
        if target == "user":
            return os.environ.get(_AMM_SYNC_USER_SCOPE, "global").strip() or "global"
        scope = os.environ.get(_AMM_SYNC_MEMORY_SCOPE, "project").strip() or "project"
        if scope == "project" and not project_id:
            return "global"
        return scope

    def _curated_type(self, target: str) -> str:
        if target == "user":
            return os.environ.get(_AMM_SYNC_USER_TYPE, "preference").strip() or "preference"
        return os.environ.get(_AMM_SYNC_MEMORY_TYPE, "fact").strip() or "fact"

    def _status_code_lt_400(self, result: Dict[str, Any]) -> bool:
        if not result:
            return False
        try:
            return int(result.get("status", 0)) < 400
        except (TypeError, ValueError):
            return False

    def _extract_memory_id(self, result: Dict[str, Any]) -> str:
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

    def _remember_memory(self, target: str, content: str, project_id: str) -> str:
        scope = self._curated_scope(target, project_id)
        payload = {
            "type": self._curated_type(target),
            "scope": scope,
            "body": content,
            "tight_description": self._tight_description(content),
            "subject": self._curated_subject(target),
        }
        if scope == "project" and project_id:
            payload["project_id"] = project_id
        if self._use_http_api():
            return self._extract_memory_id(self._request_json("POST", "/memories", payload, timeout=10))
        command = [
            "remember",
            "--type", payload["type"],
            "--scope", payload["scope"],
            "--body", payload["body"],
            "--tight", payload["tight_description"],
            "--subject", payload["subject"],
        ]
        if payload.get("project_id"):
            command.extend(["--project", payload["project_id"]])
        return self._extract_memory_id(self._run_amm(command, timeout=10))

    def _forget_memory(self, memory_id: str) -> bool:
        if self._use_http_api():
            return self._status_code_lt_400(self._request_json("DELETE", f"/memories/{memory_id}", timeout=10))
        return bool(self._run_amm(["forget", memory_id], timeout=10))

    def _curated_file_path(self, target: str) -> Path:
        memories_dir = self._hermes_home / "memories"
        return memories_dir / ("USER.md" if target == "user" else "MEMORY.md")

    def _read_curated_entries(self, target: str) -> List[str]:
        path = self._curated_file_path(target)
        if not path.exists():
            return []
        try:
            raw = path.read_text(encoding="utf-8")
        except OSError:
            return []
        if not raw.strip():
            return []
        entries = [self._normalize_text(chunk) for chunk in raw.split(_ENTRY_DELIMITER)]
        return [entry for entry in entries if entry]

    def _queue_failed_sync(self, action: str, target: str, project_id: str, details: Dict[str, Any], error: str) -> None:
        self._append_sync_queue(
            {
                "occurred_at": self._now_rfc3339(),
                "source_system": _SOURCE_SYSTEM,
                "kind": "curated_memory_sync_failed",
                "action": action,
                "target": target,
                "project_id": project_id,
                "error": error,
                "details": details,
            }
        )

    def _reconcile_curated_target(self, target: str) -> None:
        current_entries = self._read_curated_entries(target)
        previous_entries = self._snapshot.get(target, [])
        state = self._load_sync_state()
        records = [record for record in state.get("records", []) if isinstance(record, dict)]

        project_id = self._resolve_curated_project_id(self._platform)
        scope = self._curated_scope(target, project_id)
        effective_project_id = project_id if scope == "project" else ""

        previous_set = set(previous_entries)
        current_set = set(current_entries)

        removed_entries = [entry for entry in previous_entries if entry not in current_set]
        added_entries = [entry for entry in current_entries if entry not in previous_set]

        for entry in removed_entries:
            matching_indexes = [
                idx for idx, record in enumerate(records)
                if record.get("target") == target and self._normalize_text(str(record.get("content", ""))) == entry
            ]
            if len(matching_indexes) != 1:
                continue
            record_idx = matching_indexes[0]
            record = records[record_idx]
            memory_id = str(record.get("amm_memory_id", "")).strip()
            if not memory_id:
                self._queue_failed_sync("remove", target, project_id, {"content": entry}, "missing_memory_id")
                continue
            if not self._forget_memory(memory_id):
                self._queue_failed_sync("remove", target, project_id, {"content": entry, "amm_memory_id": memory_id}, "delete_failed")
                continue
            records.pop(record_idx)

        for entry in added_entries:
            fingerprint = self._record_fingerprint(target, entry, scope, effective_project_id)
            if any(record.get("fingerprint") == fingerprint for record in records):
                continue
            memory_id = self._remember_memory(target, entry, project_id)
            if not memory_id:
                self._queue_failed_sync("add", target, project_id, {"content": entry, "scope": scope}, "create_failed")
                continue
            records.append(
                {
                    "target": target,
                    "scope": scope,
                    "content": entry,
                    "fingerprint": fingerprint,
                    "amm_memory_id": memory_id,
                    "project_id": effective_project_id,
                    "updated_at": self._now_rfc3339(),
                }
            )

        state["records"] = records
        self._save_sync_state(state)
        self._snapshot[target] = current_entries


def register(ctx) -> None:
    """Register AMM as a Hermes memory provider plugin."""
    ctx.register_memory_provider(AMMMemoryProvider())
