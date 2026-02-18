# ratd REST API Specification

> Base URL: `http://localhost:8080/api/v1`
> Auth: None (Community). Bearer token (Pro, via auth plugin).
> Format: JSON request/response. Arrow IPC for query results.

---

## Error Envelope

All API errors use a structured JSON envelope so the SDK only needs to handle one shape:

```json
{
  "error": {
    "code": "ERROR_CODE",
    "message": "human-readable message"
  }
}
```

Common error codes: `INVALID_ARGUMENT`, `NOT_FOUND`, `ALREADY_EXISTS`, `INTERNAL`.

---

## Pagination

Endpoints that return lists support pagination via query params:

| Param | Default | Max | Description |
|-------|---------|-----|-------------|
| `limit` | 50 | 200 | Number of items to return |
| `offset` | 0 | - | Number of items to skip |

---

## Health

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Service health check (unauthenticated, outside /api/v1) |
| GET | `/features` | Active plugins, edition, capabilities |

### GET /health

Unauthenticated. Used by Docker health checks and load balancers. Mounted at root, not under `/api/v1`.

```json
// Response: 200
{ "status": "healthy" }
```

### GET /features

Returns the active platform capabilities. The portal uses this to show/hide UI elements based on active plugins.

```json
// Response: 200
{
  "edition": "community",
  "plugins": {
    "auth": { "enabled": false },
    "sharing": { "enabled": false },
    "executor": { "enabled": true, "type": "warmpool" },
    "audit": { "enabled": false },
    "enforcement": { "enabled": false }
  },
  "namespaces": false,
  "multi_user": false,
  "landing_zones": true,
  "license": null
}
```

---

## Pipelines

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/pipelines` | List all pipelines (filterable by namespace, layer) |
| GET | `/pipelines/:namespace/:layer/:name` | Get pipeline details + metadata |
| POST | `/pipelines` | Create a new pipeline (scaffolds S3 files) |
| PUT | `/pipelines/:namespace/:layer/:name` | Update pipeline config |
| DELETE | `/pipelines/:namespace/:layer/:name` | Delete pipeline + S3 files |

### GET /pipelines

Query params: `?namespace=default&layer=silver&limit=50&offset=0`

Pagination is pushed to SQL via LIMIT/OFFSET.

```json
// Response: 200
{
  "pipelines": [
    {
      "namespace": "default",
      "layer": "silver",
      "name": "orders",
      "type": "sql",
      "owner": null,
      "created_at": "2026-02-12T10:00:00Z",
      "last_run": {
        "run_id": "abc123",
        "status": "success",
        "finished_at": "2026-02-12T14:00:00Z"
      }
    }
  ],
  "total": 1
}
```

### POST /pipelines

```json
// Request
{
  "namespace": "default",
  "layer": "silver",
  "name": "orders",
  "type": "sql",
  "source": "raw_orders",
  "unique_key": "id",
  "description": "Clean and deduplicate orders"
}

// Response: 201
{
  "namespace": "default",
  "layer": "silver",
  "name": "orders",
  "s3_path": "default/pipelines/silver/orders/",
  "files_created": ["pipeline.sql", "config.yaml"]
}
```

| Status | Condition |
|--------|-----------|
| 201 | Pipeline created |
| 400 | Missing required fields, invalid name/layer |
| 409 | Pipeline already exists |

### PUT /pipelines/:namespace/:layer/:name

```json
// Request (partial update)
{
  "description": "Updated description",
  "type": "python",
  "owner": "user-id"
}

// Response: 200 — full pipeline object
```

Requires `write` access to the pipeline (Pro: ownership/ACL check).

### DELETE /pipelines/:namespace/:layer/:name

Soft-deletes the pipeline record and cleans up S3 files under the pipeline prefix.

Requires `delete` access to the pipeline (Pro: ownership/ACL check).

```
Response: 204 No Content
```

---

## Runs

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/runs` | List runs (filterable by pipeline, status) |
| GET | `/runs/:run_id` | Get run details |
| POST | `/runs` | Trigger a pipeline run |
| POST | `/runs/:run_id/cancel` | Cancel a running pipeline |
| GET | `/runs/:run_id/logs` | Get run logs (SSE stream or JSON) |

### GET /runs

Query params: `?namespace=default&layer=silver&pipeline=orders&status=running&limit=50&offset=0`

```json
// Response: 200
{
  "runs": [...],
  "total": 42
}
```

### POST /runs

```json
// Request
{
  "namespace": "default",
  "layer": "silver",
  "pipeline": "orders",
  "trigger": "manual"
}

// Response: 202
{
  "run_id": "abc123",
  "status": "pending"
}
```

Requires `write` access to the pipeline. If the cloud plugin is enabled, scoped credentials are injected for the run.

| Status | Condition |
|--------|-----------|
| 202 | Run created and dispatched |
| 400 | Missing required fields, invalid name/layer |
| 404 | Pipeline not found |

### POST /runs/:run_id/cancel

```json
// Response: 200
{
  "run_id": "abc123",
  "status": "cancelled"
}
```

| Status | Condition |
|--------|-----------|
| 200 | Run cancelled |
| 404 | Run not found |
| 409 | Run is not cancellable (already finished) |

### GET /runs/:run_id/logs

Server-Sent Events stream (when `Accept: text/event-stream`):

```
event: log
data: {"timestamp": "2026-02-12T14:00:01Z", "level": "info", "message": "Starting pipeline silver.orders"}

event: log
data: {"timestamp": "2026-02-12T14:00:02Z", "level": "info", "message": "Executing SQL..."}

event: status
data: {"status": "success", "rows_written": 12340, "duration_ms": 4500}
```

JSON fallback (default Accept header):

```json
// Response: 200
{
  "logs": [
    {"timestamp": "...", "level": "info", "message": "..."}
  ],
  "status": "success"
}
```

For active runs, the SSE stream keeps the connection open and polls for new logs every 2 seconds until the run reaches a terminal state.

---

## Query

> **Dispatch**: All query endpoints proxy to `ratq` (Python DuckDB sidecar) via gRPC/ConnectRPC.
> ratd deserializes Arrow IPC responses into JSON for REST clients.
> When `RATQ_ADDR` is not set, query endpoints return 500 (nil QueryStore).
> See `docs/adr/006-query-service.md` for architecture.

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/query` | Execute interactive SQL query |
| GET | `/schema` | Get all tables with their column schemas (bulk) |
| GET | `/tables` | List all tables (filterable by namespace, layer) |
| GET | `/tables/:namespace/:layer/:name` | Get table schema + stats |
| GET | `/tables/:namespace/:layer/:name/preview` | Preview first N rows |
| PUT | `/tables/:namespace/:layer/:name/metadata` | Update table metadata (description, owner, column descriptions) |

### POST /query

```json
// Request
{
  "sql": "SELECT * FROM silver.orders WHERE amount > 100 LIMIT 10",
  "namespace": "default",
  "limit": 1000
}

// Response: 200
{
  "columns": [
    { "name": "id", "type": "VARCHAR" },
    { "name": "amount", "type": "DECIMAL(14,2)" }
  ],
  "rows": [...],
  "total_rows": 10,
  "duration_ms": 45
}
```

| Status | Condition |
|--------|-----------|
| 200 | Query executed |
| 400 | Missing SQL, query too long (max 100KB) |

### GET /schema

Returns all tables with their column schemas in a single call. Uses bulk fetch to avoid N+1 gRPC calls.

```json
// Response: 200
{
  "tables": [
    {
      "namespace": "default",
      "layer": "silver",
      "name": "orders",
      "columns": [
        { "name": "id", "type": "VARCHAR" },
        { "name": "amount", "type": "DECIMAL(14,2)" }
      ]
    }
  ]
}
```

### GET /tables

Query params: `?namespace=default&layer=silver`

Tables are enriched with metadata descriptions when a TableMetadataStore is configured.

```json
// Response: 200
{
  "tables": [
    {
      "namespace": "default",
      "layer": "silver",
      "name": "orders",
      "row_count": 12340,
      "size_bytes": 524288,
      "description": "Cleaned orders table"
    }
  ],
  "total": 1
}
```

### GET /tables/:namespace/:layer/:name

```json
// Response: 200
{
  "namespace": "default",
  "layer": "silver",
  "name": "orders",
  "row_count": 12340,
  "size_bytes": 524288,
  "description": "Cleaned orders table",
  "owner": "user-id",
  "columns": [
    { "name": "id", "type": "VARCHAR", "description": "Primary key" },
    { "name": "amount", "type": "DECIMAL(14,2)", "description": "Order total" }
  ]
}
```

### GET /tables/:namespace/:layer/:name/preview

Returns the first 50 rows of the table.

```json
// Response: 200
{
  "columns": [...],
  "rows": [...],
  "total_rows": 50,
  "duration_ms": 12
}
```

### PUT /tables/:namespace/:layer/:name/metadata

Only available when a TableMetadataStore is configured.

```json
// Request (partial update)
{
  "description": "Cleaned orders from raw data",
  "owner": "user-id",
  "column_descriptions": {
    "id": "Primary key",
    "amount": "Order total in USD"
  }
}

// Response: 200 — full TableMetadata object
{
  "namespace": "default",
  "layer": "silver",
  "name": "orders",
  "description": "Cleaned orders from raw data",
  "owner": "user-id",
  "column_descriptions": {
    "id": "Primary key",
    "amount": "Order total in USD"
  }
}
```

---

## Storage (S3 Files)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/files` | List files in a path (S3 prefix) |
| POST | `/files/upload` | Multipart file upload (max 32MB) |
| GET | `/files/*path` | Read file content |
| PUT | `/files/*path` | Write/update file content |
| DELETE | `/files/*path` | Delete file |

### GET /files

Query params: `?prefix=default/pipelines/silver/orders/&exclude=landing,data`

The `prefix` parameter is required and must start with a valid namespace. The optional `exclude` parameter is a comma-separated list of path segments to filter out.

```json
// Response: 200
{
  "files": [
    { "path": "default/pipelines/silver/orders/pipeline.sql", "size": 245, "modified": "2026-02-12T10:00:00Z", "type": "pipeline-sql", "version_id": "abc123" },
    { "path": "default/pipelines/silver/orders/config.yaml", "size": 180, "modified": "2026-02-12T10:00:00Z", "type": "config" }
  ]
}
```

### POST /files/upload

Multipart form upload. Fields: `path` (destination), `file` (uploaded content). Max 32MB.

```
Content-Type: multipart/form-data

path: default/pipelines/silver/orders/data.csv
file: <binary>
```

```json
// Response: 201
{
  "path": "default/pipelines/silver/orders/data.csv",
  "filename": "data.csv",
  "size": 1024,
  "status": "uploaded",
  "version_id": "abc123"
}
```

### GET /files/*path

```json
// Response: 200
{
  "path": "default/pipelines/silver/orders/pipeline.sql",
  "content": "SELECT * FROM {{ ref('bronze.raw_orders') }} ...",
  "size": 245,
  "modified": "2026-02-12T10:00:00Z",
  "version_id": "abc123"
}
```

### PUT /files/*path

Writing a file under a pipeline's S3 prefix automatically marks the pipeline as draft-dirty.

```json
// Request
{ "content": "SELECT * FROM {{ ref('bronze.raw_orders') }}" }

// Response: 200
{
  "path": "default/pipelines/silver/orders/pipeline.sql",
  "status": "written",
  "version_id": "abc123"
}
```

### File Type Classification

The `type` field in `GET /files` responses is auto-detected from the file path:

| Type | Matches |
|------|---------|
| `pipeline-sql` | `pipeline.sql`, any `*.sql` file |
| `pipeline-py` | `pipeline.py` |
| `config` | `config.yaml`, `config.yml`, `schema.yaml`, `schema.yml` |
| `meta` | `*.meta.yaml`, `*.meta.yml`, `meta.yaml` |
| `doc` | `*.md`, `*.txt`, `*.rst`, `README` |
| `test` | Files in `tests/` or `test/` dirs, `test_*` prefix, `*_test.go`, `*_test.py` |
| `hook` | Files in `hooks/` dir, `hook_*`, `pre_*`, `post_*` prefixes |
| _(empty)_ | Unrecognized files |

### Path Validation

All file paths are validated:
- Must not be empty
- Must not contain `..` (path traversal)
- Must be relative (no leading `/`)
- Must not contain null bytes or backslashes
- Must start with a valid namespace segment

### Error Responses

| Endpoint | Condition | Status | Body |
|----------|-----------|--------|------|
| `GET /files/*path` | File not found | 404 | `file not found` |
| `GET /files/*path` | S3 error | 500 | Error message |
| `PUT /files/*path` | Invalid JSON body | 400 | `invalid request body` |
| `DELETE /files/*path` | S3 error | 500 | Error message |
| `POST /files/upload` | File > 32MB | 413 | `file too large (max 32MB)` |
| `POST /files/upload` | Missing `path` field | 400 | `path form field is required` |
| `POST /files/upload` | Missing `file` field | 400 | `file form field is required` |
| `POST /files/upload` | Path traversal | 400 | Validation message |

---

## Schedules

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/schedules` | List all schedules |
| GET | `/schedules/:id` | Get schedule details |
| POST | `/schedules` | Create a schedule for a pipeline |
| PUT | `/schedules/:id` | Update schedule (cron, enabled) |
| DELETE | `/schedules/:id` | Delete schedule |

### POST /schedules

```json
// Request
{
  "namespace": "default",
  "layer": "silver",
  "pipeline": "orders",
  "cron": "0 * * * *",
  "enabled": true
}

// Response: 201
{
  "id": "sched-123",
  "cron": "0 * * * *",
  "enabled": true
}
```

| Status | Condition |
|--------|-----------|
| 201 | Schedule created |
| 400 | Missing fields, invalid name/layer, invalid cron expression |
| 404 | Pipeline not found |

### PUT /schedules/:id

```json
// Request (partial update)
{
  "cron": "*/15 * * * *",
  "enabled": false
}

// Response: 200 — full schedule object
```

### DELETE /schedules/:id

```
Response: 204 No Content
```

---

## Quality Tests

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/tests/:namespace/:layer/:pipeline` | List quality tests for a pipeline |
| POST | `/tests/:namespace/:layer/:pipeline` | Create a quality test (writes SQL file) |
| DELETE | `/tests/:namespace/:layer/:pipeline/:test_name` | Delete a quality test |
| POST | `/tests/:namespace/:layer/:pipeline/run` | Run quality tests for a pipeline |

### GET /tests/:namespace/:layer/:pipeline

Tests are annotated with `published` status based on the pipeline's PublishedVersions.

```json
// Response: 200
{
  "tests": [
    {
      "name": "no-null-ids",
      "sql": "SELECT count(*) FROM ...",
      "severity": "error",
      "description": "IDs must not be null",
      "published": true,
      "tags": ["data-quality"],
      "remediation": "Check upstream data source"
    }
  ],
  "total": 1
}
```

### POST /tests/:namespace/:layer/:pipeline

```json
// Request
{
  "name": "no-null-ids",
  "sql": "SELECT count(*) FROM {{ ref('silver.orders') }} WHERE id IS NULL",
  "severity": "error",
  "description": "IDs must not be null"
}

// Response: 201
{
  "name": "no-null-ids",
  "severity": "error",
  "path": "default/pipelines/silver/orders/tests/quality/no-null-ids.sql"
}
```

| Status | Condition |
|--------|-----------|
| 201 | Test created |
| 400 | Missing name/sql, invalid test name |
| 409 | Test already exists |

### DELETE /tests/:namespace/:layer/:pipeline/:test_name

```
Response: 204 No Content
```

### POST /tests/:namespace/:layer/:pipeline/run

```json
// Response: 200
{
  "results": [
    {
      "name": "no-null-ids",
      "status": "passed",
      "severity": "error",
      "value": 0,
      "expected": 0,
      "duration_ms": 120
    }
  ],
  "passed": 1,
  "failed": 0,
  "total": 1
}
```

---

## Metadata

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/metadata/:namespace/pipeline/:layer/:name` | Get pipeline.meta.yaml data |
| GET | `/metadata/:namespace/quality/:layer/:name` | Get quality.meta.yaml data |

### GET /metadata/:namespace/pipeline/:layer/:name

Reads the `pipeline.meta.yaml` sidecar file from S3.

```json
// Response: 200
{
  "path": "default/pipelines/silver/orders/pipeline.meta.yaml",
  "content": "description: Clean orders\ntags: [orders, silver]"
}
```

| Status | Condition |
|--------|-----------|
| 200 | Metadata found |
| 404 | Metadata file not found |

### GET /metadata/:namespace/quality/:layer/:name

Reads the `quality.meta.yaml` sidecar file from S3.

```json
// Response: 200
{
  "path": "default/pipelines/silver/orders/tests/quality.meta.yaml",
  "content": "tests:\n  no-null-ids:\n    description: ..."
}
```

---

## Namespaces (Pro only -- no-op in Community)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/namespaces` | List namespaces |
| POST | `/namespaces` | Create namespace |
| PUT | `/namespaces/:name` | Update namespace description |
| DELETE | `/namespaces/:name` | Delete namespace |

### GET /namespaces

```json
// Response: 200
{
  "namespaces": [
    { "name": "default", "description": "", "created_at": "..." }
  ],
  "total": 1
}
```

### POST /namespaces

```json
// Request
{ "name": "analytics" }

// Response: 201
{ "name": "analytics" }
```

| Status | Condition |
|--------|-----------|
| 201 | Namespace created |
| 400 | Missing/invalid name |
| 409 | Namespace already exists |

### PUT /namespaces/:name

```json
// Request
{ "description": "Analytics team namespace" }

// Response: 204 No Content
```

### DELETE /namespaces/:name

The "default" namespace cannot be deleted (returns 403). Requires `delete` access (Pro: ownership/ACL check).

```
Response: 204 No Content
```

---

## Landing Zones

Standalone file drop areas for raw data uploads. Files are stored in S3 at `{namespace}/landing/{zoneName}/{filename}`. Landing zone data is consumed by pipelines that reference these S3 paths in their SQL (e.g., `read_csv_auto('s3://rat/{namespace}/landing/{zone}/*.csv')`). Note: direct file access functions (`read_csv_auto`, `read_parquet`, etc.) are blocked in interactive queries via ratq for security — use pipeline SQL to process landing zone data.

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/landing-zones` | List zones (filterable by namespace). Returns file count + total bytes. |
| POST | `/landing-zones` | Create a landing zone |
| GET | `/landing-zones/:ns/:name` | Get zone details with file stats |
| PUT | `/landing-zones/:ns/:name` | Update zone (description, owner, expected schema) |
| DELETE | `/landing-zones/:ns/:name` | Delete zone + all files (S3 + DB) |
| GET | `/landing-zones/:ns/:name/files` | List files in a zone |
| POST | `/landing-zones/:ns/:name/files` | Upload file (multipart, max 32MB) |
| GET | `/landing-zones/:ns/:name/files/:fileID` | Get file metadata |
| DELETE | `/landing-zones/:ns/:name/files/:fileID` | Delete file (S3 + DB) |
| GET | `/landing-zones/:ns/:name/samples` | List sample files for a zone |
| POST | `/landing-zones/:ns/:name/samples` | Upload sample file (multipart, max 32MB) |
| DELETE | `/landing-zones/:ns/:name/samples/:filename` | Delete a sample file |

### POST /landing-zones

```json
// Request
{
  "namespace": "default",
  "name": "raw-uploads",
  "description": "CSV files from partners"
}

// Response: 201
{
  "id": "zone-uuid",
  "namespace": "default",
  "name": "raw-uploads",
  "description": "CSV files from partners",
  "created_at": "2026-02-13T10:00:00Z"
}
```

| Status | Condition |
|--------|-----------|
| 201 | Zone created |
| 400 | Missing/invalid namespace or name |
| 409 | Zone already exists |

### PUT /landing-zones/:ns/:name

```json
// Request (partial update)
{
  "description": "Updated description",
  "owner": "user-id",
  "expected_schema": "id:int,name:varchar,amount:decimal"
}

// Response: 200 — full zone object
```

### DELETE /landing-zones/:ns/:name

Deletes all files from S3 (including `_samples/` folder), then deletes the zone from the database.

```
Response: 204 No Content
```

### POST /landing-zones/:ns/:name/files

Multipart form upload. Field: `file` (uploaded content). Filename taken from multipart header, prepended with UTC timestamp to avoid collisions. Max 32MB.

When triggers are configured, file uploads asynchronously evaluate matching `landing_zone_upload` and `file_pattern` triggers.

```json
// Response: 201
{
  "id": "file-uuid",
  "zone_id": "zone-uuid",
  "filename": "20260213_100500_orders.csv",
  "s3_path": "default/landing/raw-uploads/20260213_100500_orders.csv",
  "size_bytes": 1024,
  "content_type": "text/csv",
  "uploaded_at": "2026-02-13T10:05:00Z"
}
```

### GET /landing-zones/:ns/:name/files

```json
// Response: 200
{
  "files": [...],
  "total": 5
}
```

### GET /landing-zones/:ns/:name/files/:fileID

```json
// Response: 200 — full file metadata object
```

### DELETE /landing-zones/:ns/:name/files/:fileID

Deletes from S3 and the database.

```
Response: 204 No Content
```

### GET /landing-zones/:ns/:name/samples

Lists sample files stored in the `_samples/` subfolder of the landing zone's S3 prefix. Samples are curated reference files (not append-only like regular uploads).

```json
// Response: 200
{
  "files": [
    { "path": "default/landing/raw-uploads/_samples/sample.csv", "size": 512, "modified": "...", "type": "" }
  ],
  "total": 1
}
```

### POST /landing-zones/:ns/:name/samples

Multipart form upload. Field: `file`. Filename is used as-is (no timestamp prefix). Overwrites existing file with the same name.

```json
// Response: 201
{
  "path": "default/landing/raw-uploads/_samples/sample.csv",
  "filename": "sample.csv",
  "size": 512,
  "status": "uploaded"
}
```

### DELETE /landing-zones/:ns/:name/samples/:filename

```
Response: 204 No Content
```

---

## Lineage

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/lineage` | Get full lineage DAG (pipelines, tables, landing zones) |

### GET /lineage

Query params: `?namespace=default` (optional, filters to a single namespace)

Builds the lineage graph by:
1. Listing all pipelines
2. Reading pipeline SQL/Python files in parallel (bounded to 20 concurrent reads) to extract `ref()` and `landing_zone()` dependencies
3. Batch-fetching latest runs, quality test counts, tables, and landing zones in parallel
4. Constructing the DAG with nodes and edges

```json
// Response: 200
{
  "nodes": [
    {
      "id": "pipeline:default.silver.orders",
      "type": "pipeline",
      "namespace": "default",
      "layer": "silver",
      "name": "orders",
      "latest_run": {
        "id": "run-uuid",
        "status": "success",
        "started_at": "2026-02-12T14:00:00Z",
        "duration_ms": 4500
      },
      "quality": {
        "total": 3,
        "passed": 3,
        "failed": 0,
        "warned": 0
      }
    },
    {
      "id": "table:default.silver.orders",
      "type": "table",
      "namespace": "default",
      "layer": "silver",
      "name": "orders",
      "table_stats": {
        "row_count": 12340,
        "size_bytes": 524288
      }
    },
    {
      "id": "table:default.bronze.raw_orders",
      "type": "table",
      "namespace": "default",
      "layer": "bronze",
      "name": "raw_orders",
      "table_stats": { "row_count": 15000, "size_bytes": 800000 }
    },
    {
      "id": "landing:default.raw-uploads",
      "type": "landing_zone",
      "namespace": "default",
      "name": "raw-uploads",
      "landing_info": { "file_count": 5 }
    }
  ],
  "edges": [
    {
      "source": "table:default.bronze.raw_orders",
      "target": "pipeline:default.silver.orders",
      "type": "ref"
    },
    {
      "source": "pipeline:default.silver.orders",
      "target": "table:default.silver.orders",
      "type": "produces"
    },
    {
      "source": "landing:default.raw-uploads",
      "target": "pipeline:default.bronze.ingest",
      "type": "landing_input"
    }
  ]
}
```

**Node types**: `pipeline`, `table`, `landing_zone`

**Edge types**:
- `ref` — table is read by a pipeline (via `ref()` in SQL/Python)
- `produces` — pipeline writes to a table (convention: same ns.layer.name)
- `landing_input` — landing zone feeds a pipeline (via `landing_zone()` in SQL/Python)

Orphan tables (not produced by any pipeline) and orphan landing zones (not referenced by any pipeline) are included as disconnected nodes.

---

## Triggers

Pipeline triggers define automated conditions for running a pipeline. Triggers are nested under a pipeline resource.

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/pipelines/:ns/:layer/:name/triggers` | List triggers for a pipeline |
| POST | `/pipelines/:ns/:layer/:name/triggers` | Create a trigger |
| GET | `/pipelines/:ns/:layer/:name/triggers/:triggerID` | Get trigger details |
| PUT | `/pipelines/:ns/:layer/:name/triggers/:triggerID` | Update trigger config/enabled/cooldown |
| DELETE | `/pipelines/:ns/:layer/:name/triggers/:triggerID` | Delete a trigger |

Only available when the PipelineTriggerStore is configured.

### Trigger Types

| Type | Config Schema | Description |
|------|---------------|-------------|
| `landing_zone_upload` | `{ "namespace": "...", "zone_name": "..." }` | Fires when a file is uploaded to the specified landing zone |
| `cron` | `{ "cron_expr": "0 * * * *" }` | Fires on a cron schedule (5-field cron) |
| `pipeline_success` | `{ "namespace": "...", "layer": "...", "pipeline": "..." }` | Fires when the specified upstream pipeline completes successfully |
| `webhook` | _(auto-generated)_ | Fires when a webhook request is received with the correct token |
| `file_pattern` | `{ "namespace": "...", "zone_name": "...", "pattern": "*.csv" }` | Fires when an uploaded file matches the glob pattern |
| `cron_dependency` | `{ "cron_expr": "0 * * * *", "dependencies": ["ns.layer.pipeline"] }` | Fires on cron schedule only if all dependency pipelines have succeeded |

### GET /pipelines/:ns/:layer/:name/triggers

```json
// Response: 200
{
  "triggers": [
    {
      "id": "trigger-uuid",
      "pipeline_id": "pipeline-uuid",
      "type": "landing_zone_upload",
      "config": { "namespace": "default", "zone_name": "raw-uploads" },
      "enabled": true,
      "cooldown_seconds": 60,
      "last_triggered_at": "2026-02-13T10:05:00Z",
      "last_run_id": "run-uuid",
      "created_at": "2026-02-12T10:00:00Z",
      "updated_at": "2026-02-12T10:00:00Z"
    }
  ],
  "total": 1
}
```

Webhook triggers include an additional `webhook_url` field pointing to `POST /api/v1/webhooks`.

### POST /pipelines/:ns/:layer/:name/triggers

```json
// Request
{
  "type": "landing_zone_upload",
  "config": {
    "namespace": "default",
    "zone_name": "raw-uploads"
  },
  "enabled": true,
  "cooldown_seconds": 60
}

// Response: 201 — trigger object
```

For `webhook` triggers, the token is auto-generated. The plaintext token is returned **once** in the creation response as `webhook_token` and is never stored or shown again.

```json
// Webhook creation response: 201
{
  "id": "trigger-uuid",
  "type": "webhook",
  "config": { "token_hash": "sha256hex..." },
  "webhook_url": "http://localhost:8080/api/v1/webhooks",
  "webhook_token": "64-char-hex-plaintext-shown-once",
  "..."
}
```

| Status | Condition |
|--------|-----------|
| 201 | Trigger created |
| 400 | Missing/invalid type, invalid config, invalid cron expression, invalid glob pattern |
| 404 | Pipeline/landing zone/upstream pipeline not found |

### PUT /pipelines/:ns/:layer/:name/triggers/:triggerID

```json
// Request (partial update)
{
  "config": { "namespace": "default", "zone_name": "new-zone" },
  "enabled": false,
  "cooldown_seconds": 120
}

// Response: 200 — full trigger object
```

### DELETE /pipelines/:ns/:layer/:name/triggers/:triggerID

```
Response: 204 No Content
```

---

## Webhooks

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/v1/webhooks` | Fire a webhook trigger (token-authenticated) |

Mounted **outside** the auth middleware -- the webhook token IS the authentication. Token is passed via header (not URL) for security.

### POST /api/v1/webhooks

Authentication via header: `X-Webhook-Token: <token>` or `Authorization: Bearer <token>`.

The plaintext token is hashed (SHA-256) before the database lookup. After retrieval, the stored hash is verified again via constant-time comparison to guard against timing side-channels.

```json
// Response: 201
{
  "run_id": "run-uuid"
}
```

| Status | Condition |
|--------|-----------|
| 201 | Webhook trigger fired, run created |
| 400 | Missing token header |
| 404 | Token not found or invalid |
| 429 | Cooldown active |

---

## Sharing (Pro only)

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/sharing` | Grant access to a resource |
| GET | `/sharing` | List access grants for a resource |
| DELETE | `/sharing/:grantID` | Revoke an access grant |
| POST | `/sharing/transfer` | Transfer resource ownership |

All sharing endpoints require authentication. Returns 501 when the sharing plugin is not loaded (Community edition).

### POST /sharing

```json
// Request
{
  "grantee_id": "user-123",
  "resource_type": "pipeline",
  "resource_id": "pipeline-uuid",
  "permission": "read"
}

// Response: 201 — grant object from sharing plugin
```

| Permission | Description |
|------------|-------------|
| `read` | View the resource |
| `write` | Modify the resource, trigger runs |
| `admin` | Full control including sharing and deletion |

| Status | Condition |
|--------|-----------|
| 201 | Access granted |
| 400 | Missing required fields |
| 401 | Authentication required |
| 501 | Sharing not available (Community) |

### GET /sharing

Query params: `?resource_type=pipeline&resource_id=pipeline-uuid`

```json
// Response: 200 — list of access grants from sharing plugin
```

### DELETE /sharing/:grantID

Revokes the access grant. Only the grant creator or resource owner can revoke.

```
Response: 204 No Content
```

### POST /sharing/transfer

Currently only supports `pipeline` resource type.

```json
// Request
{
  "resource_type": "pipeline",
  "resource_id": "pipeline-uuid",
  "to_user_id": "user-456"
}

// Response: 200
{
  "transferred": true,
  "resource_id": "pipeline-uuid",
  "new_owner": "user-456"
}
```

| Status | Condition |
|--------|-----------|
| 200 | Ownership transferred |
| 400 | Unsupported resource type |
| 401 | Authentication required |
| 403 | Only the owner can transfer ownership |
| 404 | Pipeline not found |

---

## Audit

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/audit` | List recent audit log entries |

The audit middleware automatically logs all mutating API requests (POST, PUT, DELETE) when an AuditStore is configured. Logged fields: user ID, action (HTTP method), resource (URL path), IP address, timestamp.

### GET /audit

Query params: `?limit=50&offset=0`

```json
// Response: 200
[
  {
    "id": "entry-uuid",
    "user_id": "user-123",
    "action": "post",
    "resource": "/api/v1/runs",
    "detail": "",
    "ip": "192.168.1.1",
    "created_at": "2026-02-13T10:05:00Z"
  }
]
```

| Status | Condition |
|--------|-----------|
| 200 | Entries returned |
| 404 | Audit logging not enabled |

---

## Preview

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/pipelines/:ns/:layer/:name/preview` | Preview pipeline execution (dry-run) |

### POST /pipelines/:ns/:layer/:name/preview

Executes a pipeline in preview mode: returns sample rows, profiling stats, EXPLAIN ANALYZE, and logs -- without writing to the data lake or creating a run record. Requires the executor to be available.

```json
// Request (optional body)
{
  "limit": 100,
  "sample_files": ["default/landing/raw-uploads/_samples/sample.csv"],
  "code": "SELECT * FROM {{ ref('bronze.raw_orders') }} LIMIT 10"
}
```

Query param override: `?limit=50`

Limit is capped at 1000 rows.

```json
// Response: 200
{
  "columns": [
    { "name": "id", "type": "VARCHAR" },
    { "name": "amount", "type": "DECIMAL(14,2)" }
  ],
  "rows": [...],
  "total_row_count": 12340,
  "phases": [
    { "name": "template_render", "duration_ms": 5, "metadata": {} },
    { "name": "sql_execute", "duration_ms": 120, "metadata": {} },
    { "name": "profiling", "duration_ms": 30, "metadata": {} }
  ],
  "explain_output": "EXPLAIN ANALYZE output...",
  "memory_peak_bytes": 52428800,
  "logs": [
    { "timestamp": "...", "level": "info", "message": "Rendering template..." }
  ],
  "warnings": ["Column 'legacy_id' has 45% null values"]
}
```

| Status | Condition |
|--------|-----------|
| 200 | Preview executed |
| 400 | Invalid request body |
| 404 | Pipeline not found |
| 503 | Executor not available |

---

## Publish

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/pipelines/:ns/:layer/:name/publish` | Snapshot current S3 files as the published version |

### POST /pipelines/:ns/:layer/:name/publish

Snapshots the current HEAD S3 version IDs as the "published" versions for a pipeline. Creates a version history record. Validates templates against the runner before publishing (soft dependency -- proceeds if runner is unavailable).

When a VersionStore is configured, the operation is wrapped in a database transaction (publish + version + prune).

```json
// Request (optional body)
{
  "message": "Fix null handling in orders pipeline"
}

// Response: 200
{
  "status": "published",
  "version": 3,
  "message": "Fix null handling in orders pipeline",
  "versions": {
    "default/pipelines/silver/orders/pipeline.sql": "version-id-1",
    "default/pipelines/silver/orders/config.yaml": "version-id-2"
  }
}
```

| Status | Condition |
|--------|-----------|
| 200 | Published |
| 404 | Pipeline not found |
| 422 | Template validation failed |

### Template Validation Failure (422)

```json
{
  "error": "template validation failed",
  "validation": {
    "valid": false,
    "files": [
      {
        "path": "pipeline.sql",
        "valid": false,
        "errors": ["ref('missing_table') references a table that does not exist"],
        "warnings": []
      }
    ]
  }
}
```

---

## Versions

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/pipelines/:ns/:layer/:name/versions` | List version history for a pipeline |
| GET | `/pipelines/:ns/:layer/:name/versions/:number` | Get a specific version by number |
| POST | `/pipelines/:ns/:layer/:name/rollback` | Rollback to a previous version |

Only available when a VersionStore is configured.

### GET /pipelines/:ns/:layer/:name/versions

```json
// Response: 200
{
  "versions": [
    {
      "id": "version-uuid",
      "pipeline_id": "pipeline-uuid",
      "version_number": 3,
      "message": "Fix null handling",
      "published_versions": {
        "default/pipelines/silver/orders/pipeline.sql": "version-id-1"
      },
      "created_at": "2026-02-14T10:00:00Z"
    }
  ],
  "total": 3
}
```

### GET /pipelines/:ns/:layer/:name/versions/:number

```json
// Response: 200 — single version object
```

| Status | Condition |
|--------|-----------|
| 200 | Version found |
| 400 | Invalid version number |
| 404 | Pipeline or version not found |

### POST /pipelines/:ns/:layer/:name/rollback

Creates a new version that re-pins an old version's file snapshots as the current published state. The operation is atomic when a PipelinePublisher is configured (version + publish + prune in one transaction).

Versions are pruned to keep the last `max_versions` (default 50) per pipeline.

```json
// Request
{
  "version": 2,
  "message": "Rollback to v2 due to regression"
}

// Response: 200
{
  "status": "rolled_back",
  "from_version": 2,
  "new_version": 4,
  "message": "Rollback to v2 due to regression"
}
```

| Status | Condition |
|--------|-----------|
| 200 | Rolled back |
| 400 | Invalid version number (must be >= 1) |
| 404 | Pipeline or target version not found |

---

## Retention (Admin)

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/admin/retention/config` | Get system retention config |
| PUT | `/admin/retention/config` | Update system retention config |
| GET | `/admin/retention/status` | Get reaper last-run statistics |
| POST | `/admin/retention/run` | Trigger manual reaper run |

### GET /admin/retention/config

```json
// Response: 200
{
  "config": {
    "runs_max_per_pipeline": 100,
    "runs_max_age_days": 90,
    "logs_max_age_days": 30,
    "quality_results_max_per_test": 100,
    "soft_delete_purge_days": 30,
    "stuck_run_timeout_minutes": 120,
    "audit_log_max_age_days": 365,
    "nessie_orphan_branch_max_age_hours": 6,
    "reaper_interval_minutes": 60,
    "iceberg_snapshot_max_age_days": 7,
    "iceberg_orphan_file_max_age_days": 3
  }
}
```

### PUT /admin/retention/config

Request body: same shape as `config` above.

| Status | Condition |
|--------|-----------|
| 200 | Config updated |
| 400 | Invalid config (`runs_max_per_pipeline` < 1 or `reaper_interval_minutes` < 1) |

### GET /admin/retention/status

```json
// Response: 200
{
  "last_run_at": "2026-02-16T10:00:00Z",
  "runs_pruned": 42,
  "logs_pruned": 150,
  "quality_pruned": 0,
  "pipelines_purged": 1,
  "runs_failed": 3,
  "branches_cleaned": 7,
  "lz_files_cleaned": 28,
  "audit_pruned": 0,
  "updated_at": "2026-02-16T10:01:23Z"
}
```

### POST /admin/retention/run

Returns 202 Accepted with the reaper status after run completes.

```json
// Response: 202 — ReaperStatus object
```

| Status | Condition |
|--------|-----------|
| 202 | Reaper run completed |
| 503 | Reaper not configured |

---

## Pipeline Retention

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/pipelines/{ns}/{layer}/{name}/retention` | Get pipeline retention config (system + overrides + effective) |
| PUT | `/pipelines/{ns}/{layer}/{name}/retention` | Update per-pipeline retention overrides |

### GET /pipelines/{ns}/{layer}/{name}/retention

```json
// Response: 200
{
  "system": { "runs_max_per_pipeline": 100, "..." : "..." },
  "overrides": { "runs_max_per_pipeline": 50 },
  "effective": { "runs_max_per_pipeline": 50, "..." : "..." }
}
```

`overrides` is `null` when no per-pipeline overrides are set. `effective` is the merged result.

### PUT /pipelines/{ns}/{layer}/{name}/retention

Request body: partial `RetentionConfig` -- only fields to override.

```
Response: 204 No Content
```

---

## Landing Zone Lifecycle

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/landing-zones/{ns}/{name}/lifecycle` | Get zone lifecycle settings |
| PUT | `/landing-zones/{ns}/{name}/lifecycle` | Update zone lifecycle settings |

### GET /landing-zones/{ns}/{name}/lifecycle

```json
// Response: 200
{
  "processed_max_age_days": 30,
  "auto_purge": true
}
```

### PUT /landing-zones/{ns}/{name}/lifecycle

```json
// Request
{
  "processed_max_age_days": 14,
  "auto_purge": true
}

// Response: 204 No Content
```

---

## Summary

| Group | Endpoints | Description |
|-------|-----------|-------------|
| Health | 2 | Health check + feature flags |
| Pipelines | 5 | CRUD for pipelines |
| Runs | 5 | Trigger, monitor, cancel, logs |
| Query | 6 | Interactive SQL, table browsing, schema catalog, table metadata |
| Storage | 5 | S3 file management + upload (editor backend) |
| Schedules | 5 | Cron scheduling |
| Quality | 4 | Test management + execution |
| Metadata | 2 | Pipeline + quality metadata sidecars |
| Namespaces | 4 | Namespace management (includes update) |
| Landing Zones | 12 | File drop areas with upload, samples, preview |
| Lineage | 1 | DAG graph of pipelines, tables, landing zones |
| Triggers | 5 | Pipeline trigger CRUD (cron, landing zone, webhook, etc.) |
| Webhooks | 1 | Webhook trigger execution (token-authenticated) |
| Sharing | 4 | Pro: access grants + ownership transfer |
| Audit | 1 | Audit log listing (auto-logged via middleware) |
| Preview | 1 | Pipeline dry-run with profiling |
| Publish | 1 | Snapshot S3 files as published version |
| Versions | 3 | Version history + rollback |
| Retention | 4 | Admin: system retention config + reaper |
| Pipeline Retention | 2 | Per-pipeline retention overrides |
| LZ Lifecycle | 2 | Landing zone cleanup settings |
| **Total** | **75** | |
