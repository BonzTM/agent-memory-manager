# amm 1.0.0 Release Notes - 2026-03-30

## Release Summary

amm 1.0.0 is the first stable release of the Agent Memory Manager: a Go-based durable memory substrate for agents that exposes one shared service layer through CLI, MCP, and HTTP. This release ships the core persistent-memory workflow, SQLite and PostgreSQL backends, MCP-over-stdio and MCP-over-HTTP support, REST APIs, background maintenance jobs, and integration guidance for Claude Code, Codex, OpenCode, OpenClaw, Hermes-Agent, container deployments, and Kubernetes.

## Highlights

- One product surface across local and remote operation: `amm` for CLI administration, `amm-mcp` for stdio MCP runtimes, and `amm-http` for REST and MCP-over-HTTP clients.
- Durable memory primitives for events, memories, projects, relationships, summaries, episodes, recall, explain-recall, history, repair, policies, and derived-data reset.
- Storage flexibility with SQLite for local single-user installs and PostgreSQL for shared multi-agent deployments.
- Runtime integrations and example configurations for Claude Code, Codex, OpenCode, OpenClaw, Hermes-Agent, and generic HTTP/API clients.
- Kubernetes deployment options through both a sidecar example and a Helm chart.

## Deployment and Distribution

- Release binaries: `amm`, `amm-mcp`, `amm-http`
- Docker image: `ghcr.io/bonztm/agent-memory-manager`
- Helm chart path: `deploy/helm/amm`

```bash
docker pull ghcr.io/bonztm/agent-memory-manager:1.0.0
helm upgrade --install amm ./deploy/helm/amm --set image.tag=1.0.0
```

## Breaking Changes

- None. This is the initial stable release.

## Known Issues

- SQLite remains a single-writer backend, so shared or high-concurrency deployments should prefer PostgreSQL.
- Background maintenance still requires an external scheduler or runtime-triggered execution model; AMM does not ship an internal worker daemon.

## Compatibility and Migration

No migration steps are required for first-time users. Choose SQLite for the fastest local install, or PostgreSQL when you need a shared/networked backend. Use plain semantic versions such as `1.0.0` for release tags and release references.

## Full Changelog

- Release tag: https://github.com/bonztm/agent-memory-manager/releases/tag/1.0.0
- Full changelog: https://github.com/bonztm/agent-memory-manager/blob/main/CHANGELOG.md
