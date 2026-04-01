# AMM Helm Quickstart

This chart deploys `amm-http` as a Kubernetes service.

## What this chart provides

- One `amm-http` Deployment
- A Service on port `8080`
- A ConfigMap for non-secret runtime configuration
- A Secret for `AMM_API_KEY` and optional provider credentials
- A PVC when using the default SQLite backend

## Prerequisites

- Kubernetes cluster
- Helm 3

## SQLite install (fastest path)

```bash
helm upgrade --install amm ./deploy/helm/amm \
  --set backend=sqlite \
  --set sqlite.persistence.enabled=true \
  --set sqlite.persistence.size=1Gi
```

This creates a PVC and stores the SQLite database at `/data/amm.db`.

## PostgreSQL install

Use PostgreSQL when you want a shared multi-agent backend.

```bash
helm upgrade --install amm ./deploy/helm/amm \
  --set backend=postgres \
  --set postgres.dsn='postgres://user:pass@postgres.example:5432/amm?sslmode=require'
```

For production, prefer supplying secrets through an existing Kubernetes Secret:

```bash
kubectl create secret generic amm-secrets \
  --from-literal=AMM_API_KEY='replace-me' \
  --from-literal=AMM_POSTGRES_DSN='postgres://user:pass@postgres.example:5432/amm?sslmode=require'

helm upgrade --install amm ./deploy/helm/amm \
  --set backend=postgres \
  --set secrets.existingSecret=amm-secrets
```

## Common options

- `backend=sqlite|postgres`
- `secrets.apiKey` or `secrets.existingSecret`
- `summarizer.endpoint`, `summarizer.model`, `secrets.summarizerApiKey`
- `embeddings.enabled=true`, `embeddings.endpoint`, `embeddings.model`, `secrets.embeddingsApiKey`
- `service.type=ClusterIP|LoadBalancer`
- `ingress.enabled=true`

See [`values.yaml`](./values.yaml) for the full set of chart values.

## Verify

```bash
kubectl get deploy,pods,svc -l app.kubernetes.io/name=amm
kubectl port-forward svc/amm-amm 8080:8080
curl http://localhost:8080/healthz
curl http://localhost:8080/v1/status
```

Expected health response:

```json
{"data":{"status":"ok"}}
```

## Notes

- The chart uses `appVersion: 1.1.0` by default.
- SQLite only supports a single writer at a time; use PostgreSQL for shared high-concurrency deployments.
- AMM maintenance jobs still need an external scheduler or runtime-triggered execution model.

For a sidecar deployment instead of a standalone service, see [`../../sidecar/README.md`](../../sidecar/README.md).
