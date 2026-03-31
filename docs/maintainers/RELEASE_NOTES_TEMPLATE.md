# [{{VERSION}}] Release Notes - {{RELEASE_DATE}}

## Release Summary

{{RELEASE_SUMMARY}}

## Fixed

{{FIXED_ITEMS}}

## Added

{{ADDED_ITEMS}}

## Changed

{{CHANGED_ITEMS}}

## Admin/Operations

{{ADMIN_ITEMS}}

## Deployment and Distribution

- Release binaries: `amm`, `amm-mcp`, `amm-http`
- Docker image: `ghcr.io/bonztm/agent-memory-manager`
- Helm chart path: `deploy/helm/amm`

```bash
docker pull ghcr.io/bonztm/agent-memory-manager:{{VERSION}}
helm upgrade --install amm ./deploy/helm/amm --set image.tag={{VERSION}}
```

## Breaking Changes

{{BREAKING_CHANGES}}

## Known Issues

{{KNOWN_ISSUES}}

## Compatibility and Migration

{{COMPATIBILITY_AND_MIGRATION}}

## Full Changelog

- Release tag: https://github.com/bonztm/agent-memory-manager/releases/tag/{{VERSION}}
- Full changelog: https://github.com/bonztm/agent-memory-manager/blob/main/CHANGELOG.md
