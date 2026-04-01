# [1.2.1] Release Notes - 2026-04-01

## Release Summary

amm 1.2.1 is a patch release focused on the OpenClaw plugin and integration polish. The OpenClaw plugin is now published on npm as `@bonztm/amm` for one-command install, with a local `install.sh` for binary-mode deployments. The OpenCode plugin gains native `memory_search`/`memory_get` tools. The release also fixes the Postgres test for the sessionless claim filter introduced in 1.2.0.

## What's New

### OpenClaw plugin on npm

```bash
openclaw plugins install @bonztm/amm
```

The npm package uses HTTP transport only (OpenClaw's security scanner blocks `child_process` imports). Configure `apiUrl` in plugin config to point at your `amm-http` instance.

For local binary mode (no HTTP server needed):

```bash
cd examples/openclaw && ./install.sh
```

The install script provides the full dual-transport plugin with options for `--api-url`, `--project-id`, `--mcp`, and more.

### OpenCode native tools

The OpenCode plugin now registers `memory_search` and `memory_get` as native tools via the `tool` hook, providing direct memory access without requiring the MCP sidecar.

### Trusted publisher workflow

The release workflow now publishes the OpenClaw npm package via GitHub OIDC trusted publishers with provenance attestation. No npm token secret needed.

## OpenClaw Memory Slot

The 1.2.0 changelog mentioned the plugin claiming the OpenClaw memory slot. After testing, this was reverted. OpenClaw's memory slot contract (`MemoryPluginRuntime`) requires plugins that own the full memory lifecycle — storage, embeddings, search managers, flush plans, sync. AMM's architecture (Go binary + thin TypeScript client) doesn't match this contract. The hooks-based integration (`before_prompt_build` for ambient recall, `registerHook` for event capture) has been stable since 1.1.0 and provides the same user-facing functionality.

## Deployment and Distribution

- Release binaries: `amm`, `amm-mcp`, `amm-http`
- Docker image: `ghcr.io/bonztm/agent-memory-manager`
- Helm chart: published at `https://bonztm.github.io/agent-memory-manager`
- OpenClaw plugin: `@bonztm/amm` on npm

```bash
docker pull ghcr.io/bonztm/agent-memory-manager:1.2.1
helm repo add amm https://bonztm.github.io/agent-memory-manager
helm upgrade --install amm amm/amm --set image.tag=1.2.1
openclaw plugins install @bonztm/amm
```

## Breaking Changes

None. This is a backwards-compatible patch release.

## Compatibility and Migration

No migration required from 1.2.0. The OpenClaw plugin changes are plugin-side only — no server-side changes.

If you installed the OpenClaw plugin with `kind: "memory"` from a 1.2.0 pre-release, reinstall to get the hooks-only version:

```bash
cd examples/openclaw && ./install.sh
# Or: openclaw plugins install @bonztm/amm
```

## Full Changelog

- Release tag: https://github.com/bonztm/agent-memory-manager/releases/tag/1.2.1
- Full changelog: https://github.com/bonztm/agent-memory-manager/blob/main/CHANGELOG.md
