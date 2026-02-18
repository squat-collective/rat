# ADR-006: Query Service Architecture (ratq)

## Status: Accepted

## Context

ratd has 4 HTTP endpoints for interactive queries (`POST /query`, `GET /tables`, `GET /tables/:ns/:layer/:name`,
`GET /tables/:ns/:layer/:name/preview`) fronted by a `QueryStore` interface. These need a backend that:

- Executes read-only DuckDB queries against Iceberg table data stored in S3
- Discovers tables from the Nessie catalog (authoritative source, no S3 listing)
- Returns results as Arrow IPC for efficient binary transfer
- Provides schema introspection and row count statistics
- Runs as a persistent sidecar (not per-request)

Key constraints:
- Must be Python (DuckDB + PyArrow native ecosystem)
- Read-only — all writes go through the runner
- Must handle concurrent queries from the portal

## Decision

### Long-lived DuckDB connection (not per-request)

ratq is a persistent sidecar, not an ephemeral service. A single DuckDB in-memory connection is
created on startup and reused for all queries. DuckDB 1.0+ handles internal query parallelism on
a single connection for concurrent reads.

This differs from the runner (one connection per run) because:
- ratq is read-only — no write contention
- Connection setup cost (httpfs + iceberg extensions) is non-trivial
- DuckDB views must persist across requests

### DuckDB schemas map to layers

Tables are registered as DuckDB views using the medallion layer as the schema name:
```sql
CREATE SCHEMA IF NOT EXISTS silver;
CREATE OR REPLACE VIEW silver.orders AS
  SELECT * FROM read_parquet('s3://bucket/ns/data/silver/orders/**/*.parquet');
```

This allows natural SQL: `SELECT * FROM silver.orders WHERE amount > 100`.

### Nessie REST API for table discovery (not S3 listing)

Tables are discovered via `GET /api/v2/trees/main/entries` which returns all Iceberg table entries.
This is authoritative (Nessie is the catalog) and avoids expensive S3 list operations.

Entry key convention: `[namespace, "data", layer, table_name]` — parsed to extract the S3 path.

### Background catalog refresh (30s interval)

A daemon thread re-discovers tables from Nessie every 30 seconds:
1. Call Nessie API to list all table entries
2. Drop all existing DuckDB schemas (clean slate)
3. Re-register all views from discovered tables

This ensures new tables appear within 30s of being written by the runner. The refresh is
idempotent — if Nessie is temporarily unavailable, the previous views remain until the next
successful refresh.

A `threading.Lock` protects DDL operations (view registration) only. Read queries proceed
without holding the lock.

### Arrow IPC for result serialization

Query results are serialized as Arrow IPC stream format (`pa.ipc.new_stream`) and sent as
`bytes` in the gRPC response. ratd deserializes using Apache Arrow Go (`apache/arrow-go/v18`)
and converts to `[]map[string]interface{}` for JSON REST responses.

Alternatives considered:
- **JSON in proto**: Higher overhead, lossy type conversion
- **Parquet**: Overkill for interactive query results (designed for storage, not transfer)
- **Arrow IPC**: Zero-copy-friendly, schema-preserving, well-supported in both Python and Go

### gRPC (not ConnectRPC) for Python side

The Python side uses standard `grpcio` (synchronous), matching the runner's pattern. The Go side
uses ConnectRPC client which is wire-compatible with standard gRPC servers.

## Consequences

### Positive
- Single DuckDB connection — fast query startup, no extension loading per request
- Nessie-authoritative discovery — consistent with Iceberg catalog semantics
- Arrow IPC — efficient binary transfer with type fidelity
- Background refresh — new tables appear automatically, no manual registration
- 52 unit tests, 87% coverage — well-tested from day one

### Negative
- 30s refresh delay for new tables (acceptable for interactive use)
- `drop_all_views()` on every refresh causes brief window where views are missing (sub-millisecond)
- No query result caching (DuckDB is fast enough for interactive use)
- Single connection limits max concurrent heavy queries (DuckDB internal parallelism helps)
- `read_parquet()` glob, not `iceberg_scan()` — same MVP limitation as runner's `ref()`

## Implementation

```
query/src/rat_query/
├── __main__.py      # Entrypoint: sys.path setup, logging config, serve()
├── server.py        # QueryServiceImpl (4 RPCs) + serve() function
├── engine.py        # QueryEngine — long-lived DuckDB, DDL lock, S3 extensions
├── catalog.py       # NessieCatalog — discovery, view registration, 30s refresh
├── arrow_ipc.py     # table_to_ipc(), columns_from_schema() helpers
├── config.py        # S3Config, NessieConfig (reuses runner's pattern)
└── gen/             # Generated gRPC stubs

platform/internal/query/
└── client.go        # Implements api.QueryStore via ConnectRPC + Arrow Go deserialization

query/tests/
├── conftest.py
└── unit/
    ├── test_config.py       # 12 tests
    ├── test_engine.py       # 8 tests
    ├── test_catalog.py      # 9 tests
    ├── test_arrow_ipc.py    # 5 tests
    └── test_server.py       # 18 tests
```

Total: 7 Python source files + 1 Go client, 52 Python tests + 8 Go tests.
