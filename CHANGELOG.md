# Changelog

All notable changes to amm are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0] - 2026-03-30

### Added

- Initial 1.0.0 stable release of amm, a Go-based durable memory substrate for AI agents with one service layer exposed consistently through CLI (`amm`), MCP (`amm-mcp`), and HTTP (`amm-http`).
- Dual storage backend support with SQLite as the default local backend and PostgreSQL as the shared high-concurrency backend.
- Durable-memory workflow covering event ingestion, explicit memory writes, multi-mode recall, expand/describe/history queries, projects, relationships, privacy controls, policies, and integrity repair.
- HTTP API and MCP-over-HTTP support for remote, sidecar, and containerized deployments, plus OpenAPI and Swagger documentation.
- Runtime integration guidance and shipped examples for Claude Code, Codex, OpenCode, OpenClaw, Hermes-Agent, API-mode HTTP clients, and Kubernetes sidecar deployments.
- Background maintenance pipeline with reflect, compression, indexing, contradiction detection, graph rebuild, lifecycle review, and related worker jobs.
- Helm chart and sidecar deployment artifacts for Kubernetes-based installations.

[unreleased]: https://github.com/bonztm/agent-memory-manager/compare/1.0.0...HEAD
[1.0.0]: https://github.com/bonztm/agent-memory-manager/releases/tag/1.0.0
