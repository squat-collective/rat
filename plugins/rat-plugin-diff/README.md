# rat-plugin-diff

Live activity feed (15 s poll) + Nessie-backed row-level diff drill-in
for any Iceberg table. When two snapshots of a table exist, click any
event in the feed to see exactly which rows were inserted, updated, or
removed.

## Install

```bash
docker run -d --name diff --network infra_default --restart unless-stopped \
  -e RATD_URL=http://ratd:8080 \
  -e RATD_INTERNAL_URL=http://ratd:8090 \
  -e NESSIE_URL=http://nessie:19120 \
  -e S3_ENDPOINT=http://minio:9000 \
  -e S3_ACCESS_KEY=minioadmin \
  -e S3_SECRET_KEY=minioadmin \
  -e S3_BUCKET=rat \
  ghcr.io/squat-collective/rat-plugin-diff:latest
```

Or uncomment the `diff:` block in
[`infra/docker-compose.plugins.yml`](../../infra/docker-compose.plugins.yml).

The activity feed lives at `/x/diff` in the portal.

## How it works

- **Activity feed** polls Nessie's `/api/v2/trees/main/commits` every
  15 s, ingests new commits as events `{commit_id, table, snapshot_id,
  operation, summary}`.
- **Row-level diff** is computed on demand: when the user opens a diff,
  the plugin fingerprints both snapshots by reading the parquet data
  files for each, hashes each row, then set-differences the two hash
  sets to identify inserted vs removed rows. Updates are detected by
  matching on the user-declared primary key.
- No persistent state — the feed is in-memory (last 500 events).
  Restart loses history.

## Environment

| Var | Default | Purpose |
|---|---|---|
| `RATD_URL` | `http://ratd:8080` | ratd base URL |
| `RATD_INTERNAL_URL` | `http://ratd:8090` | ratd internal listener |
| `NESSIE_URL` | _(required)_ | Nessie REST base URL |
| `S3_ENDPOINT`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_BUCKET` | _(required)_ | MinIO/S3 access for parquet reads |
| `GRPC_PORT` | `50105` | Port to serve on |
| `DIFF_POLL_INTERVAL_SECS` | `15` | Activity-feed poll cadence |

## Build from source

```bash
git clone https://github.com/squat-collective/rat
cd rat/plugins/rat-plugin-diff
make build && make run
```
