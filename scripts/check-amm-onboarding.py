#!/usr/bin/env python3

from __future__ import annotations

import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent
STALE_PATTERN = re.compile(r"\bacm memory\b|/acm-memory")
TEXT_SUFFIXES = {".md", ".json", ".toml", ".sh", ".py"}
SKIP_PARTS = {"node_modules", "__pycache__", ".git"}


def iter_targets() -> list[Path]:
    targets: list[Path] = [
        ROOT / "AGENTS.md",
        ROOT / "CLAUDE.md",
        ROOT / "CODEX.md",
        ROOT / "README.md",
        ROOT / "docs" / "agent-onboarding.md",
        ROOT / "docs" / "integration.md",
        ROOT / "docs" / "codex-integration.md",
        ROOT / "docs" / "opencode-integration.md",
    ]

    for directory in [ROOT / ".claude", ROOT / ".codex", ROOT / ".opencode", ROOT / "examples"]:
        if not directory.exists():
            continue
        for path in directory.rglob("*"):
            if not path.is_file() or path.suffix not in TEXT_SUFFIXES:
                continue
            if any(part in SKIP_PARTS for part in path.parts):
                continue
            targets.append(path)

    seen: set[Path] = set()
    ordered: list[Path] = []
    for path in targets:
        if path.exists() and path not in seen:
            seen.add(path)
            ordered.append(path)
    return ordered


def main() -> int:
    failures: list[str] = []

    for path in iter_targets():
        text = path.read_text(encoding="utf-8")
        for line_no, line in enumerate(text.splitlines(), start=1):
            if STALE_PATTERN.search(line):
                failures.append(f"{path.relative_to(ROOT)}:{line_no}: {line.strip()}")

    if failures:
        print("Active onboarding surfaces still reference removed ACM memory commands:", file=sys.stderr)
        for failure in failures:
            print(f"- {failure}", file=sys.stderr)
        return 1

    print("AMM onboarding surfaces are clean.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
