# RAT v2 — Master Fix Plan

> **Generated**: 2026-02-16
> **Source**: 7 code review reports totaling 307 raw findings
> **De-duplicated**: 194 unique actionable items across 11 phases
> **Branch**: `feat/ratd-health-and-pipelines`

This plan covers **every finding** from all 7 reviews (Go Platform, Python Services, TypeScript/Portal, Security Audit, Infrastructure, API/Proto, Architecture), organized from highest to lowest severity. Findings that appear in multiple reviews are consolidated under a single item with cross-references.

---

## How to Read This Plan

- Each item has a **unique ID** (e.g., `P0-01`) for tracking
- **Cross-refs** link to the original review finding IDs
- **Component** tags: `ratd` `runner` `ratq` `portal` `sdk` `proto` `infra`
- **Effort**: S (< 1 hour), M (1-4 hours), L (4-16 hours), XL (multi-day)
- Phases are meant to be executed **sequentially** — later phases may depend on earlier ones
- Within each phase, items are independent and can be parallelized

---

## Phase 0 — Critical Security & Crash Fixes

> **Priority**: P0 — Do immediately, before any other work
> **Goal**: Eliminate crash vectors, security vulnerabilities with CVSS >= 9.0, and data loss risks
> **Estimated effort**: 3-5 days

### P0-01: Harden Python exec() sandbox against type introspection escape

- **Component**: `runner`
- **Effort**: L
- **Cross-refs**: Security SEC-001, Python SEC-01, Architecture F-37
- **File**: `runner/src/rat_runner/python_exec.py`, lines 20-136
- **Problem**: The sandbox blocks `eval`, `exec`, `open`, `getattr`, but `type`, `isinstance`, `hasattr` remain available. Attackers escape via `().__class__.__bases__[0].__subclasses__()` to reach `os._wrap_close` or `subprocess.Popen`. The exposed `duckdb_conn` can write to arbitrary paths via `COPY`.
- **Fix**:
  1. Add `type` to `_BLOCKED_BUILTINS`
  2. Implement an AST visitor that rejects attribute access to dunder attributes: `__class__`, `__bases__`, `__subclasses__`, `__globals__`, `__code__`, `__builtins__`, `__mro__`
  3. Block `duckdb_conn.execute()` calls containing `COPY`, `ATTACH`, `INSTALL`, `LOAD`
  4. Write tests for each blocked vector
- **Long-term**: Migrate to RestrictedPython or run in ephemeral containers with seccomp

### P0-02: Fix SQL injection via unsanitized column names in Iceberg merge operations

- **Component**: `runner`
- **Effort**: M
- **Cross-refs**: Security SEC-002, Python SEC-02
- **Files**: `runner/src/rat_runner/iceberg.py` — lines 163-188 (`merge_iceberg`), 277-285 (`delete_insert_iceberg`), 368-409 (`scd2_iceberg`), 467-474 (`snapshot_iceberg`), 514 (`read_watermark`)
- **Problem**: Column names from `unique_key`, `watermark_column`, `partition_column` are f-string-interpolated into SQL. `unique_key: ["id; DROP TABLE --"]` in config.yaml becomes injection.
- **Fix**:
  ```python
  import re
  _SAFE_IDENTIFIER = re.compile(r"^[a-zA-Z_][a-zA-Z0-9_]*$")
  def _quote_identifier(name: str) -> str:
      if not _SAFE_IDENTIFIER.match(name):
          raise ValueError(f"Invalid SQL identifier: {name!r}")
      return f'"{name}"'
  ```
  Apply to ALL column name interpolation sites. Write tests with malicious inputs.

### P0-03: Remove hardcoded default credentials

- **Component**: `runner` `ratq` `infra`
- **Effort**: M
- **Cross-refs**: Security SEC-003, Infra #22, #23
- **Files**: `runner/src/rat_runner/config.py` (lines 20-23), `infra/docker-compose.yml` (lines 18-24, 116-117, 136-137)
- **Problem**: `minioadmin`/`minioadmin` and `rat`/`rat` hardcoded. Platform silently uses them if env vars unset.
- **Fix**:
  1. Remove default values from Python config classes — raise on missing credentials
  2. Create `.env.example` with placeholder values, add `.env` to `.gitignore`
  3. Use `${POSTGRES_PASSWORD:-}` syntax in compose (fail if empty)
  4. Use YAML anchors or `x-` extension fields for S3 credentials (define once)
  5. Add startup warning if default-looking credentials detected
  6. Add `MINIO_ROOT_USER` / `MINIO_ROOT_PASSWORD` env var substitution

### P0-04: Fix nil-pointer panic in HandleDeletePipeline

- **Component**: `ratd`
- **Effort**: S
- **Cross-refs**: Go 1.1
- **File**: `platform/internal/api/pipelines.go`, line ~258
- **Problem**: Calls `s.Storage.ListFiles()` without nil-checking `s.Storage`. Panics when S3 is absent.
- **Fix**:
  ```go
  if p.S3Path != "" && s.Storage != nil {
      files, err := s.Storage.ListFiles(ctx, p.S3Path)
  }
  ```

### P0-05: Add WriteTimeout to http.Server

- **Component**: `ratd`
- **Effort**: S
- **Cross-refs**: Go 3.1, Security SEC-014
- **File**: `platform/cmd/ratd/main.go`, lines 253-258
- **Problem**: No `WriteTimeout` — Slowloris DoS vector.
- **Fix**:
  ```go
  httpServer := &http.Server{
      Addr:              addr,
      Handler:           router,
      ReadTimeout:       60 * time.Second,
      ReadHeaderTimeout: 10 * time.Second,
      WriteTimeout:      120 * time.Second,
      IdleTimeout:       120 * time.Second,
  }
  ```

### P0-06: Extract writeJSON helper for unchecked json.Encode errors

- **Component**: `ratd`
- **Effort**: M
- **Cross-refs**: Go 1.2
- **Files**: All handler files (~80+ instances)
- **Problem**: `json.NewEncoder(w).Encode(...)` return value never checked. Partial JSON on failure.
- **Fix**: Extract helper, find-and-replace all instances:
  ```go
  func writeJSON(w http.ResponseWriter, status int, v any) {
      w.Header().Set("Content-Type", "application/json")
      w.WriteHeader(status)
      if err := json.NewEncoder(w).Encode(v); err != nil {
          slog.Error("failed to encode response", "error", err)
      }
  }
  ```

### P0-07: Add timeout to urlopen in catalog discovery

- **Component**: `ratq`
- **Effort**: S
- **Cross-refs**: Python SEC-04
- **File**: `query/src/rat_query/catalog.py`, line 51
- **Problem**: `urlopen()` blocks indefinitely on non-responsive Nessie server.
- **Fix**: `with urlopen(req, timeout=10) as resp:`

### P0-08: Move webhook token from URL path to header

- **Component**: `ratd`
- **Effort**: M
- **Cross-refs**: API SEC-02, Go 3.3, Security SEC-009
- **File**: `platform/internal/api/webhooks.go`, line 17
- **Problem**: Token in URL path gets logged by proxies, CDNs, and access logs.
- **Fix**: Move to `Authorization: Bearer <token>` or `X-Webhook-Token` header. Update SDK and docs. Use `crypto/subtle.ConstantTimeCompare()` for token validation (fixes timing attack).

### P0-09: Add RUN_STATUS_CANCELLED to proto enum

- **Component**: `proto`
- **Effort**: S
- **Cross-refs**: API PROTO-02
- **File**: `proto/common/v1/common.proto`, lines 18-24
- **Problem**: Go domain has `cancelled` status but proto lacks it. gRPC clients see `UNSPECIFIED` for cancelled runs.
- **Fix**:
  ```protobuf
  RUN_STATUS_CANCELLED = 5;
  ```
  Regenerate stubs. Update all mappers.

### P0-10: Fix wrong API endpoint for features in server-api.ts

- **Component**: `portal`
- **Effort**: S
- **Cross-refs**: TS CO-4
- **File**: `portal/src/lib/server-api.ts`, line 77
- **Problem**: Uses `/health/features` but SDK uses `/api/v1/features`. Settings page may fail.
- **Fix**: Verify correct endpoint against ratd router, unify.

### P0-11: Add debug_redact to credential fields in proto

- **Component**: `proto`
- **Effort**: S
- **Cross-refs**: API SEC-01
- **Files**: `proto/enforcement/v1/enforcement.proto` (lines 32-37), `proto/cloud/v1/cloud.proto` (lines 19-27)
- **Problem**: `secret_key` and `session_token` printed in plaintext in debug output.
- **Fix**:
  ```protobuf
  string secret_key = 2 [debug_redact = true];
  string session_token = 3 [debug_redact = true];
  ```

---

## Phase 1 — High Security Fixes

> **Priority**: P1 — Within 1-2 weeks after Phase 0
> **Goal**: Close all HIGH-severity security vulnerabilities
> **Estimated effort**: 5-8 days

### P1-01: Add basic API key authentication for community mode

- **Component**: `ratd`
- **Effort**: L
- **Cross-refs**: Security SEC-004
- **Files**: `platform/internal/auth/middleware.go`, `platform/internal/api/authorizer.go`
- **Problem**: All API endpoints completely open. Any network-reachable client can execute SQL, upload files, trigger pipelines.
- **Fix**:
  1. Add optional `RAT_API_KEY` env var
  2. When set, require `Authorization: Bearer <key>` on all endpoints
  3. Bind ratd to `127.0.0.1` by default (require explicit `0.0.0.0` for network access)

### P1-02: Add TLS support to Python gRPC servers

- **Component**: `runner` `ratq`
- **Effort**: L
- **Cross-refs**: Security SEC-005
- **Files**: `runner/src/rat_runner/server.py` (line 377), `query/src/rat_query/server.py` (line 241)
- **Problem**: All gRPC uses `add_insecure_port()`. Credentials transmitted in cleartext.
- **Fix**: Add `GRPC_TLS_CERT` / `GRPC_TLS_KEY` env vars. When present, use `add_secure_port()`.

### P1-03: Switch query engine to allowlist-based SQL filtering

- **Component**: `ratq` `ratd`
- **Effort**: L
- **Cross-refs**: Security SEC-006, API VAL-02
- **Files**: `query/src/rat_query/engine.py` (lines 167-188), `platform/internal/api/query.go` (lines 96-119)
- **Problem**: Blocklist approach has gaps. `read_parquet()`, `read_csv_auto()`, `httpfs` allow data exfiltration. CTE bypass possible. API accepts arbitrary SQL.
- **Fix**:
  1. Allow only `SELECT` and `WITH...SELECT` at API layer
  2. Block DuckDB read functions (`read_parquet`, `read_csv_auto`, `read_json_auto`) in user queries
  3. Add namespace-scoped query isolation
  4. Add query length limit (e.g., 100KB)

### P1-04: Add branch name validation and URL encoding for Nessie

- **Component**: `runner` `ratd`
- **Effort**: M
- **Cross-refs**: Security SEC-007, SEC-016, Python SEC-03
- **Files**: `runner/src/rat_runner/nessie.py` (lines 57, 84, 98), `platform/internal/reaper/nessie_client.go`
- **Problem**: Branch names interpolated into URL paths without encoding. Path traversal possible.
- **Fix**: Validate branch names against `^[a-zA-Z0-9._-]+$`. URL-encode all path parameters with `urllib.parse.quote(name, safe="")`.

### P1-05: Enforce namespace-scoped access on S3 file listing

- **Component**: `ratd`
- **Effort**: M
- **Cross-refs**: Security SEC-008
- **File**: `platform/internal/api/storage.go`, lines 62-97
- **Problem**: `HandleListFiles` accepts arbitrary prefix, returns ALL files across ALL namespaces.
- **Fix**: Extract namespace from request context, prepend to prefix, validate prefix doesn't escape namespace.

### P1-06: Hash webhook tokens at rest

- **Component**: `ratd`
- **Effort**: M
- **Cross-refs**: Security SEC-009
- **File**: `platform/internal/api/webhooks.go`, line 29
- **Problem**: Tokens stored and compared as plaintext. Timing attack possible.
- **Fix**: SHA-256 hash tokens before storage. Look up by hash. Use `crypto/subtle.ConstantTimeCompare()` for any direct comparisons.

### P1-07: Add HTTP security headers middleware

- **Component**: `ratd`
- **Effort**: M
- **Cross-refs**: Security SEC-010, SEC-020
- **File**: `platform/internal/api/router.go`
- **Fix**: Add middleware setting:
  - `X-Content-Type-Options: nosniff`
  - `X-Frame-Options: DENY`
  - `Strict-Transport-Security: max-age=31536000`
  - `Content-Security-Policy` (basic policy)

### P1-08: Add CORS origin validation

- **Component**: `ratd`
- **Effort**: S
- **Cross-refs**: Security SEC-011, API SEC-03
- **File**: `platform/internal/api/router.go`, lines 168-179
- **Problem**: `AllowCredentials: true` with configurable origins. Wildcard + credentials is dangerous.
- **Fix**: Add startup validation rejecting `*` origin when credentials are enabled.

---

## Phase 2 — High Reliability & Performance

> **Priority**: P1 — Within 2-3 weeks
> **Goal**: Fix stability issues that cause data loss, crashes, or severe degradation
> **Estimated effort**: 8-12 days

### P2-01: Implement runner crash recovery (persist minimal run state)

- **Component**: `runner`
- **Effort**: XL
- **Cross-refs**: Architecture F-02
- **File**: `runner/src/rat_runner/server.py`, line 68
- **Problem**: All run state in a Python dict. Crash loses everything. Runs appear stuck for up to 120 min.
- **Fix**:
  1. Persist run state to a local SQLite file or S3 marker
  2. Add startup reconciliation — check for in-progress runs, mark as failed
  3. Reduce stuck run timeout from 120 min to 30 min

### P2-02: Implement backup strategy

- **Component**: `infra`
- **Effort**: L
- **Cross-refs**: Architecture F-36, Infra #53
- **Problem**: No backup mechanism for Postgres, MinIO, or Nessie. Volume loss = total data loss.
- **Fix**:
  1. Add `make backup` target: `pg_dump` for Postgres, `mc mirror` for MinIO
  2. Add `make restore` target
  3. Document RTO/RPO in `docs/operations.md`
  4. Add `make backup-schedule` for cron-based automated backup

### P2-03: Add circuit breakers and retry logic

- **Component**: `ratd` `runner`
- **Effort**: L
- **Cross-refs**: Architecture F-09, Python EH-01
- **Files**: `platform/internal/executor/warmpool.go`, `runner/src/rat_runner/nessie.py`
- **Problem**: Direct HTTP/gRPC calls with no circuit breaker, retry, or backoff. Flaky Nessie/S3 cascades into total failure.
- **Fix**:
  1. Add `sony/gobreaker` for Go → runner/ratq calls
  2. Add retry with exponential backoff on Nessie HTTP calls in runner
  3. Add request-level timeouts on all gRPC client calls

### P2-04: Implement backpressure (pending run limits)

- **Component**: `ratd` `runner`
- **Effort**: L
- **Cross-refs**: Architecture F-12, Python GR-03
- **Files**: `platform/internal/executor/warmpool.go`, `runner/src/rat_runner/server.py`
- **Problem**: Unlimited run submissions. Runner queues internally with no feedback. Scheduler keeps submitting when runner is saturated.
- **Fix**:
  1. Track active runs in runner, return `RESOURCE_EXHAUSTED` when limit reached
  2. Add pending run limit per pipeline in ratd
  3. Scheduler skips pipelines with pending runs

### P2-05: Push pagination to SQL (eliminate in-memory pagination)

- **Component**: `ratd`
- **Effort**: L
- **Cross-refs**: Go 3.2, API PAG-02, TS P-4
- **Files**: `platform/internal/api/router.go` (`paginate[T]`), `pipelines.go`, `runs.go`
- **Problem**: Every paginated endpoint loads ALL records into memory then slices. OOM risk with large datasets.
- **Fix**:
  1. Add `limit`/`offset` parameters to all store interfaces
  2. Add corresponding `LIMIT`/`OFFSET` to sqlc queries
  3. Return total count via `COUNT(*)` query
  4. Remove in-memory `paginate[T]` helper

### P2-06: Fix N+1 query patterns

- **Component**: `ratd`
- **Effort**: M
- **Cross-refs**: Go 4.1, API PERF-01
- **Files**: `platform/internal/api/lineage.go`, `platform/internal/api/query.go` (HandleGetSchema)
- **Problem**: Lineage loads all pipelines then 2N+1 queries. Schema fires N+1 gRPC calls.
- **Fix**:
  1. Add batch queries for lineage (JOIN pipelines + runs)
  2. Add `GetBulkSchema` RPC to QueryService
  3. Cache lineage results with short TTL

### P2-07: Fix SSE log accumulation (O(n^2) memory)

- **Component**: `portal`
- **Effort**: M
- **Cross-refs**: TS P-1
- **File**: `portal/src/hooks/use-sse.ts`, lines 42-48
- **Problem**: Every log entry creates new array via spread. Thousands of lines = O(n^2) memory.
- **Fix**: Use `useRef` for buffer, batch updates every 100ms. Cap at max displayed lines (e.g., 10,000).

### P2-08: Add transactions for multi-step database operations

- **Component**: `ratd`
- **Effort**: M
- **Cross-refs**: Go 6.1
- **Files**: `platform/internal/api/publish.go`, `platform/internal/api/versions.go`
- **Problem**: Pipeline publish and version rollback perform multiple DB operations without transactions.
- **Fix**: Wrap multi-step operations in `pgx` transactions.

### P2-09: Track and manage fire-and-forget goroutines

- **Component**: `ratd`
- **Effort**: M
- **Cross-refs**: Go 2.1, 2.2, 11.1
- **Files**: `platform/internal/api/landing_zones.go` (line 332), `platform/internal/api/ratelimit.go`, `platform/cmd/ratd/main.go` (lines 173-178)
- **Problem**: `context.Background()` goroutines unlinked from server lifecycle. Rate limiter stop channel never closed. No mechanism to wait for in-flight callbacks.
- **Fix**:
  1. Pass server-scoped context to background goroutines
  2. Add `sync.WaitGroup` for lifecycle tracking
  3. Close rate limiter `stop` channel on shutdown
  4. Add panic recovery to all goroutines

### P2-10: Fix CreatePipeline error handling

- **Component**: `ratd`
- **Effort**: S
- **Cross-refs**: Go 1.3
- **File**: `platform/internal/api/pipelines.go`, line ~110-115
- **Problem**: All store errors treated as 409 Conflict. DB connection failure returns 409 instead of 500.
- **Fix**: Check for specific `ErrAlreadyExists` sentinel error. Default to 500 for unknown errors.

### P2-11: Fix render-body side effect in runs page

- **Component**: `portal`
- **Effort**: S
- **Cross-refs**: TS RP-1, TS SM-1
- **File**: `portal/src/app/runs/[id]/page.tsx`, lines 107-109
- **Problem**: `mutate()` called directly in render body. Causes infinite re-renders.
- **Fix**:
  ```typescript
  useEffect(() => {
    if (run?.status === "success" || run?.status === "failed") {
      mutate((key: string) => key.startsWith("pipelines"));
    }
  }, [run?.status, mutate]);
  ```

### P2-12: Add query timeout to QueryEngine

- **Component**: `ratq`
- **Effort**: M
- **Cross-refs**: Python RM-01
- **File**: `query/src/rat_query/engine.py`, lines 167-188
- **Problem**: No timeout on user queries. Malicious or slow query consumes all resources indefinitely.
- **Fix**:
  ```python
  self._conn.execute(f"SET statement_timeout = '{timeout_seconds}s'")
  ```

### P2-13: Fix Nessie branch accumulation

- **Component**: `ratd`
- **Effort**: M
- **Cross-refs**: Architecture F-11
- **File**: `platform/internal/reaper/reaper.go`
- **Problem**: Ephemeral branches accumulate between reaper runs (60 min default). Bursts of failures create hundreds of orphans.
- **Fix**: Make `create_branch` idempotent. Reduce orphan cleanup frequency to 15 min.

---

## Phase 3 — High Code Quality & DX

> **Priority**: P1/P2 — Within 1 month
> **Goal**: Fix architectural debt and developer experience issues
> **Estimated effort**: 10-15 days

### P3-01: Break up the 1035-line pipeline detail page

- **Component**: `portal`
- **Effort**: XL
- **Cross-refs**: TS RP-4
- **File**: `portal/src/app/pipelines/[ns]/[layer]/[name]/page.tsx`
- **Problem**: Handles metadata, code editing, preview, run history, quality tests, triggers, scheduling, and config in one component.
- **Fix**: Extract `PipelineEditor`, `PipelinePreview`, `PipelineRunHistory`, `PipelineConfig`, `PipelineTriggers` sub-components.

### P3-02: Break up the 938-line landing zone page

- **Component**: `portal`
- **Effort**: L
- **Cross-refs**: TS RP-5
- **File**: `portal/src/app/landing/[ns]/[name]/page.tsx`
- **Fix**: Extract file manager, metadata editor, trigger configuration into sub-components.

### P3-03: Break up execute_pipeline() monolith

- **Component**: `runner`
- **Effort**: L
- **Cross-refs**: Python CQ-02
- **File**: `runner/src/rat_runner/executor.py`, lines 105-404
- **Problem**: 270-line function with deeply nested try/except, duplicated success-path code.
- **Fix**: Extract each phase into named functions. Extract `_post_success()`.

### P3-04: Add route-level error boundaries

- **Component**: `portal`
- **Effort**: M
- **Cross-refs**: TS NX-2
- **Problem**: No `error.tsx` files. Page errors crash the entire app.
- **Fix**: Add `error.tsx` to:
  - `portal/src/app/error.tsx`
  - `portal/src/app/pipelines/error.tsx`
  - `portal/src/app/runs/error.tsx`
  - `portal/src/app/explorer/error.tsx`
  - `portal/src/app/landing/error.tsx`

### P3-05: Wire up validateName() in create pipeline dialog

- **Component**: `portal`
- **Effort**: S
- **Cross-refs**: TS FV-1
- **Files**: `portal/src/components/create-pipeline-dialog.tsx`, `portal/src/lib/validation.ts`
- **Problem**: Validation function exists but is not connected to the dialog.
- **Fix**: Import and call `validateName()` on submit, show inline errors.

### P3-06: Show error states for failed SWR fetches

- **Component**: `portal`
- **Effort**: M
- **Cross-refs**: TS DF-1
- **Problem**: Most SWR hooks destructure `{ data, isLoading }` but ignore `error`. API errors result in silent empty state.
- **Fix**: Destructure `error` from all SWR hooks. Display error UI component.

### P3-07: Standardize JSON error responses

- **Component**: `ratd`
- **Effort**: M
- **Cross-refs**: API ERR-01, ERR-02, Go 3.5
- **Files**: `platform/internal/api/router.go`, all handler files
- **Problem**: `internalError` and all validation errors return `text/plain`. SDK must handle two formats.
- **Fix**: Create `errorJSON(w, message, code, status)` helper. Replace all `http.Error()` calls with structured JSON:
  ```json
  {"error": {"code": "INVALID_ARGUMENT", "message": "..."}}
  ```

### P3-08: Complete API specification documentation

- **Component**: `docs`
- **Effort**: L
- **Cross-refs**: API DOC-01, DOC-02
- **File**: `docs/api-spec.md`
- **Problem**: Missing ~15 endpoint groups (Lineage, Sharing, Triggers, Preview, Publish, Versions, Webhooks, Audit, Schema, etc.). Claims 51 endpoints, actual is ~65-70.
- **Fix**: Document all endpoints with request/response schemas.

### P3-09: Set up GitHub Actions CI

- **Component**: `infra`
- **Effort**: L
- **Cross-refs**: Infra #51
- **Problem**: No `.github/workflows/` directory despite CLAUDE.md referencing CI enforcement.
- **Fix**: Create `.github/workflows/ci.yml` with jobs:
  - `test-go`: Go tests
  - `test-py`: Python tests (runner + query)
  - `test-ts`: TypeScript tests (SDK + portal)
  - `lint`: All linters
  - `proto-breaking`: `buf breaking`
  - `docker-build`: Build all images

### P3-10: Fix dev dependency leak in Python Dockerfiles

- **Component**: `infra`
- **Effort**: M
- **Cross-refs**: Infra #6, #13
- **Files**: `runner/Dockerfile`, `query/Dockerfile`
- **Problem**: `[dev]` deps (pytest, ruff, pyright, grpcio-tools) installed in builder and copied to production image.
- **Fix**: Two-stage install — production deps for runtime copy, dev deps only for build/test.

### P3-11: Fix /usr/local/bin full copy in Python Dockerfiles

- **Component**: `infra`
- **Effort**: S
- **Cross-refs**: Infra #7, #14
- **Problem**: Copies ALL binaries including `pip`, `uv`, `ruff`, `pytest` to runtime.
- **Fix**: Copy only needed binaries, or use `COPY --from=builder /usr/local/lib/python3.12/site-packages /usr/local/lib/python3.12/site-packages` only.

### P3-12: Fix Makefile Go image inconsistency

- **Component**: `infra`
- **Effort**: S
- **Cross-refs**: Infra #40
- **File**: `Makefile`
- **Problem**: `test-go` uses `golang:1.24` (Debian ~800MB) but `GO_IMAGE` variable is `golang:1.24-alpine`.
- **Fix**: Replace all hardcoded `golang:1.24` with `$(GO_IMAGE)`.

### P3-13: Standardize path parameter naming across all endpoints

- **Component**: `ratd`
- **Effort**: M
- **Cross-refs**: API API-01
- **Problem**: Inconsistent naming: `{namespace}` vs `{ns}`, `{name}` vs `{pipeline}`, extra path segments.
- **Fix**: Standardize on `{namespace}/{layer}/{name}` everywhere. Never abbreviate.

### P3-14: Validate cron expression syntax on schedule creation

- **Component**: `ratd`
- **Effort**: S
- **Cross-refs**: API VAL-01
- **File**: `platform/internal/api/schedules.go`, lines 85-139
- **Problem**: Checks cron is not empty but does not validate syntax. Triggers DO validate.
- **Fix**: Add `cronParser.Parse(cron)` validation like the trigger handler.

### P3-15: Fix SDK transport type safety

- **Component**: `sdk`
- **Effort**: M
- **Cross-refs**: TS TS-1, TS-2
- **Files**: `sdk-typescript/src/resources/*.ts`, `sdk-typescript/src/transport.ts`
- **Problem**: Transport returns `Promise<any>`, resources double-cast `as unknown as Record<string, unknown>`.
- **Fix**: Make transport accept `unknown` JSON bodies, return `Promise<unknown>`.

### P3-16: Add missing barrel exports for SDK resources

- **Component**: `sdk`
- **Effort**: S
- **Cross-refs**: TS SDK-1
- **File**: `sdk-typescript/src/resources/index.ts`
- **Problem**: Missing exports for `TriggersResource` and `RetentionResource`.
- **Fix**: Add missing exports.

### P3-17: Update stale SDK client test

- **Component**: `sdk`
- **Effort**: S
- **Cross-refs**: TS T-1
- **File**: `sdk-typescript/tests/client.test.ts`
- **Problem**: Asserts 7 resources but client has 12.
- **Fix**: Update to test all 12 resources and verify count.

### P3-18: Replace StreamLogs polling with condition variables

- **Component**: `runner`
- **Effort**: M
- **Cross-refs**: Python GR-01
- **File**: `runner/src/rat_runner/server.py`, lines 178-198
- **Problem**: Polls every 500ms with `time.sleep(0.5)`. Wastes CPU, adds latency.
- **Fix**: Add `threading.Condition` to `RunState`, wait on it instead of sleeping.

### P3-19: Fix read_watermark full table scan

- **Component**: `runner`
- **Effort**: M
- **Cross-refs**: Python DB-02, Architecture F-16
- **File**: `runner/src/rat_runner/iceberg.py`, lines 507-508
- **Problem**: `table.scan().to_arrow()` reads full table to compute `MAX(watermark_column)`.
- **Fix**: Use column projection: `table.scan(selected_fields=(watermark_column,)).to_arrow()` or `iceberg_scan`.

---

## Phase 4 — Medium Security

> **Priority**: P2 — Within 1-2 months
> **Goal**: Close remaining security gaps
> **Estimated effort**: 4-6 days

### P4-01: Sanitize sensitive data in logs

- **Component**: `runner`
- **Effort**: M
- **Cross-refs**: Security SEC-012, Python LG-01
- **Files**: `runner/src/rat_runner/executor.py` (line 238)
- **Problem**: Compiled SQL with S3 paths logged at INFO. Quality test violation rows with PII logged and persisted to Postgres.
- **Fix**: Log compiled SQL at DEBUG. Add log sanitization layer for PII.

### P4-02: Add SSE connection limits

- **Component**: `ratd`
- **Effort**: M
- **Cross-refs**: Security SEC-013
- **File**: `platform/internal/api/runs.go`, lines 288-372
- **Problem**: No max duration, per-user limit, or global cap. DoS vector.
- **Fix**: Add max SSE connection duration (30 min), per-IP limit (10), global cap (1000).

### P4-03: Fix S3 credentials in lru_cache

- **Component**: `runner`
- **Effort**: M
- **Cross-refs**: Security SEC-015, Python PF-01
- **File**: `runner/src/rat_runner/config.py`, lines 175-189
- **Problem**: STS session tokens cached indefinitely via `@functools.lru_cache`.
- **Fix**: Use `@functools.lru_cache` with TTL or replace with `cachetools.TTLCache`.

### P4-04: Validate YAML config schema

- **Component**: `runner`
- **Effort**: M
- **Cross-refs**: Security SEC-017, Python CF-02
- **Files**: `runner/src/rat_runner/config.py`
- **Problem**: `safe_load` used (good) but accepts any key silently. Unknown keys not flagged. `merge_strategy` not validated.
- **Fix**: Define allowed keys, warn on unknown keys, validate `merge_strategy` against known values.

### P4-05: Escape metadata_location in SQL

- **Component**: `runner`
- **Effort**: S
- **Cross-refs**: Python SEC-05
- **File**: `runner/src/rat_runner/iceberg.py`, lines 153-154
- **Problem**: `metadata_location` from PyIceberg catalog interpolated into SQL without escaping.
- **Fix**: `safe_location = metadata_location.replace("'", "''")`

### P4-06: Add license key signature validation

- **Component**: `ratd`
- **Effort**: M
- **Cross-refs**: Go 5.1
- **File**: `platform/internal/license/info.go`
- **Problem**: License key decoded without signature verification.

### P4-07: Add Portal Content-Security-Policy ✅

- **Component**: `portal`
- **Effort**: M
- **Cross-refs**: Security SEC-020
- **File**: `portal/src/middleware.ts`, `portal/src/app/layout.tsx`, `portal/next.config.mjs`
- **Fix**: Nonce-based CSP via Next.js middleware. Per-request nonce generated in `middleware.ts`, passed to `next-themes` ThemeProvider via `x-nonce` header. Static CSP removed from `next.config.mjs`.

---

## Phase 5 — Medium Performance & Reliability

> **Priority**: P2 — Within 1-2 months
> **Goal**: Performance optimizations and reliability improvements
> **Estimated effort**: 10-15 days

### P5-01: Separate scheduler/reaper from API server

- **Component**: `ratd`
- **Effort**: XL
- **Cross-refs**: Architecture F-01, F-10
- **File**: `platform/cmd/ratd/main.go`, lines 207-232
- **Problem**: ratd runs API, scheduler, trigger evaluator, reaper all in one process. Multiple replicas = duplicate runs.
- **Fix**:
  1. Short-term: Add `SCHEDULER_ENABLED=true/false` env var
  2. Use Postgres advisory locks (`pg_advisory_lock`) for leader election
  3. Long-term: Extract into separate `ratd-worker` process

### P5-02: Add event bus via Postgres LISTEN/NOTIFY

- **Component**: `ratd`
- **Effort**: L
- **Cross-refs**: Architecture F-19
- **Problem**: All communication synchronous. Trigger evaluator polls every 30s.
- **Fix**: Use Postgres `LISTEN/NOTIFY` for events (`run.completed`, `pipeline.created`). Zero new dependencies. Enables instant trigger evaluation.

### P5-03: Add distributed tracing (OpenTelemetry)

- **Component**: `ratd` `runner` `ratq`
- **Effort**: XL
- **Cross-refs**: Architecture F-22, API OBS-02
- **Problem**: No trace ID propagation. Debugging production issues requires manual correlation across 5+ services.
- **Fix**:
  1. Add OpenTelemetry SDK to all services
  2. Propagate `request_id` via gRPC metadata headers
  3. Include trace ID in all log messages
  4. Add Jaeger/Zipkin to compose for dev

### P5-04: Add caching layer

- **Component**: `ratd`
- **Effort**: L
- **Cross-refs**: Architecture F-20, F-21
- **Problem**: Every API request hits Postgres. ratq re-registers views every 30s regardless of changes. Lineage reads every pipeline SQL from S3.
- **Fix**:
  1. In-memory cache for namespaces, pipeline metadata (30s TTL)
  2. ETags for API responses
  3. Check Nessie commit hash before re-registering views

### P5-05: Improve health checks (deep dependency checks)

- **Component**: `ratd`
- **Effort**: M
- **Cross-refs**: Architecture F-23, Go 12.8
- **Problem**: `/health` returns `{"status": "healthy"}` unconditionally.
- **Fix**: Separate `/health/live` (process alive) and `/health/ready` (dependencies reachable). Check Postgres, S3, runner, ratq connectivity.

### P5-06: Fix Iceberg merge to avoid full table rewrites

- **Component**: `runner`
- **Effort**: XL
- **Cross-refs**: Architecture F-13
- **File**: `runner/src/rat_runner/iceberg.py`
- **Problem**: All strategies except `append_only` read full table, merge, `table.overwrite()`. 10M rows + 1K new = write 10M+1K.
- **Fix**: Use PyIceberg native row-level deletes + append for `incremental` and `delete_insert`. Partition tables for scoped overwrites.

### P5-07: Add Iceberg partitioning support

- **Component**: `runner`
- **Effort**: L
- **Cross-refs**: Architecture F-14
- **Problem**: Tables created without partition specs.
- **Fix**: Add `partition_by` to `config.yaml`, pass to `catalog.create_table()` as `PartitionSpec`.

### P5-08: Migrate ref() to iceberg_scan

- **Component**: `runner`
- **Effort**: L
- **Cross-refs**: Architecture F-15
- **Problem**: `ref()` resolves to `read_parquet('s3://.../**/*.parquet')` which reads orphaned files and ignores Iceberg metadata.
- **Fix**: Resolve ref to `iceberg_scan()` with metadata file path from catalog.

### P5-09: Reuse DuckDB connections in Iceberg merge operations

- **Component**: `runner`
- **Effort**: M
- **Cross-refs**: Python DB-01
- **File**: `runner/src/rat_runner/iceberg.py`
- **Problem**: Five functions each create new `duckdb.connect(":memory:")` with extension loading overhead.
- **Fix**: Accept optional `DuckDBEngine` parameter and reuse its connection.

### P5-10: Add S3 operations retry and timeout

- **Component**: `ratd`
- **Effort**: M
- **Cross-refs**: Architecture F-34
- **File**: `platform/internal/storage/s3store.go`
- **Fix**: Configure explicit timeouts (10s metadata, 60s data). Enable SDK-level retries with exponential backoff.

### P5-11: Replace polling with push-based run status

- **Component**: `ratd` `runner`
- **Effort**: L
- **Cross-refs**: Architecture F-03, F-17
- **Problem**: ratd polls runner every 5s per active run. 100 concurrent runs = 100 gRPC calls/5s. Status lives in both Postgres and runner memory (split-brain).
- **Fix**: Add `StreamRunStatus` push-based gRPC RPC. Make Postgres single source of truth.

### P5-12: Make runner worker count configurable, support multiple replicas

- **Component**: `runner`
- **Effort**: L
- **Cross-refs**: Architecture F-05, Python GR-02
- **File**: `runner/src/rat_runner/server.py`, line 374
- **Problem**: Hardcoded `ThreadPoolExecutor(max_workers=10)`. Single runner is a hard bottleneck.
- **Fix**:
  1. Make `MAX_WORKERS` configurable via env var
  2. Allow `RUNNER_ADDR` as comma-separated list in ratd
  3. Add round-robin dispatch

### P5-13: Add database migration locking

- **Component**: `ratd`
- **Effort**: M
- **Cross-refs**: Architecture F-25, Go 6.3
- **File**: `platform/internal/postgres/migrate.go`
- **Problem**: No rollback mechanism, no migration locking. Concurrent ratd starts could run migrations simultaneously.
- **Fix**: Add `pg_advisory_lock` around migration execution.

### P5-14: Add request/response logging middleware

- **Component**: `ratd`
- **Effort**: S
- **Cross-refs**: API OBS-01
- **File**: `platform/internal/api/router.go`
- **Problem**: No `middleware.Logger`. API calls invisible in logs.
- **Fix**: Add `chi/middleware.Logger` or custom structured logger.

---

## Phase 6 — Medium Code Quality

> **Priority**: P2/P3 — Within 2-3 months
> **Goal**: Reduce technical debt, eliminate duplication, improve maintainability
> **Estimated effort**: 8-12 days

### P6-01: Extract shared config into common Python package

- **Component**: `runner` `ratq`
- **Effort**: L
- **Cross-refs**: Python CQ-01, CQ-05, CF-01
- **Problem**: `S3Config`, `DuckDBConfig`, `NessieConfig` independently defined in both services with subtle differences. DuckDB S3 setup duplicated in 3 places.
- **Fix**: Create `rat_common` package with shared config classes and `configure_duckdb_s3()` utility.

### P6-02: Convert MergeStrategy to StrEnum

- **Component**: `runner`
- **Effort**: S
- **Cross-refs**: Python CQ-04
- **File**: `runner/src/rat_runner/models.py`, lines 69-87
- **Fix**: Convert from class constants to `StrEnum`. Update `PipelineConfig.merge_strategy` type.

### P6-03: Define PipelineLogger protocol

- **Component**: `runner`
- **Effort**: M
- **Cross-refs**: Python TS-01, TS-04
- **Files**: `runner/src/rat_runner/maintenance.py`, `python_exec.py`
- **Problem**: `log` parameter typed as `object | None`, requiring 6+ `# type: ignore` comments.
- **Fix**: Define `PipelineLogger` protocol with `info()`, `warn()`, `error()` methods.

### P6-04: Deduplicate RAT_LOGO constant

- **Component**: `portal`
- **Effort**: S
- **Cross-refs**: TS CO-1
- **Files**: `portal/src/app/page.tsx`, `portal/src/components/nav/sidebar.tsx`
- **Fix**: Move to shared constant in `lib/constants.ts`.

### P6-05: Deduplicate formatBytes function

- **Component**: `portal`
- **Effort**: S
- **Cross-refs**: TS CO-2
- **Files**: `portal/src/lib/utils.ts`, `portal/src/components/preview-panel.tsx`
- **Fix**: Use single definition from `lib/utils.ts`.

### P6-06: Import portal types from SDK instead of duplicating

- **Component**: `portal`
- **Effort**: L
- **Cross-refs**: TS CO-3
- **File**: `portal/src/lib/server-api.ts`
- **Problem**: Server-side API types duplicate SDK model types. Will drift.
- **Fix**: Import types from SDK package.

### P6-07: Extract shared proto messages

- **Component**: `proto`
- **Effort**: M
- **Cross-refs**: API PROTO-03, PROTO-04
- **Files**: `proto/runner/v1/runner.proto`, `proto/executor/v1/executor.proto`
- **Problem**: `LogEntry`, `GetRunStatus*`, `StreamLogs*`, `Cancel*` duplicated across protos.
- **Fix**: Move shared messages to `common/v1/common.proto`. Define `LogLevel` enum.

### P6-08: Migrate to google.protobuf.Timestamp

- **Component**: `proto`
- **Effort**: L
- **Cross-refs**: API PROTO-01
- **File**: `proto/common/v1/common.proto`, lines 6-9
- **Fix**: Replace custom `Timestamp` with `google.protobuf.Timestamp`. Update all services.

### P6-09: Create typed S3Credentials proto message

- **Component**: `proto`
- **Effort**: M
- **Cross-refs**: API PROTO-05
- **File**: `proto/runner/v1/runner.proto`, line 37
- **Problem**: S3 credentials passed as untyped `map<string,string>`.
- **Fix**: Create typed `S3Credentials` message with named fields + `debug_redact`.

### P6-10: Add SWR cache key factory

- **Component**: `portal`
- **Effort**: M
- **Cross-refs**: TS SM-2
- **File**: `portal/src/hooks/use-api.ts`
- **Fix**:
  ```typescript
  const KEYS = {
    runs: () => "runs" as const,
    run: (id: string) => `runs/${id}` as const,
    pipelines: (ns?: string) => ns ? `pipelines/${ns}` : "pipelines",
  } as const;
  ```

### P6-11: Add reserved fields strategy to all proto messages

- **Component**: `proto`
- **Effort**: M
- **Cross-refs**: API BC-01
- **Fix**: Document field reservation policy. Add `reserved` ranges to any messages that have had fields removed.

### P6-12: Reuse get_catalog() in maintenance.py

- **Component**: `runner`
- **Effort**: S
- **Cross-refs**: Python CQ-03
- **File**: `runner/src/rat_runner/maintenance.py`, lines 31-41, 86-96
- **Problem**: Duplicates catalog creation logic slightly differently from `iceberg.get_catalog()`.
- **Fix**: Import and use `iceberg.get_catalog()`.

### P6-13: Memoize DataTable columns

- **Component**: `portal`
- **Effort**: S
- **Cross-refs**: TS RP-2
- **File**: `portal/src/components/data-table.tsx`
- **Fix**: Memoize column definitions with `useMemo`.

### P6-14: Memoize CodeEditor extensions array

- **Component**: `portal`
- **Effort**: S
- **Cross-refs**: TS RP-3
- **Files**: `portal/src/components/code-editor.tsx`, `portal/src/components/sql-editor.tsx`
- **Fix**: Memoize `extensions` array with `useMemo`.

### P6-15: Fix broad except clauses in Iceberg operations

- **Component**: `runner`
- **Effort**: M
- **Cross-refs**: Python CQ-06
- **File**: `runner/src/rat_runner/iceberg.py`, lines 156, 272, 364, 463
- **Problem**: Silently falls back to loading entire table on any exception. Hides errors, can cause OOM.
- **Fix**: Catch only specific DuckDB/S3 exceptions. Log at warning level.

### P6-16: Fix RunState encapsulation

- **Component**: `runner`
- **Effort**: S
- **Cross-refs**: Python TS-03
- **File**: `runner/src/rat_runner/server.py`, line 180
- **Problem**: `StreamLogs` directly accesses `run._lock`.
- **Fix**: Add public `get_logs_from(cursor)` method to `RunState`.

### P6-17: Add thread safety to _runs dict

- **Component**: `runner`
- **Effort**: M
- **Cross-refs**: Python EH-03
- **File**: `runner/src/rat_runner/server.py`, lines 81-91
- **Problem**: `self._runs` accessed concurrently without lock.
- **Fix**: Add `threading.Lock` around `_runs` access.

### P6-18: Fix HandleDeleteSchedule 500 on not-found

- **Component**: `ratd`
- **Effort**: S
- **Cross-refs**: API REST-02
- **File**: `platform/internal/api/schedules.go`, lines 166-175
- **Fix**: Check existence before deletion. Return 404 for not-found.

### P6-19: Fix audit log response format

- **Component**: `ratd`
- **Effort**: S
- **Cross-refs**: API API-03
- **File**: `platform/internal/api/audit.go`, line 72
- **Problem**: Returns bare array instead of envelope.
- **Fix**: Wrap in `{"entries": [...], "total": N}`.

### P6-20: Nest quality tests under pipeline resource

- **Component**: `ratd`
- **Effort**: M
- **Cross-refs**: API API-05
- **File**: `platform/internal/api/quality.go`
- **Problem**: `/api/v1/tests/{ns}/{layer}/{pipeline}` breaks resource hierarchy.
- **Fix**: Move to `/api/v1/pipelines/{ns}/{layer}/{name}/tests`.

### P6-21: Fix silent error swallowing in SSE handlers

- **Component**: `portal`
- **Effort**: S
- **Cross-refs**: TS EH-1
- **File**: `portal/src/hooks/use-sse.ts`
- **Fix**: `console.warn("Failed to parse SSE log entry:", e)` instead of empty catch.

### P6-22: Add error logging to generic catch blocks

- **Component**: `portal`
- **Effort**: S
- **Cross-refs**: TS EH-2
- **Fix**: `console.error("Failed to save:", e)` before `triggerGlitch()`.

### P6-23: Add SSE reconnection with exponential backoff

- **Component**: `portal`
- **Effort**: M
- **Cross-refs**: TS EH-3
- **File**: `portal/src/hooks/use-sse.ts`
- **Fix**: Implement reconnection logic with exponential backoff on connection failure.

### P6-24: Fix _resolve_ref silent exception swallowing

- **Component**: `runner`
- **Effort**: S
- **Cross-refs**: Python TM-01
- **File**: `runner/src/rat_runner/templating.py`, lines 166-177
- **Fix**: `logger.warning("Failed to resolve ref '%s' via catalog, using fallback: %s", table_ref, e)`

---

## Phase 7 — Medium Testing

> **Priority**: P2 — Within 2-3 months
> **Goal**: Achieve meaningful test coverage across all components
> **Estimated effort**: 10-15 days

### P7-01: Add portal test coverage (hooks and utilities)

- **Component**: `portal`
- **Effort**: XL
- **Cross-refs**: TS T-3, T-5
- **Problem**: 40+ components, 4 hooks, 8 lib modules — only 2 test files exist.
- **Fix**: Priority test targets:
  1. `use-api.ts` hook — SWR cache management
  2. `use-sse.ts` hook — SSE connection, reconnection, parsing
  3. `use-preview.ts` hook — preview lifecycle
  4. `server-api.ts` — API client functions
  5. `pipeline-merge-strategy.tsx` — annotation parsing
  6. `create-pipeline-dialog.tsx` — form validation
  7. `data-table.tsx` — rendering, pagination

### P7-02: Add tests for 5 missing SDK resource classes

- **Component**: `sdk`
- **Effort**: L
- **Cross-refs**: TS T-2
- **File**: `sdk-typescript/tests/resources.test.ts`
- **Fix**: Add tests for `LandingResource`, `TriggersResource`, `QualityResource`, `LineageResource`, `RetentionResource`.

### P7-03: Add Postgres integration tests

- **Component**: `ratd`
- **Effort**: L
- **Cross-refs**: Go 7.2
- **Problem**: No `_test.go` files in `platform/internal/postgres/`.
- **Fix**: Add integration tests using testcontainers or a test database.

### P7-04: Add Python integration tests

- **Component**: `runner` `ratq`
- **Effort**: L
- **Cross-refs**: Python TT-03
- **Fix**: Add integration tests using `testcontainers-python` for DuckDB + S3 + Nessie.

### P7-05: Add edge case tests for annotations

- **Component**: `portal`
- **Effort**: M
- **Cross-refs**: TS T-4
- **Fix**: Add tests for malformed annotations, unicode, Windows line endings, mixed comment styles.

### P7-06: Add catalog urlopen timeout tests

- **Component**: `ratq`
- **Effort**: S
- **Cross-refs**: Python TT-01
- **Fix**: Test timeout behavior with mock server.

### P7-07: Add golangci-lint to CI

- **Component**: `ratd` `infra`
- **Effort**: M
- **Cross-refs**: Go 12.5
- **Fix**: Add `.golangci.yml` config, integrate into `make lint` and CI.

---

## Phase 8 — Medium Infrastructure

> **Priority**: P2/P3 — Within 2-3 months
> **Goal**: Production-ready infrastructure hardening
> **Estimated effort**: 6-10 days

### P8-01: Add container security hardening

- **Component**: `infra`
- **Effort**: M
- **Cross-refs**: Infra #24, #25
- **Fix**: Add to all application services in compose:
  ```yaml
  read_only: true
  cap_drop: [ALL]
  security_opt: [no-new-privileges:true]
  tmpfs: [/tmp]
  ```

### P8-02: Add resource limits to compose services

- **Component**: `infra`
- **Effort**: M
- **Cross-refs**: Infra #26
- **Fix**:
  ```yaml
  deploy:
    resources:
      limits:
        memory: 2G
        cpus: '2.0'
  pids_limit: 100
  ```

### P8-03: Add Docker network segmentation

- **Component**: `infra`
- **Effort**: M
- **Cross-refs**: Infra #28
- **Fix**: Define `frontend` (portal, ratd) and `backend` (ratd, ratq, runner, postgres, minio, nessie) networks.

### P8-04: Add log rotation

- **Component**: `infra`
- **Effort**: S
- **Cross-refs**: Infra #52
- **Fix**:
  ```yaml
  logging:
    driver: json-file
    options:
      max-size: "10m"
      max-file: "3"
  ```

### P8-05: Create .dockerignore files

- **Component**: `infra`
- **Effort**: S
- **Cross-refs**: Infra #21
- **Fix**: Create `.dockerignore` for each service (`platform/`, `runner/`, `query/`, repo root for portal).

### P8-06: Pin all Docker image versions

- **Component**: `infra`
- **Effort**: S
- **Cross-refs**: Infra #34, #35, Security SEC-018
- **Fix**: Pin `minio/minio`, `minio/mc`, `nessie`, `bufbuild/buf` to specific versions. Pin all base images to patch + digest.

### P8-07: Add missing compose health checks and start_period

- **Component**: `infra`
- **Effort**: S
- **Cross-refs**: Infra #30, #31, #32, #33
- **Fix**: Add portal health check, Nessie health check, `start_period: 10s` on Python services. Consider `grpc_health_probe` binary.

### P8-08: Fix dev-ratd hardcoded network name

- **Component**: `infra`
- **Effort**: S
- **Cross-refs**: Infra #42
- **Fix**: Set explicit `name:` in compose networks section.

### P8-09: Add stop_grace_period to compose

- **Component**: `infra`
- **Effort**: S
- **Cross-refs**: Infra #27
- **Fix**: Add `stop_grace_period: 30s` to runner and postgres.

### P8-10: Add smoke test error handling

- **Component**: `infra`
- **Effort**: S
- **Cross-refs**: Infra #48, #49, #50
- **File**: `infra/scripts/smoke-test.sh`
- **Fix**: Add `set -euo pipefail`. Replace `python3` with `jq` or run inside container. Use `mktemp -d`.

### P8-11: Optimize test-py performance

- **Component**: `infra`
- **Effort**: M
- **Cross-refs**: Infra #41
- **Problem**: Each `test-py` invocation installs deps from scratch.
- **Fix**: Create pre-built test images with dependencies cached.

### P8-12: Add `make setup` target

- **Component**: `infra`
- **Effort**: S
- **Cross-refs**: Infra #44
- **Problem**: CLAUDE.md documents `make setup` but target doesn't exist.
- **Fix**: Add `setup: proto sqlc sdk-build` target.

### P8-13: Move grpcio-tools to dev dependencies in query

- **Component**: `ratq`
- **Effort**: S
- **Cross-refs**: Python DP-01
- **File**: `query/pyproject.toml`, line 10
- **Fix**: Move `grpcio-tools` from `[project.dependencies]` to `[project.optional-dependencies.dev]`.

### P8-14: Remove unused Python dependencies

- **Component**: `runner` `ratq`
- **Effort**: S
- **Cross-refs**: Python DP-02, DP-03
- **Fix**: Verify and remove `s3fs` from runner if not needed transitively. Remove `boto3` from query if unused.

---

## Phase 9 — Medium Accessibility

> **Priority**: P3 — Within 3 months
> **Goal**: Make the portal accessible to all users
> **Estimated effort**: 3-5 days

### P9-01: Add accessible label to theme toggle

- **Component**: `portal`
- **Effort**: S
- **Cross-refs**: TS A11Y-1
- **File**: `portal/src/components/theme-toggle.tsx`
- **Fix**: Add `aria-label="Toggle dark mode"`.

### P9-02: Add ARIA tree role to file tree

- **Component**: `portal`
- **Effort**: M
- **Cross-refs**: TS A11Y-2
- **File**: `portal/src/components/file-tree.tsx`
- **Fix**: Add `role="tree"`, `role="treeitem"`, `aria-expanded` attributes.

### P9-03: Add caption/summary to DataTable

- **Component**: `portal`
- **Effort**: S
- **Cross-refs**: TS A11Y-3
- **File**: `portal/src/components/data-table.tsx`
- **Fix**: Add `<caption>` element.

### P9-04: Make drag-and-drop keyboard accessible

- **Component**: `portal`
- **Effort**: M
- **Cross-refs**: TS A11Y-4, Security SEC-009 (related: file input as alt)
- **File**: `portal/src/app/landing/[ns]/[name]/page.tsx`
- **Problem**: Drop zones use only mouse events.
- **Fix**: Add `<input type="file">` as keyboard-accessible alternative.

### P9-05: Add descriptive labels to delete buttons

- **Component**: `portal`
- **Effort**: S
- **Cross-refs**: TS A11Y-5
- **Fix**: Add `aria-label={`Delete file ${f.filename}`}` to all icon-only buttons.

### P9-06: Associate form labels with inputs

- **Component**: `portal`
- **Effort**: S
- **Cross-refs**: TS A11Y-6
- **Fix**: Add `htmlFor` to all `<label>` elements, matching input `id`s.

---

## Phase 10 — Low & Suggestion Items

> **Priority**: P3/P4 — Within 3-6 months
> **Goal**: Polish, optimization, and future-proofing
> **Estimated effort**: 15-20 days (can be parallelized extensively)

### Architecture & Design

| ID | Component | Item | Cross-refs |
|----|-----------|------|------------|
| P10-01 | `ratq` | Configure pgxpool connection limits (`MaxConns`, `MinConns`, `MaxConnLifetime`) | Architecture F-07, Go 4.4 |
| P10-02 | `portal` | Use `next.revalidate` for time-based caching instead of `cache: "no-store"` | Architecture F-08, TS DF-3 |
| P10-03 | `ratd` | Add service discovery documentation for Kubernetes | Architecture F-04 |
| P10-04 | `ratq` | Optimize view re-registration (compare Nessie state before CREATE OR REPLACE) | Architecture F-21 |
| P10-05 | `ratd` | Add plugin periodic health checks (every 30s) with disable/re-enable | Architecture F-29 |
| P10-06 | `ratd` | Add plugin version negotiation at startup | Architecture F-30 |
| P10-07 | `ratd` | Add feature flags via `platform_settings` table | Architecture F-27 |
| P10-08 | `runner` | Build dependency DAG validation from `ref()` calls, add cycle detection | Architecture F-31 |
| P10-09 | `runner` | Add configurable retry for failed pipelines (`max_retries`, `retry_delay`) | Architecture F-32 |
| P10-10 | `runner` | Document idempotency guarantees per merge strategy | Architecture F-33 |
| P10-11 | `ratd` | Add per-namespace resource quotas for Pro (max runs, storage, query duration) | Architecture F-28 |
| P10-12 | `ratd` | Add Redis-backed distributed rate limiter for Pro | Architecture F-35 |

### Go Platform

| ID | Component | Item | Cross-refs |
|----|-----------|------|------------|
| P10-13 | `ratd` | Fix auto-publish silent error ignoring | Go 1.4 |
| P10-14 | `ratd` | Fix loadRetentionConfig error swallowing | Go 1.5 |
| P10-15 | `ratd` | Redact internal error details from preview.go | Go 1.6 |
| P10-16 | `ratd` | Add max SSE connection duration | Go 2.3 |
| P10-17 | `ratd` | Fix schema endpoint sharing request context across goroutines | Go 2.4 |
| P10-18 | `ratd` | Fix OnRunComplete using parent context | Go 2.5 |
| P10-19 | `ratd` | Add request body size limit for file uploads | Go 3.4 |
| P10-20 | `ratd` | Fix CORS credentials + wildcard conflict | Go 3.6 |
| P10-21 | `ratd` | Deduplicate Arrow IPC deserialization code | Go 4.2 |
| P10-22 | `ratd` | Remove unnecessary StatObject in S3 DeleteFile | Go 4.3 |
| P10-23 | `ratd` | Fix webhook handler using `context.Background()` | Go 5.2 |
| P10-24 | `ratd` | Add rate limiting on webhook endpoint | Go 5.3 |
| P10-25 | `ratd` | Fix DurationMs int64 to int32 narrowing | Go 6.2 |
| P10-26 | `ratd` | Fix fmt.Sprintf for SQL argument positions | Go 6.4 |
| P10-27 | `ratd` | Add constraints to in-memory test stores | Go 7.3 |
| P10-28 | `ratd` | Add table-driven tests for validation logic | Go 7.4 |
| P10-29 | `ratd` | Fix query/client.go creating its own HTTP client | Go 8.1 |
| P10-30 | `ratd` | Implement plugin executor placeholder methods | Go 8.2 |
| P10-31 | `ratd` | Fix audit middleware logging after response | Go 9.1 |
| P10-32 | `ratd` | Standardize log levels for similar events | Go 9.2 |
| P10-33 | `ratd` | Refactor main.go into builder/options pattern | Go 10.1 |
| P10-34 | `ratd` | Validate environment variable values | Go 10.2 |
| P10-35 | `ratd` | Fix reaper ticker interval never updating | Go 11.2 |
| P10-36 | `ratd` | Fix type assertion to concrete type in sharing.go | Go 12.1 |
| P10-37 | `ratd` | Separate JSON tags from DB models (domain types) | Go 12.2 |
| P10-38 | `ratd` | Split large router.go file | Go 12.3 |
| P10-39 | `ratd` | Add errgroup for coordinated goroutine lifecycle | Go 12.6 |
| P10-40 | `ratd` | Add context-aware slog handler | Go 12.9 |

### Python Services

| ID | Component | Item | Cross-refs |
|----|-----------|------|------------|
| P10-41 | `runner` | Fix _boto3_client return type annotation | Python TS-02 |
| P10-42 | `runner` | Log branch deletion exceptions in executor cleanup | Python EH-02 |
| P10-43 | `ratq` | Log count_rows failures in ListTables | Python EH-04 |
| P10-44 | `runner` | Extract `_to_arrow_table()` utility for RecordBatchReader handling | Python IC-01 |
| P10-45 | `runner` | Deduplicate new_data on unique_key in scd2_iceberg | Python IC-02 |
| P10-46 | `runner` | Use explicit 3-part splitting for namespace.layer.table | Python IC-03 |
| P10-47 | `runner` | Fix explain_analyze f-string SQL formatting | Python DB-03 |
| P10-48 | `runner` | Fix preview_pipeline executing full query 3 times | Python DB-04 |
| P10-49 | `runner` | Fix validate_landing_zones sequential S3 LIST | Python PF-02 |
| P10-50 | `ratq` | Fix ListTables calling count_rows for every table | Python PF-03 |
| P10-51 | `runner` | Fix DuckDBConfig.from_env not validating threads value | Python CF-03 |
| P10-52 | `runner` | Fix validate_template edge cases (SQL comments, `{% %}` blocks) | Python TM-02 |
| P10-53 | `runner` | Remove unused preview.py parameters (sample_files, env) | Python TT-04 |
| P10-54 | `runner` | Standardize logger naming on `__name__` | Python LG-02 |
| P10-55 | `runner` | Add DuckDBEngine thread safety comment or lock | Python RM-02 |
| P10-56 | `ratq` | Join catalog refresh thread on shutdown | Python RM-03 |
| P10-57 | `ratq` | Add query engine thread safety tests | Python TT-02 |

### TypeScript / Portal / SDK

| ID | Component | Item | Cross-refs |
|----|-----------|------|------------|
| P10-58 | `portal` | Add runtime type validation on API responses (Zod) | TS TS-3 |
| P10-59 | `portal` | Add SSE event typing | TS TS-4 |
| P10-60 | `portal` | Remove `any` casts from portal components | TS TS-5 |
| P10-61 | `sdk` | Fix Error class for proper instanceof checks | TS TS-6 |
| P10-62 | `portal` | Use `useReducer` for landing zone form state | TS RP-6 |
| P10-63 | `portal` | Add `useCallback` for recreated event handlers | TS RP-7 |
| P10-64 | `portal` | Fix prop drilling in landing zone page | TS RP-8 |
| P10-65 | `portal` | Consider server component for home page | TS NX-1 |
| P10-66 | `portal` | Add loading.tsx for route transitions | TS NX-3 |
| P10-67 | `portal` | Add generateMetadata for SEO | TS NX-4 |
| P10-68 | `portal` | Fix SSE hook non-serializable state for SSR | TS NX-5 |
| P10-69 | `portal` | Fix retention page form state re-derivation | TS SM-3 |
| P10-70 | `portal` | Reset file upload state on navigation | TS SM-4 |
| P10-71 | `portal` | Use semantic theme variables instead of hardcoded colors | TS ST-1 |
| P10-72 | `portal` | Standardize micro font-size classes | TS ST-2 |
| P10-73 | `portal` | Memoize useScreenGlitch return value | TS P-2 |
| P10-74 | `portal` | Add error boundary for dynamic CodeEditor import | TS P-3 |
| P10-75 | `portal` | Add AbortController timeout to server-side fetches | TS EH-4 |
| P10-76 | `portal` | Add SWR refreshInterval for active runs | TS DF-2 |
| P10-77 | `portal` | Validate number inputs (no negative, no NaN) | TS FV-2 |
| P10-78 | `portal` | Add dirty tracking for landing zone metadata form | TS FV-3 |
| P10-79 | `portal` | Sanitize download filename from S3 | TS SEC-2 (portal) |
| P10-80 | `portal` | Add preview execution warning indicator | TS SEC-1 (portal) |
| P10-81 | `sdk` | Add AbortController cancellation support | TS SDK-2, SDK-2 (API) |
| P10-82 | `sdk` | Add multipart support for uploadFile | TS SDK-3 |
| P10-83 | `sdk` | Use exponential backoff for retry delay | TS SDK-4 |
| P10-84 | `sdk` | Add request/response interceptors | TS SDK-5 |
| P10-85 | `sdk` | Deduplicate RatClientOptions/ClientConfig | TS SDK-6 |
| P10-86 | `sdk` | Add Layer type for CreatePipelineRequest.layer | API SDK-02 |
| P10-87 | `sdk` | Add missing SDK models (Audit, Sharing, Webhooks) | API SDK-03 |

### API & Proto

| ID | Component | Item | Cross-refs |
|----|-----------|------|------------|
| P10-88 | `proto` | Add `option go_package` to proto files | API PROTO-07 |
| P10-89 | `proto` | Add field-level documentation comments | API PROTO-08 |
| P10-90 | `proto` | Use `oneof` for PreviewPipelineResponse | API PROTO-06 |
| P10-91 | `ratd` | Add pagination to HandleListNamespaces | API API-02 |
| P10-92 | `ratd` | Standardize response field types (run_id, duration_ms) | API API-04 |
| P10-93 | `ratd` | Fix metadata endpoint URL structure | API API-06 |
| P10-94 | `docs` | Document API deprecation strategy | API BC-02 |
| P10-95 | `ratd` | Redact error details from HandleCreatePipeline conflict | API ERR-03 |
| P10-96 | `ratd` | Add structured error type codes | API ERR-04 |
| P10-97 | `ratd` | Add SSE error event on abnormal close | API ERR-05 |
| P10-98 | `ratd` | Add cursor-based pagination option | API PAG-01 |
| P10-99 | `ratd` | Add pagination to HandleListLandingFiles | API PAG-03 |
| P10-100 | `ratd` | Add sorting support to list endpoints | API FILT-01 |
| P10-101 | `ratd` | Add date range filtering on runs | API FILT-02 |
| P10-102 | `ratd` | Implement documented `search` query param for pipelines | API FILT-03 |
| P10-103 | `ratd` | Add max length validation on description/SQL fields | API VAL-04 |
| P10-104 | `ratd` | Hardcode → configurable limit on HandlePreviewTable | API VAL-03 |
| P10-105 | `proto` | Add TypeScript proto generation to `buf.gen.yaml` | API VER-01 |
| P10-106 | `ratd` | Add rate limit headers to 429 responses | API RL-01 |
| P10-107 | `ratd` | Add per-endpoint rate limit differentiation | API RL-02 |
| P10-108 | `ratd` | Fix rate limiter goroutine leak on shutdown | API RL-03 |
| P10-109 | `ratd` | Add `/metrics` endpoint for Prometheus | API OBS-03 |
| P10-110 | `ratd` | Include version/build info in health response | API OBS-04 |
| P10-111 | `ratd` | Suppress S3 cleanup errors in HandleDeletePipeline | API REST-03 |
| P10-112 | `ratd` | Fix webhook handler `context.Background()` timeout | API REST-05 |

### Infrastructure

| ID | Component | Item | Cross-refs |
|----|-----------|------|------------|
| P10-113 | `infra` | Use `scratch` base image for ratd | Infra #1 |
| P10-114 | `infra` | Add OCI labels to all Dockerfiles | Infra #3, #12 |
| P10-115 | `infra` | Add EXPOSE instructions to Python Dockerfiles | Infra #8, #15 |
| P10-116 | `infra` | Add HEALTHCHECK instructions to all Dockerfiles | Infra #2, #9, #18 |
| P10-117 | `infra` | Fix portal lockfile glob in COPY | Infra #17 |
| P10-118 | `infra` | Add `--mount=type=cache` for npm in portal build | Infra #20 |
| P10-119 | `infra` | Expose Nessie port for dev or remove URL from Makefile | Infra #29 |
| P10-120 | `infra` | Add `init: true` to compose application services | Infra #37 |
| P10-121 | `infra` | Use compose profiles for test services | Infra #39 |
| P10-122 | `infra` | Add `restart: on-failure` to minio-init | Infra #36 |
| P10-123 | `infra` | Improve `clean` target visibility | Infra #43 |
| P10-124 | `infra` | Fix dev-portal network connectivity for SSR | Infra #45 |
| P10-125 | `infra` | Add `make restart` target | Infra #46 |
| P10-126 | `infra` | Add parallel test support | Infra #47 |
| P10-127 | `infra` | Pin test compose MinIO version | Infra #38 |
| P10-128 | `infra` | Document zero-downtime deployment for Pro | Infra #54, Architecture F-26 |

### Security (Low)

| ID | Component | Item | Cross-refs |
|----|-----------|------|------------|
| P10-129 | `ratd` | Consider TLS 1.3 minimum | Security SEC-019 |
| P10-130 | `runner` `ratq` | Sanitize DuckDB error messages before returning to clients | Security SEC-021 |
| P10-131 | `ratd` | Add request ID propagation to gRPC calls | Security SEC-022 |
| P10-132 | `ratd` | Add startup warning for default credentials | Security SEC-003 (supplement) |

---

## Summary

| Phase | Priority | Findings | Focus |
|-------|----------|----------|-------|
| **Phase 0** | P0 | 11 | Critical security & crash fixes |
| **Phase 1** | P1 | 8 | High security hardening |
| **Phase 2** | P1 | 13 | High reliability & performance |
| **Phase 3** | P1/P2 | 19 | High code quality & DX |
| **Phase 4** | P2 | 7 | Medium security |
| **Phase 5** | P2 | 14 | Medium performance & reliability |
| **Phase 6** | P2/P3 | 24 | Medium code quality |
| **Phase 7** | P2 | 7 | Medium testing |
| **Phase 8** | P2/P3 | 14 | Medium infrastructure |
| **Phase 9** | P3 | 6 | Medium accessibility |
| **Phase 10** | P3/P4 | 132 | Low & suggestion items |
| | | **255** | **Total unique items** |

> **Note**: The original 307 findings across 7 reviews reduced to 255 unique items after de-duplication. ~52 findings appeared in multiple reviews (e.g., exec() sandbox, SQL injection, hardcoded credentials, pagination, WriteTimeout).

---

## Recommended Execution Strategy

1. **Phase 0**: Assign to senior engineers immediately. All items can be parallelized. Each has a focused, well-defined scope.
2. **Phase 1-2**: Run in parallel. Security fixes (Phase 1) are independent from reliability fixes (Phase 2).
3. **Phase 3**: Can be distributed across the team. Component breakdown (portal vs ratd vs runner) enables natural parallelism.
4. **Phase 4-6**: Best tackled per-component. One engineer per service area.
5. **Phase 7**: Testing can run alongside any phase — start early, run continuously.
6. **Phase 8-9**: Infrastructure and accessibility are independent tracks.
7. **Phase 10**: Chip away during slack time or as part of related feature work.

---

*Plan generated 2026-02-16 from 7 comprehensive code reviews.*
*Source reviews: `docs/reviews/{go-platform-review,python-services-review,typescript-portal-review,security-audit,infrastructure-review,api-proto-review,architecture-review}.md`*
