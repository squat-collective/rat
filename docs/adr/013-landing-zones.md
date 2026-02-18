# ADR-013: Landing Zones — Standalone File Drop Areas

## Status: Accepted

## Context

Users need to upload raw data files (CSV, JSON, Parquet, Excel) to the platform before pipelines exist to consume them. The existing `POST /files/upload` endpoint writes to the S3 namespace tree but provides no metadata tracking, no file management UI, and no organized storage areas.

We considered two approaches:

1. **Pipeline-coupled upload**: Files are attached to a pipeline's ingestion config (requires pipeline-first workflow)
2. **Standalone landing zones**: Independent file storage areas that exist before and independently of pipelines

Standalone zones match user workflow better — data arrives before the pipeline is designed.

## Decision

### Landing zones as first-class entities

- **Independent of pipelines**: Zones exist at the namespace level, not tied to any pipeline. Users create a zone, upload files, preview them via DuckDB, then reference them in pipeline SQL manually.
- **Postgres-tracked metadata**: Zone definitions (name, namespace, description, owner) and file metadata (filename, size, content type, S3 path) stored in Postgres. S3 is storage, Postgres is the registry.
- **S3 path convention**: `{namespace}/landing/{zoneName}/{filename}` — separate from the pipeline tree `{namespace}/pipelines/...`
- **DuckDB preview via existing query endpoint**: No new backend for preview. `read_csv_auto('s3://rat/{path}')` through `POST /api/v1/query` auto-detects schemas.

### API design

8 endpoints under `/api/v1/landing-zones`:

- Zone CRUD: list (with namespace filter), get, create, delete
- File operations: list files, upload (multipart), get metadata, delete
- Zone delete cascades: removes all files from S3 and DB

Upload uses the same `maxUploadSize` (32MB) and pattern as the existing `HandleUploadFile`.

### Key design choices

1. **No triggers/auto-ingest**: Landing zones are passive storage. Trigger mechanisms (cron-ingest, file-watcher) are a separate abstraction for a future version.
2. **Binary-safe via Go strings**: Go's `string` holds arbitrary bytes, so `StorageStore.WriteFile(path, string(content))` works for binary files (Parquet, Excel) without new storage interface methods.
3. **LEFT JOIN file stats**: Zone list/get queries use `LEFT JOIN landing_files` to return `file_count` and `total_bytes` without N+1 queries.
4. **Cascade delete**: `ON DELETE CASCADE` on `landing_files.zone_id` ensures DB consistency; handler also cleans up S3 objects.

## Consequences

### Positive

- Users can upload and preview data before writing pipelines — unblocks exploration workflow
- File metadata is queryable (list files, filter by zone, check sizes)
- Preview reuses existing DuckDB query infrastructure — zero new backend services
- Clean separation from pipeline storage tree

### Negative

- No automatic ingestion — users must manually reference zone files in pipeline SQL
- File deduplication not implemented — uploading the same filename overwrites silently
- 32MB per-file limit (same as existing upload constraint)

### Neutral

- Future trigger/ingestion mechanisms can reference landing zone files by S3 path without schema changes
