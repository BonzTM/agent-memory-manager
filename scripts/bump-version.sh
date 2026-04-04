#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: scripts/bump-version.sh <semver>

Examples:
  scripts/bump-version.sh 1.0.1
  scripts/bump-version.sh 1.1.0

Notes:
  - Use plain semver only (no leading 'v').
  - This updates release-version surfaces in the repo.
  - Release dates are derived from the current UTC date.
  - It does NOT publish a GitHub release for you.
EOF
}

VERSION="${1:-}"
if [[ -z "$VERSION" ]]; then
  usage
  exit 1
fi

if [[ "$VERSION" == v* ]]; then
  printf 'error: use plain semver without a v prefix: %s\n' "$VERSION" >&2
  exit 1
fi

if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.-]+)?$ ]]; then
  printf 'error: invalid semver: %s\n' "$VERSION" >&2
  exit 1
fi

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RELEASE_DATE="$(date -u +%F)"

python3 - "$ROOT" "$VERSION" "$RELEASE_DATE" <<'PY'
from __future__ import annotations

import json
import pathlib
import re
import sys

root = pathlib.Path(sys.argv[1])
version = sys.argv[2]
release_date = sys.argv[3]


def read(path: pathlib.Path) -> str:
    return path.read_text(encoding="utf-8")


def write(path: pathlib.Path, content: str) -> None:
    path.write_text(content, encoding="utf-8")


def replace_once(path: pathlib.Path, pattern: str, repl: str) -> None:
    content = read(path)
    updated, count = re.subn(pattern, repl, content, count=1, flags=re.MULTILINE)
    if count != 1:
        raise SystemExit(f"expected exactly one match in {path} for pattern: {pattern}")
    write(path, updated)


def update_openapi_version(path: pathlib.Path) -> None:
    data = json.loads(read(path))
    info = data.get("info")
    if not isinstance(info, dict):
        raise SystemExit(f"missing info object in {path}")
    info["version"] = version
    write(path, json.dumps(data, indent=2) + "\n")


def bump_chart_metadata(path: pathlib.Path) -> None:
    content = read(path)

    chart_match = re.search(r"(?m)^version: (\d+)\.(\d+)\.(\d+)$", content)
    if not chart_match:
        raise SystemExit(f"could not find chart version in {path}")
    major, minor, patch = map(int, chart_match.groups())
    next_chart_version = f"{major}.{minor}.{patch + 1}"

    updated, app_count = re.subn(
        r'(?m)^appVersion: ".*"$',
        f'appVersion: "{version}"',
        content,
        count=1,
    )
    if app_count != 1:
        raise SystemExit(f"could not update appVersion in {path}")

    updated, chart_count = re.subn(
        r"(?m)^version: \d+\.\d+\.\d+$",
        f"version: {next_chart_version}",
        updated,
        count=1,
    )
    if chart_count != 1:
        raise SystemExit(f"could not update chart version in {path}")

    write(path, updated)


def update_changelog(path: pathlib.Path) -> None:
    content = read(path)

    if f"## [{version}] - " in content:
        raise SystemExit(f"changelog already contains version {version}")

    match = re.search(
        r"(?ms)^## \[Unreleased\]\n(?P<body>.*?)(?=^## \[|^\[unreleased\]:|\Z)",
        content,
    )
    if not match:
        raise SystemExit(f"could not find Unreleased section in {path}")

    unreleased_body = match.group("body").strip("\n")
    released_section = f"## [Unreleased]\n\n## [{version}] - {release_date}\n"
    if unreleased_body:
        released_section += f"\n{unreleased_body}\n\n"
    else:
        released_section += "\n"

    content = content[: match.start()] + released_section + content[match.end() :]

    unreleased_link = f"[unreleased]: https://github.com/bonztm/agent-memory-manager/compare/{version}...HEAD"
    if re.search(r"(?m)^\[unreleased\]: .+$", content):
        content = re.sub(r"(?m)^\[unreleased\]: .+$", unreleased_link, content, count=1)
    else:
        if not content.endswith("\n"):
            content += "\n"
        content += unreleased_link + "\n"

    release_link = f"[{version}]: https://github.com/bonztm/agent-memory-manager/releases/tag/{version}"
    if not re.search(rf"(?m)^\[{re.escape(version)}\]: .+$", content):
        content = re.sub(
            r"(?m)^\[unreleased\]: .+$",
            lambda m: m.group(0) + "\n" + release_link,
            content,
            count=1,
        )

    write(path, content)


required_json_files = [
    root / "spec/v1/openapi.json",
    root / "internal/adapters/http/openapi_spec.json",
]

for json_file in required_json_files:
    update_openapi_version(json_file)

bump_chart_metadata(root / "deploy/helm/amm/Chart.yaml")
update_changelog(root / "CHANGELOG.md")

replace_once(
    root / "docs/mcp-reference.md",
    r'("version": ")\d+\.\d+\.\d+(")',
    rf'\g<1>{version}\g<2>',
)
replace_once(
    root / "deploy/helm/amm/README.md",
    r"appVersion: \d+\.\d+\.\d+",
    f"appVersion: {version}",
)
replace_once(
    root / "examples/api-mode/opencode/amm-http-plugin.ts",
    r"version: '\d+\.\d+\.\d+'",
    f"version: '{version}'",
)
replace_once(
    root / "examples/openclaw/package.json",
    r'"version": "\d+\.\d+\.\d+"',
    f'"version": "{version}"',
)
replace_once(
    root / "examples/hermes-agent/amm-legacy/plugin.yaml",
    r"(?m)^version: \d+\.\d+\.\d+$",
    f"version: {version}",
)
replace_once(
    root / "examples/hermes-agent/memory/amm/plugin.yaml",
    r"(?m)^version: \d+\.\d+\.\d+$",
    f"version: {version}",
)

print(f"Bumped repo release surfaces to {version}")
print("Updated:")
print("- spec/v1/openapi.json")
print("- internal/adapters/http/openapi_spec.json")
print("- deploy/helm/amm/Chart.yaml (appVersion + chart patch version)")
print("- CHANGELOG.md (promoted Unreleased to released section)")
print("- docs/mcp-reference.md")
print("- deploy/helm/amm/README.md")
print("- examples/api-mode/opencode/amm-http-plugin.ts")
print("- examples/openclaw/package.json")
print("- examples/hermes-agent/amm-legacy/plugin.yaml")
print("- examples/hermes-agent/memory/amm/plugin.yaml")
print()
print("Still manual:")
print(f"- fill the new CHANGELOG.md section for {version} if it was empty")
print(f"- create docs/release-notes/RELEASE_NOTES_{version}.md if you publish versioned release notes")
print(f"- publish the GitHub release/tag as plain semver: {version}")
PY

