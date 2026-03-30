#!/usr/bin/env python3

import json
import pathlib
import re
import subprocess
import sys


REPO_ROOT = pathlib.Path(__file__).resolve().parents[1]

COMMANDS_GO = REPO_ROOT / "internal/contracts/v1/commands.go"
MCP_SERVER_GO = REPO_ROOT / "internal/adapters/mcp/server.go"
HTTP_SERVER_GO = REPO_ROOT / "internal/adapters/http/server.go"
CLI_SCHEMA_JSON = REPO_ROOT / "spec/v1/cli.command.schema.json"
MCP_TOOLS_JSON = REPO_ROOT / "spec/v1/mcp.tools.v1.json"
OPENAPI_JSON = REPO_ROOT / "spec/v1/openapi.json"
HTTP_OPENAPI_JSON = REPO_ROOT / "internal/adapters/http/openapi_spec.json"
PAYLOADS_SCHEMA_JSON = REPO_ROOT / "spec/v1/payloads.schema.json"

INFRA_ROUTES = {
    "/healthz",
    "/openapi.json",
    "/swagger/",
    "/v1/mcp",
    "/v1/mcp/",
}

GO_BINARY_CANDIDATES = [
    "go",
    "/usr/local/go/bin/go",
    "/usr/bin/go",
    "/usr/local/bin/go",
    "/opt/homebrew/bin/go",
]


def read_text(path):
    return path.read_text(encoding="utf-8")


def read_json(path):
    with path.open("r", encoding="utf-8") as handle:
        return json.load(handle)


def format_items(items):
    if not items:
        return "none"
    return ", ".join(sorted(items))


def resolve_go_binary():
    for candidate in GO_BINARY_CANDIDATES:
        candidate_path = pathlib.Path(candidate)
        if candidate != "go" and not candidate_path.exists():
            continue
        try:
            result = subprocess.run(
                [candidate, "version"],
                cwd=str(REPO_ROOT),
                capture_output=True,
                text=True,
            )
        except FileNotFoundError:
            continue
        if result.returncode == 0:
            return candidate
    raise FileNotFoundError("could not locate a working Go binary")


def extract_brace_block(text, anchor_pattern, label):
    match = re.search(anchor_pattern, text, re.MULTILINE)
    if not match:
        raise ValueError("could not find %s" % label)

    start = match.end() - 1
    depth = 0
    in_string = False
    string_quote = ""
    in_line_comment = False
    in_block_comment = False
    escape = False

    for index in range(start, len(text)):
        char = text[index]
        next_char = text[index + 1] if index + 1 < len(text) else ""

        if in_line_comment:
            if char == "\n":
                in_line_comment = False
            continue

        if in_block_comment:
            if char == "*" and next_char == "/":
                in_block_comment = False
            continue

        if in_string:
            if string_quote == '"':
                if escape:
                    escape = False
                elif char == "\\":
                    escape = True
                elif char == '"':
                    in_string = False
            elif char == string_quote:
                in_string = False
            continue

        if char == "/" and next_char == "/":
            in_line_comment = True
            continue

        if char == "/" and next_char == "*":
            in_block_comment = True
            continue

        if char in ('"', "`", "'"):
            in_string = True
            string_quote = char
            escape = False
            continue

        if char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return text[start:index + 1]

    raise ValueError("could not extract %s block" % label)


def parse_go_command_registry(path):
    text = read_text(path)
    const_values = dict(
        re.findall(r'^\s*(Cmd[A-Za-z0-9_]+)\s*=\s*"([^"]+)"\s*$', text, re.MULTILINE)
    )
    registry_block = extract_brace_block(
        text,
        r"var\s+CommandRegistry\s*=\s*map\[string\]CommandInfo\s*\{",
        "CommandRegistry",
    )
    registry_keys = re.findall(r'^\s*(Cmd[A-Za-z0-9_]+)\s*:\s*\{', registry_block, re.MULTILINE)

    missing_constants = [key for key in registry_keys if key not in const_values]
    if missing_constants:
        raise ValueError(
            "registry keys missing matching constants: %s" % format_items(missing_constants)
        )

    return {const_values[key] for key in registry_keys}


def parse_cli_schema_commands(path):
    spec = read_json(path)
    enum_values = spec["properties"]["command"]["enum"]
    return set(enum_values)


def parse_mcp_go_tools(path):
    text = read_text(path)
    tools_block = extract_brace_block(text, r"func\s+tools\s*\(\s*\)\s*\[\]Tool\s*\{", "tools()")
    name_matches = re.findall(
        r'Name\s*:\s*"([^"]+)"|mcp\.NewTool\(\s*"([^"]+)"',
        tools_block,
        re.MULTILINE,
    )

    names = set()
    for left, right in name_matches:
        if left:
            names.add(left)
        if right:
            names.add(right)
    return names


def parse_mcp_spec_tools(path):
    spec = read_json(path)
    tools = spec.get("tools", [])
    return {tool.get("name", "") for tool in tools if tool.get("name")}


def normalize_route_pattern(route_pattern):
    parts = route_pattern.split()
    if not parts:
        return ""
    if parts[0].startswith("/"):
        return parts[0]
    if len(parts) >= 2 and parts[1].startswith("/"):
        return parts[1]
    return ""


def parse_http_go_routes(path):
    text = read_text(path)
    routes_block = extract_brace_block(
        text,
        r"func\s*\(s\s*\*Server\)\s*registerRoutes\s*\([^)]*\)\s*\{",
        "registerRoutes",
    )
    raw_patterns = re.findall(r'\bHandle(?:Func)?\(\s*"([^"]+)"', routes_block, re.MULTILINE)
    routes = set()
    for pattern in raw_patterns:
        route = normalize_route_pattern(pattern)
        if route:
            routes.add(route)
    return routes


def parse_openapi_paths(path):
    spec = read_json(path)
    return set(spec.get("paths", {}).keys())


def check_cli_command_enum():
    go_commands = parse_go_command_registry(COMMANDS_GO)
    spec_commands = parse_cli_schema_commands(CLI_SCHEMA_JSON)
    envelope_commands = go_commands - {"run", "validate"}
    missing = envelope_commands - spec_commands
    if missing:
        return False, "missing commands in spec: %s" % format_items(missing)
    return True, "Go commands covered by schema enum (%d commands)" % len(envelope_commands)


def check_mcp_tool_catalog():
    go_tools = parse_mcp_go_tools(MCP_SERVER_GO)
    spec_tools = parse_mcp_spec_tools(MCP_TOOLS_JSON)
    missing = go_tools - spec_tools
    extra = spec_tools - go_tools
    if missing or extra:
        details = []
        if missing:
            details.append("missing in spec: %s" % format_items(missing))
        if extra:
            details.append("extra in spec: %s" % format_items(extra))
        return False, "; ".join(details)
    return True, "MCP tool catalog matches Go (%d tools)" % len(go_tools)


def check_openapi_paths():
    go_routes = parse_http_go_routes(HTTP_SERVER_GO)
    openapi_paths = parse_openapi_paths(OPENAPI_JSON)
    missing = {route for route in go_routes if route not in openapi_paths and route not in INFRA_ROUTES}
    if missing:
        return False, "routes missing from OpenAPI: %s" % format_items(missing)
    return True, "HTTP routes covered by OpenAPI (%d checked)" % len(go_routes - INFRA_ROUTES)


def check_openapi_file_sync():
    source_bytes = OPENAPI_JSON.read_bytes()
    copy_bytes = HTTP_OPENAPI_JSON.read_bytes()
    if source_bytes != copy_bytes:
        return False, "spec/v1/openapi.json and internal/adapters/http/openapi_spec.json differ"
    return True, "OpenAPI files are byte-identical"


def check_payload_schema_freshness():
    if not PAYLOADS_SCHEMA_JSON.exists():
        return None, "spec/v1/payloads.schema.json not present; skipping freshness check"

    before = PAYLOADS_SCHEMA_JSON.read_bytes()
    go_binary = resolve_go_binary()
    result = subprocess.run(
        [go_binary, "run", "scripts/generate-payload-schema.go"],
        cwd=str(REPO_ROOT),
        capture_output=True,
        text=True,
    )
    after = PAYLOADS_SCHEMA_JSON.read_bytes() if PAYLOADS_SCHEMA_JSON.exists() else b""

    if after != before:
        PAYLOADS_SCHEMA_JSON.write_bytes(before)

    if result.returncode != 0:
        stderr = result.stderr.strip() or result.stdout.strip() or "generator failed"
        return False, "payload schema generator failed: %s" % stderr

    if before != after:
        return False, "payloads.schema.json is stale relative to scripts/generate-payload-schema.go"

    return True, "payloads.schema.json matches generator output"


def main():
    checks = [
        ("CLI command enum completeness", check_cli_command_enum),
        ("MCP tool catalog completeness", check_mcp_tool_catalog),
        ("OpenAPI path completeness", check_openapi_paths),
        ("OpenAPI file sync", check_openapi_file_sync),
        ("payloads.schema.json freshness", check_payload_schema_freshness),
    ]

    failed = 0
    warned = 0
    passed = 0

    for name, check in checks:
        try:
            ok, detail = check()
        except Exception as exc:
            ok = False
            detail = str(exc)

        if ok is True:
            passed += 1
            print("PASS %s: %s" % (name, detail))
        elif ok is None:
            warned += 1
            print("WARN %s: %s" % (name, detail))
        else:
            failed += 1
            print("FAIL %s: %s" % (name, detail))

    total = len(checks)
    print("SUMMARY total=%d passed=%d failed=%d warnings=%d" % (total, passed, failed, warned))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
