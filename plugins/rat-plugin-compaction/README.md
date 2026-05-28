# rat-plugin-compaction

Detects Iceberg tables with too many small data files and rewrites them.
After 8 demo-loader runs, `shop.bronze.orders` had 2,860 parquet files
(~3 rows each); after compaction it has 1, and `SELECT COUNT(*)` drops
from 1100 ms to 6 ms.

## Install

```bash
docker run -d --name compaction --network infra_default --restart unless-stopped \
  -e RATD_URL=http://ratd:8080 \
  -e RATD_INTERNAL_URL=http://ratd:8090 \
  -e NESSIE_URL=http://nessie:19120 \
  -e S3_ENDPOINT=http://minio:9000 \
  -e S3_ACCESS_KEY=minioadmin \
  -e S3_SECRET_KEY=minioadmin \
  -e S3_BUCKET=rat \
  ghcr.io/squat-collective/rat-plugin-compaction:latest
```

Or uncomment the `compaction:` block in
[`infra/docker-compose.plugins.yml`](../../infra/docker-compose.plugins.yml)
and run `docker compose -f infra/docker-compose.yml -f infra/docker-compose.plugins.yml up -d`.

The plugin phones home immediately; open **Compaction** in the portal
sidebar (or `/x/compaction`) to see per-table file health.

## Configuration

All knobs are env vars (no portal config yet — see *Roadmap*):

| Var | Default | Purpose |
|---|---|---|
| `COMPACT_INTERVAL_SECS` | `600` | Detection sweep cadence |
| `COMPACT_TARGET_FILE_BYTES` | `16777216` (16 MiB) | Well-sized file target |
| `COMPACT_MIN_FILE_COUNT` | `50` | Skip tables under this count |
| `COMPACT_RATIO` | `0.3` | Undersize-score required to fire |
| `COMPACT_AUTO` | `true` | Run auto-compaction loop |
| `COMPACT_TIMEOUT_SECS` | `300` | Per-table timeout |

## How it works

- **Detection** reads each table's current `metadata.json` snapshot
  summary (`total-data-files`, `total-files-size`) — not a raw S3 listing.
  Raw listing would include orphan files from past snapshots that
  haven't been physically deleted yet, which would loop the
  auto-compactor forever.
- **Rewrite** shells out to `compact.py` (PyIceberg) per table. The
  Python script reads the current snapshot to Arrow and calls
  `tbl.overwrite(arrow_data)` — a poor-man's compaction until PyIceberg
  ships a native `rewrite_data_files` API.
- The old data files **stay on S3** as unreferenced past snapshots until
  snapshot expiration runs (`pyiceberg.table.maintenance.expire_snapshots`).
  Pair with snapshot expiry for true storage reclamation.

## API (proxied at `/api/v1/x/compaction/*`)

| Route | Purpose |
|---|---|
| `GET /tables` | All monitored tables + health stats |
| `POST /scan` | Force out-of-cycle detection sweep |
| `POST /tables/{ns}/{layer}/{name}/compact` | Manual compact (fire-and-forget, returns 202) |

## Build from source

```bash
git clone https://github.com/squat-collective/rat
cd rat/plugins/rat-plugin-compaction
make build && make run
```

## Roadmap

- Portal config editor (replace env vars with the
  [plugin-config](../../docs/PLUGIN_AUTHOR_GUIDE.md) pattern).
- Sibling snapshot-expiry task so storage drops too.
- Sort-on-rewrite for better query pruning (z-order / sort key support
  in PyIceberg).
