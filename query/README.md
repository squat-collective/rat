# rat-query (ratq)

The Python service that answers interactive DuckDB queries.

Where `runner` *writes* to Iceberg tables, `ratq` *reads* from them.
The portal's query editor, table preview, and schema introspection all
flow through here. ratq is read-only by design — there's no `INSERT` /
`UPDATE` / `DELETE` path through this service.

## Architecture

```
        portal (browser)
          │
          ▼
        ratd  POST /api/v1/query
          │ ExecuteQuery (ConnectRPC)
          ▼
    ┌───────────┐
    │   ratq    │  ─► Nessie  (catalog: list tables, get metadata.json)
    │           │  ─► S3      (parquet reads via DuckDB httpfs)
    │           │  ─► DuckDB  (registered Iceberg views, execute SQL)
    └───────────┘
              │ Arrow IPC response
              ▼
            ratd  (decode + JSON-encode for the REST client)
```

DuckDB's Iceberg extension does the actual parquet reads — ratq just
keeps the catalog of `CREATE VIEW name AS iceberg_scan('<metadata>')`
statements in sync with what Nessie says exists.

## Catalog refresh loop

Every 30 seconds (`CATALOG_REFRESH_SECS`), ratq:

1. Asks Nessie for the current `main` branch commit hash.
2. If unchanged from the previous tick → no-op (the fast path; covers
   the steady-state where no pipelines have run).
3. If changed → re-discover every table via Nessie's content API, then
   `CREATE OR REPLACE VIEW` for any whose `metadata.json` location
   moved (new snapshot). Drop views for tables no longer in Nessie.

This means a freshly written table is queryable within 30s of its
pipeline finishing. The view definition is just `iceberg_scan(...)` —
DuckDB handles snapshot-aware reads, so old data files superseded by
overwrites or deletes are correctly excluded.

## Query path

| RPC | Behaviour |
|---|---|
| `ExecuteQuery` | Run user SQL against the registered views. Returns Arrow IPC. |
| `ListTables` | Cached list of all `(ns, layer, name)` triples Nessie advertises. |
| `GetSchema` | DuckDB's `DESCRIBE "schema"."table"` against the iceberg view. |
| `PreviewTable` | `SELECT * FROM "schema"."table" LIMIT n` (default 100). |

## Two ways to address a table

The catalog registers each table under **two** schema paths so SQL can
say either:

```sql
SELECT * FROM bronze.orders          -- short form (default catalog)
SELECT * FROM shop.bronze.orders     -- explicit namespace (attached catalog)
```

The namespace path uses DuckDB's `ATTACH ':memory:' AS "<ns>"` so each
namespace gets its own catalog database. This avoids collisions when
two namespaces hold a `bronze.orders` table.

## Configuration

All config via env vars — see [`docs/config.md`](../docs/config.md#query-service-ratq)
for the full table. Common knobs:

| Var | Default | Purpose |
|---|---|---|
| `RATQ_ADDR` (on `ratd`) | — | When set, ratd proxies queries to this address |
| `GRPC_PORT` | `50051` | Server listen port |
| `QUERY_TIMEOUT_SECS` | `60` | Per-query DuckDB timeout |
| `CATALOG_REFRESH_SECS` | `30` | How often to re-poll Nessie for changes |
| `NESSIE_URL`, `S3_*` | — | Catalog + object store config |

## Limitations

- **Read-only.** DuckDB is bootstrapped without write access; the
  service intentionally has no path to mutate Iceberg state.
- **No direct `read_parquet` / `read_csv_auto`.** Those would let users
  reach past the catalog and read raw bucket contents. Query through
  the registered views instead.
- **30-second freshness window** for newly written tables. Acceptable
  for an interactive analytics use case; tune `CATALOG_REFRESH_SECS`
  down if needed.

## Development

```bash
# from repo root
make test-py     # runs runner + query test suites in Docker
make dev-ratq    # hot-reload ratq container with code mount
make proto       # regenerate gRPC stubs (if proto/ changed)
```

Test suite in `tests/unit/` (~200 tests).

## See also

- [`docs/adr/006-query-service.md`](../docs/adr/006-query-service.md) — original architecture decision
- [`docs/api-spec.md`](../docs/api-spec.md) — REST endpoints that wrap ratq's gRPC surface
