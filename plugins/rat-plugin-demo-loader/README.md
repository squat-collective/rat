# rat-plugin-demo-loader

An example RAT **platform + portal plugin** — a **one-click sample-data
demo installer**. Adds a "Demos" sidebar entry where the user picks a
self-contained demo and installs it: namespace + pipelines + quality tests
+ bronze runs, all created via ratd's HTTP API.

## What ships

Each demo is a full **bronze → silver → gold** story with quality tests:

| Demo | Theme | What's inside |
|---|---|---|
| 🚀 **Cosmos** | Space | missions, observations, satellites → mission success rate per program, satellite fleet summary |
| 🎤 **Underground** | DIY scene | venues, gigs, attendees → attendance per venue, most-active artists |
| 🛒 **Shop** | Sales | customers, products, orders → monthly revenue, top customers |

Per demo: 3 bronze + 2 silver + 2 gold pipelines, 2 quality tests, all
synthesised in pure SQL using `generate_series()` (no external files).

## How it works

`POST /install {demo_id}` walks the demo's `manifest.json` and:

1. **Creates the namespace** (idempotent — 409 conflicts are treated as
   success, so reinstalls work).
2. **Creates each pipeline** and **writes its `pipeline.sql`** from the
   embedded files.
3. **Creates the quality tests** under each pipeline.
4. **Submits the initial bronze runs**, lightly staggered (the runner's
   DuckDB process struggles with several parallel executions).

Silver and gold pipelines are *not* auto-run — pick them up on the
Pipelines page or wire a schedule.

## API

| Route | Purpose |
|---|---|
| `GET /demos` | Lists the available demos |
| `POST /install` | `{demo_id, namespace?}` — installs the demo |

## Adding your own demo

Drop a folder under `demos/<id>/`:

```
demos/my-demo/
  manifest.json
  bronze/raw.sql
  silver/clean.sql
  gold/agg.sql
  tests/no_nulls.sql
```

The `manifest.json` lists the pipelines and tests. Rebuild the plugin and
your demo appears in the list. Synthesise data with DuckDB's
`generate_series()` so nothing external is required.

## Build & run

```bash
make build
make run
```

Then in the portal: **Demos** in the sidebar → pick one → **Install**.

## Run tests

```bash
make test
```
