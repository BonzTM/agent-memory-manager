# PostgreSQL Storage Backend

AMM supports PostgreSQL as a high-concurrency storage backend, ideal for multi-agent systems and shared memory environments.

## Configuration

To enable the PostgreSQL backend, set the following environment variables:

- `AMM_STORAGE_BACKEND=postgres`
- `AMM_POSTGRES_DSN=postgres://user:pass@host:5432/dbname?sslmode=disable`

SQLite remains the default when `AMM_STORAGE_BACKEND` is unset or set to `sqlite`.

## Docker Compose

A pre-configured `docker-compose.postgres.yaml` is provided for local development:

```bash
docker compose -f docker-compose.postgres.yaml up
```

This starts a PostgreSQL 16 container and AMM configured to use it.

If you still use legacy Compose v1 tooling, `docker-compose -f docker-compose.postgres.yaml up` works as well.

For Kubernetes deployments, see the [Helm quickstart](../deploy/helm/amm/README.md).

## Features & Parity

The PostgreSQL backend implements the same `core.Repository` interface as the SQLite backend. 

### Search Support
PostgreSQL search currently uses standard SQL pattern matching. For performance parity with SQLite's FTS5, it is recommended to have `pgroonga` or `pg_trgm` installed on your database, although AMM will fall back to basic matching if they are missing.

### Migrations
AMM handles PostgreSQL migrations automatically on startup. The schema version is tracked in the `schema_version` table.

## Testing

PostgreSQL integration tests are located in `internal/adapters/postgres/repository_test.go`. These tests are skipped by default unless the `AMM_TEST_POSTGRES_DSN` environment variable is set.

```bash
AMM_TEST_POSTGRES_DSN="postgres://user:pass@localhost:5432/amm_test" go test ./internal/adapters/postgres/...
```

## Current Status
The PostgreSQL backend is fully supported for core operations (Ingest, Remember, Recall). Some advanced maintenance jobs may still have optimized paths specifically for SQLite (FTS5).
