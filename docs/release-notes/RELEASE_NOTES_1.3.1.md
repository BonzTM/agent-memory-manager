# [1.3.1] Release Notes - 2026-04-02

## Release Summary

amm 1.3.1 fixes two issues that caused degraded summary quality: the LLM reasoning parameter was sent in the wrong format (bare boolean instead of the required object), and the HTTP client timeout for LLM calls was hardcoded at 30 seconds, causing timeouts on large sessions. This release also makes all timeout values configurable and splits the reasoning toggle into two independent tunables to support the variety of model APIs available via OpenRouter and OpenAI.

## Fixed

- **LLM reasoning request format.** The `reasoning` parameter is now sent as the correct object format per the OpenAI/OpenRouter API spec:
  - `reasoning: {"effort": "high"}` when effort is configured
  - `reasoning: {"enabled": true}` when the simple toggle is configured
  - Previously sent `"reasoning": true` (bare boolean), which caused API errors or was silently ignored.

- **LLM timeout too short for large context.** The HTTP client timeout for summarizer and review model calls was hardcoded at 30 seconds. Large-context summarization (e.g., sessions with hundreds of events and a 900k context window) would time out, falling back to the heuristic summarizer which produced raw tool output as summaries instead of meaningful narratives. Default is now 300 seconds (5 minutes).

## Added

- **Configurable LLM timeouts.**
  - `AMM_SUMMARIZER_TIMEOUT_SECONDS` (default 300): HTTP client timeout for summarizer and review model calls.
  - `AMM_EMBEDDING_TIMEOUT_SECONDS` (default 30): HTTP client timeout for embedding API calls.

- **Configurable HTTP server timeouts.**
  - `AMM_HTTP_READ_TIMEOUT_SECONDS` (default 30)
  - `AMM_HTTP_WRITE_TIMEOUT_SECONDS` (default 60)
  - `AMM_HTTP_IDLE_TIMEOUT_SECONDS` (default 120)

- **Independent reasoning tunables.** The reasoning toggle and effort level are now separate configuration fields because different models support different subsets:
  - `AMM_SUMMARIZER_REASONING` / `AMM_REVIEW_REASONING`: Set to `enabled` to send `reasoning: {"enabled": true}`. Any other value omits the field.
  - `AMM_SUMMARIZER_REASONING_EFFORT` / `AMM_REVIEW_REASONING_EFFORT`: Set to `low`, `medium`, or `high` to send `reasoning: {"effort": "..."}`. Takes precedence over the simple toggle when both are set.

  | Model behavior | Set `reasoning` | Set `reasoning_effort` | API sends |
  |---|---|---|---|
  | Simple toggle only | `enabled` | _(empty)_ | `{"enabled": true}` |
  | Effort levels only | _(empty)_ | `low`/`medium`/`high` | `{"effort": "high"}` |
  | Both supported | `enabled` | `low`/`medium`/`high` | `{"effort": "high"}` |
  | Neither | _(empty)_ | _(empty)_ | _(field omitted)_ |

## Changed

- **Reasoning effort takes precedence.** When both `reasoning` and `reasoning_effort` are configured, only the effort object is sent since it's more specific.

## Admin/Operations

- All new timeout and reasoning env vars are documented in [docs/configuration.md](../configuration.md).
- All fields support JSON config, TOML config, and environment variable override.
- Existing deployments are unaffected — all new fields have sensible defaults matching or improving previous behavior (timeout raised from 30s to 300s is the only default change).

## Deployment and Distribution

- Release binaries: `amm`, `amm-mcp`, `amm-http`
- Docker image: `ghcr.io/bonztm/agent-memory-manager`
- Helm chart path: `deploy/helm/amm`

```bash
docker pull ghcr.io/bonztm/agent-memory-manager:1.3.1
helm upgrade --install amm ./deploy/helm/amm --set image.tag=1.3.1
```

## Breaking Changes

None. All new fields have defaults that match or improve previous behavior.

## Compatibility and Migration

No migration required. The only behavioral change is:
- LLM summarizer timeout increased from 30s to 300s (configurable via `AMM_SUMMARIZER_TIMEOUT_SECONDS`)
- If you previously set `AMM_SUMMARIZER_REASONING_EFFORT`, it now correctly sends the object format. No config changes needed.
- The new `AMM_SUMMARIZER_REASONING` and `AMM_REVIEW_REASONING` fields default to empty (omitted), preserving existing behavior.

## Full Changelog

- Release tag: https://github.com/bonztm/agent-memory-manager/releases/tag/1.3.1
- Full changelog: https://github.com/bonztm/agent-memory-manager/blob/main/CHANGELOG.md
