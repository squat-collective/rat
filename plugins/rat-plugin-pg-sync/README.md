# rat-plugin-pg-sync

Mirrors external Postgres tables into RAT's Iceberg lake. Each table
sync becomes an auto-generated SQL pipeline + cron schedule. Two modes:
**snapshot** (full refresh each run) and **incremental** (watermark
column → only new rows). Connection URLs are stored as named secrets via
[`rat-plugin-secrets`](../rat-plugin-secrets/) — never in this plugin's
own state.

## Install

Requires `rat-plugin-secrets` to be running first (URLs are resolved
through the `secrets.get` interconnect capability).

```bash
docker run -d --name pg-sync --network infra_default --restart unless-stopped \
  -e RATD_URL=http://ratd:8080 \
  -e RATD_INTERNAL_URL=http://ratd:8090 \
  ghcr.io/squat-collective/rat-plugin-pg-sync:latest
```

Or uncomment the `pg-sync:` block in
[`infra/docker-compose.plugins.yml`](../../infra/docker-compose.plugins.yml).

Manage connections + table syncs at `/x/pg-sync` in the portal.

## Usage

1. Create a secret in `/x/secrets` holding your Postgres URL
   (`postgres://user:pass@host:5432/db`).
2. In `/x/pg-sync`, add a **connection** that points at that secret.
3. Add a **table sync** under that connection — pick source schema/table,
   target namespace/layer/name, mode, schedule.
4. The plugin generates `pipeline.sql` + a cron schedule in ratd. Run it
   manually via *Sync now* or wait for the schedule to fire.

Rotating the secret value → next sync regenerates the SQL file with the
new URL (no manual update needed).

## Environment

| Var | Default | Purpose |
|---|---|---|
| `RATD_URL` | `http://ratd:8080` | ratd base URL |
| `RATD_INTERNAL_URL` | `http://ratd:8090` | ratd internal listener |
| `GRPC_PORT` | `50100` | Port to serve on |
| `PLUGIN_NAME` | `pg-sync` | Registered plugin name |
| `PLUGIN_ADDR` | `pg-sync:50100` | Address ratd dials back |

## Limitations

- **Watermark filter is `>`, not `>=`.** Rows with the exact same
  watermark timestamp as the last sync are skipped — fine for monotonic
  timestamps, lossy if multiple rows share a timestamp. Pair watermark
  with a unique key for true deduplication.
- **Schema changes need re-apply.** If the source's columns change,
  re-apply the table sync to regenerate the pipeline.

## Build from source

```bash
git clone https://github.com/squat-collective/rat
cd rat/plugins/rat-plugin-pg-sync
make build && make run
```
