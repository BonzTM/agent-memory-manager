# [1.4.1] Release Notes - 2026-04-04

## Release Summary

amm 1.4.1 is a patch release fixing a packaging issue in the OpenClaw native plugin. The `src/curated-sync.ts` module was missing from the npm package, causing the plugin to fail on load when curated memory mirroring was enabled.

## Fixed

- **Include `curated-sync.ts` in OpenClaw npm package.** `src/curated-sync.ts` was imported by `index.ts` but omitted from the `files` array in `examples/openclaw/package.json`. This caused `Cannot find module './src/curated-sync.ts'` when OpenClaw attempted to load the plugin. The file is now included in the published package.

## Deployment and Distribution

- Release binaries: `amm`, `amm-mcp`, `amm-http`
- Docker image: `ghcr.io/bonztm/agent-memory-manager`
- Helm chart path: `deploy/helm/amm`
- npm package: `@bonztm/amm@1.4.1`

```bash
docker pull ghcr.io/bonztm/agent-memory-manager:1.4.1
helm upgrade --install amm ./deploy/helm/amm --set image.tag=1.4.1
npm install @bonztm/amm@1.4.1
```

## Breaking Changes

None.

## Known Issues

None.

## Compatibility and Migration

No migration required. Reinstall the OpenClaw plugin and restart to pick up the fix.

## Full Changelog

- Release tag: https://github.com/bonztm/agent-memory-manager/releases/tag/1.4.1
- Full changelog: https://github.com/bonztm/agent-memory-manager/blob/main/CHANGELOG.md
