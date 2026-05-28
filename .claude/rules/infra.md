---
paths:
  - "infra/**"
  - "**/Dockerfile"
  - "**/docker-compose*.yml"
---

# Infra rules (compose, Docker, services)

## Never install on the host
Everything runs in containers. Multi-stage builds, non-root runtime users, healthchecks on every service. Pin base images by digest.

## Port map (memorise — collisions cost real time)
Dev stack (`infra/docker-compose.yml`): postgres `127.0.0.1:5433`, minio `9002`/`9003`, nessie `19120`, ratd `8080`, portal `3000`.
Test stack (`docker-compose.test.yml`): postgres `5433`, minio `9002` — **collides with the dev stack**, so you can't run both at once. Use an isolated `-p` project name + shifted ports, or stop dev first.

## Verified compose fixes (don't regress these)
- **Postgres** needs `cap_add: [CHOWN, SETUID, SETGID, FOWNER, DAC_OVERRIDE]` with `cap_drop: [ALL]`.
- **DuckDB services** (ratq/runner) need tmpfs with `uid=1000,gid=1000,exec` for the extension install dir.
- **Portal healthcheck** uses `127.0.0.1` not `localhost` (IPv6 issue with wget).
- **ratd** needs `RAT_LISTEN_ADDR: "0.0.0.0:8080"` for Docker port mapping, and `INTERNAL_LISTEN_ADDR: "0.0.0.0:8090"` so other containers can reach the internal listener.

## Internal listener (ADR-019) — runner callbacks
ratd binds a **second listener on :8090** for service-to-service callbacks (run status, plugin phone-home, failed-merge audit). It has no auth — its model is network isolation. The runner must set `RATD_CALLBACK_URL: http://ratd:8090` (NOT `:8080`) or its "I'm done" POST 404s and runs hang "running" forever.

## Nessie (0.99.x) gotchas
- Env vars MUST use dotted property format (`- nessie.catalog.x=y`), not UPPERCASE.
- S3 auth uses a URN: `…access-key=urn:nessie-secret:quarkus:nessie.catalog.secrets.access-key` + `…secrets.access-key.name/.secret`.
- **Genesis bootstrap:** a fresh Nessie starts `main` at zero commits, and merging two descendants of an empty genesis 404s with "No common ancestor." The `nessie-init` container seeds one bootstrap commit; ratd/ratq/runner depend on it via `service_completed_successfully`. Without it the **first pipeline run on a fresh install always fails the merge.**
- `compose up --wait` treats a one-shot init container exiting (even 0) as failure — `--wait` only the long-running services, then `run --rm` the init separately.

## ratq query surface
ratq is read-only and **blocks direct file access** (`read_parquet`, `read_csv_auto`). Query via catalog views: `SELECT * FROM shop.bronze.orders` or `bronze.orders`.
