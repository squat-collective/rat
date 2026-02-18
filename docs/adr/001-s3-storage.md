# ADR-001: S3-Backed StorageStore via MinIO Go SDK

## Status: Accepted

## Context

The web IDE (portal) needs file operations — listing, reading, writing, and deleting pipeline
code, configs, tests, and documentation. These files live in S3-compatible storage (MinIO for
Community, AWS S3 for Pro). The `ratd` Go platform needs a `StorageStore` implementation that
the REST API handlers can call.

Key requirements:
- Implement the `api.StorageStore` interface (4 methods: ListFiles, ReadFile, WriteFile, DeleteFile)
- Work with MinIO (Community) and be swappable for native S3 (Pro plugin)
- Zero-config startup — bucket created automatically if missing
- File type classification for the portal file browser (pipeline-sql, config, test, etc.)
- Content-type detection for better browser previewing / API debugging

## Decision

### SDK: MinIO Go SDK (`minio-go/v7`)

Use `github.com/minio/minio-go/v7` with static V4 credentials.

**Why not AWS SDK for Go?**
- MinIO SDK is lighter (fewer transitive deps)
- First-class MinIO support (error response parsing, bucket operations)
- S3-compatible — works with AWS S3, GCS (via interop), and any S3-compatible store
- Already proven in the v1 Python codebase (minio-py)

### Error semantics

| Operation | Not found | Other errors |
|-----------|-----------|-------------|
| `ReadFile` | `nil, nil` | `nil, error` |
| `DeleteFile` | `error` | `error` |
| `ListFiles` | `[]FileInfo{}, nil` (empty, never nil) | `nil, error` |

**Why `nil, nil` for ReadFile?** The HTTP handler checks `if file == nil { 404 }`. Returning
an error would trigger a 500, which is wrong for "file doesn't exist". This matches the
Postgres store pattern where `GetRun` returns `nil, nil` on `pgx.ErrNoRows`.

**Why error for DeleteFile?** The handler returns 500 on error, which is acceptable for
"tried to delete something that doesn't exist". A 404-style check could be added later in
the handler layer if needed.

### Bucket auto-creation

`NewS3Store()` calls `ensureBucket()` which checks if the bucket exists and creates it if
not. This means `docker compose up` just works with zero manual setup.

### Content-type detection

Files are written with an appropriate `Content-Type` header based on extension:
- `.sql` → `application/sql`
- `.py` → `text/x-python`
- `.yaml` / `.yml` → `application/x-yaml`
- `.json` → `application/json`
- `.md` → `text/markdown`
- Default → `application/octet-stream`

This improves browser preview when accessing MinIO console and aids API debugging.

### File type classification

`ListFiles` returns a `Type` field on each `FileInfo`, derived from the file path/name:
- `pipeline.sql` or `*.sql` → `pipeline-sql`
- `pipeline.py` → `pipeline-py`
- `config.yaml` / `schema.yaml` → `config`
- `*.meta.yaml` → `meta`
- `*.md` / `*.txt` → `doc`
- Files in `tests/` or `test_*` prefix → `test`
- Files in `hooks/` or `hook_*` / `pre_*` / `post_*` prefix → `hook`

This drives the portal file browser's icon/color differentiation.

### File upload flow

Files are uploaded **through ratd** (multipart POST, 32MB max), not via presigned URLs
directly to MinIO. Rationale:
- Simpler auth model — Community has no auth, Pro only needs to check the ratd token
- No need for presigned URL generation, expiry management, or CORS configuration
- 32MB is generous for code files (typical pipeline.sql is <10KB)
- Pro Edition could add direct-to-S3 via presigned URLs later if needed for large files

## Consequences

### Positive
- Zero-config: `docker compose up` creates bucket automatically
- Portal file operations work immediately with no manual MinIO setup
- Content-type makes files browsable in MinIO console
- File type classification enables rich UI without client-side path parsing
- Integration tests run against real MinIO (port 9002) in `make test-integration`

### Negative
- `minio-go/v7` adds ~10 transitive dependencies to `go.mod`
- Upload goes through ratd (extra hop vs presigned URL) — acceptable for code files
- File type detection is heuristic-based — edge cases possible for unusual filenames

## Implementation

- `platform/internal/storage/s3store.go` — `S3Store` struct
- `platform/internal/storage/helpers.go` — `detectFileType()`, `detectContentType()`
- `platform/internal/storage/s3store_test.go` — 8 integration tests
- `platform/cmd/ratd/main.go` — wired via `S3_ENDPOINT` env var
- `infra/docker-compose.test.yml` — `minio-test` service on port 9002
