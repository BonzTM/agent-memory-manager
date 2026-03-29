# AMM HTTP Sidecar Deployment Example

This example runs `amm-http` as a sidecar container in the same Pod as your agent runtime (Hermes, OpenClaw, or any other runtime). Because both containers share Pod networking, the agent can call AMM at `http://localhost:8080`.

## Prerequisites

- A Kubernetes cluster
- `kubectl` configured for that cluster

## Files

- `sidecar-deployment.yaml` — plain Kubernetes manifest with:
  - `Secret` (`amm-api-key`) placeholder
  - `PersistentVolumeClaim` (`amm-data`) for SQLite persistence
  - `Deployment` with two containers: `agent` and `amm`

## Customize Before Deploying

1. Replace the agent image:
   - In `sidecar-deployment.yaml`, set `spec.template.spec.containers[name=agent].image` from `your-agent-image:latest` to your real image.

2. Set the AMM API key:
   - Replace `data.api-key: <base64-encoded-key>` in the `Secret`.
   - Example (do not commit real keys):
     ```bash
     printf '%s' 'your-strong-api-key' | base64
     ```

3. Configure project/session IDs:
   - Set `AMM_PROJECT_ID` on the agent container to a stable project identifier.
   - Set `AMM_SESSION_ID` from your runtime per session/thread.

4. Choose backend:
   - **SQLite (default in this example):**
     - `AMM_STORAGE_BACKEND=sqlite`
     - `AMM_DB_PATH=/data/amm.db`
     - PVC `amm-data` mounted at `/data`
   - **PostgreSQL (alternative):**
     - Uncomment the PostgreSQL env vars in the AMM container
     - Provide a real `AMM_POSTGRES_DSN`
     - You can remove the `/data` volume mount and PVC when not using SQLite

5. Namespace:
   - Manifest uses `namespace: default`; update it for your environment.

## Deploy

```bash
kubectl apply -f sidecar-deployment.yaml
```

## Verify

1. Check pods:

   ```bash
   kubectl get pods -l app=agent-with-amm
   ```

2. Call AMM health endpoint from inside the Pod:

   ```bash
   kubectl exec deploy/agent-with-amm -c agent -- curl -sS http://localhost:8080/healthz
   ```

3. Check AMM status endpoint:

   ```bash
   kubectl exec deploy/agent-with-amm -c agent -- curl -sS http://localhost:8080/v1/status
   ```

Expected health response shape:

```json
{"data":{"status":"ok"}}
```

## Related Docs

- [Hermes agent integration guide](../../docs/hermes-agent-integration.md)
