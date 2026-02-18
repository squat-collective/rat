# Go Platform (`ratd`) — Comprehensive Code Review

**Reviewer**: Senior Go Engineer
**Date**: 2026-02-16
**Scope**: All Go source files in `platform/` (~19,750 lines, 38 test files)
**Branch**: `feat/ratd-health-and-pipelines`

---

## Executive Summary

The `ratd` platform is well-architected for a Go service of this complexity. The interface-driven design, consumer-site interface definitions, optional dependency pattern (nil-check guards), and structured logging are all strong choices. The codebase is clean, consistent, and demonstrates good Go idioms overall.

That said, the review identified **47 findings** across 12 categories, including 3 critical issues that could cause panics or data loss in production, and 8 high-severity issues that should be addressed before a stable release.

| Severity | Count |
|----------|-------|
| 🔴 Critical | 3 |
| 🟠 High | 8 |
| 🟡 Medium | 15 |
| 🔵 Low | 10 |
| 💡 Suggestion | 11 |

---

## Category 1: Error Handling

### 1.1 — 🔴 CRITICAL: Nil-pointer panic in HandleDeletePipeline

**File**: `platform/internal/api/pipelines.go`, line ~258

`HandleDeletePipeline` calls `s.Storage.ListFiles()` to clean up S3 artifacts but does not check whether `s.Storage` is nil first. Since Storage is an optional dependency (not set when `S3_ENDPOINT` is absent), this will panic in environments running without S3.

```go
// FIX: guard with nil check
if p.S3Path != "" && s.Storage != nil {
    files, err := s.Storage.ListFiles(ctx, p.S3Path)
}
```

### 1.2 — 🔴 CRITICAL: Unchecked json.Encode errors across all handlers

**File**: All handler files (~80+ instances)

Nearly every HTTP handler writes the response with `json.NewEncoder(w).Encode(...)` without checking the returned error. If encoding fails, the error is silently lost and partial JSON may have been written to the client.

**Fix**: Extract a `writeJSON` helper:
```go
func writeJSON(w http.ResponseWriter, status int, v any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    if err := json.NewEncoder(w).Encode(v); err != nil {
        slog.Error("failed to encode response", "error", err)
    }
}
```

### 1.3 — 🟠 HIGH: CreatePipeline masks database errors as "already exists"

**File**: `platform/internal/api/pipelines.go`, line ~110-115

The handler treats *all* store errors from `CreatePipeline` as 409 Conflict. A database connection failure, timeout, or constraint violation on a different column all return 409 instead of 500.

### 1.4 — 🟡 MEDIUM: HandleCreatePipeline silently ignores auto-publish errors

**File**: `platform/internal/api/pipelines.go`, line ~130-140

### 1.5 — 🟡 MEDIUM: loadRetentionConfig swallows all errors

**File**: `platform/internal/api/retention.go`

### 1.6 — 🟡 MEDIUM: preview.go leaks internal error details to client

**File**: `platform/internal/api/preview.go`, line ~76

---

## Category 2: Concurrency & Goroutines

### 2.1 — 🟠 HIGH: Fire-and-forget goroutine in landing zone upload

**File**: `platform/internal/api/landing_zones.go`, line ~332

```go
go s.evaluateLandingZoneTriggers(context.Background(), ns, name, header.Filename)
```

Uses `context.Background()` (unlinked from server lifecycle), no lifecycle tracking, no panic recovery.

### 2.2 — 🟠 HIGH: Rate limiter cleanup goroutine leaks

**File**: `platform/internal/api/ratelimit.go`

The `stop` channel is created but never closed during server shutdown.

### 2.3 — 🟡 MEDIUM: SSE log streaming has no maximum connection duration

**File**: `platform/internal/api/runs.go`

### 2.4 — 🟡 MEDIUM: Schema endpoint shares request context across goroutines

**File**: `platform/internal/api/query.go`, HandleGetSchema

### 2.5 — 🟡 MEDIUM: OnRunComplete callback uses parent context

**File**: `platform/internal/executor/warmpool.go`, line ~180

---

## Category 3: API Design & HTTP Handling

### 3.1 — 🔴 CRITICAL: Missing WriteTimeout on http.Server

**File**: `platform/cmd/ratd/main.go`, lines 253-258

```go
httpServer := &http.Server{
    Addr:              addr,
    Handler:           router,
    ReadHeaderTimeout: 10 * time.Second,
    IdleTimeout:       120 * time.Second,
    // WriteTimeout is MISSING — DoS vector
}
```

### 3.2 — 🟠 HIGH: In-memory pagination loads all rows

**File**: `platform/internal/api/router.go`, `paginate[T]` function

Every paginated endpoint first loads ALL records from the database into memory, then slices in Go. Push `LIMIT`/`OFFSET` down to SQL queries.

### 3.3 — 🟡 MEDIUM: Webhook token exposed in URL path

**File**: `platform/internal/api/webhooks.go`

### 3.4 — 🟡 MEDIUM: No request body size limit for file uploads

**File**: `platform/internal/api/landing_zones.go`

### 3.5 — 🔵 LOW: Inconsistent error response format

### 3.6 — 🔵 LOW: CORS middleware may conflict with credentials

---

## Category 4: Performance

### 4.1 — 🟠 HIGH: N+1 query pattern in lineage endpoint

**File**: `platform/internal/api/lineage.go`

Loads all pipelines, then spawns a goroutine per pipeline to fetch runs and quality tests (2N+1 queries).

### 4.2 — 🟡 MEDIUM: Duplicated Arrow IPC deserialization code

**Files**: `platform/internal/executor/warmpool.go` and `platform/internal/query/client.go`

### 4.3 — 🟡 MEDIUM: S3 DeleteFile does unnecessary StatObject

**File**: `platform/internal/storage/s3store.go`

### 4.4 — 🔵 LOW: No connection pool configuration for pgxpool

**File**: `platform/internal/postgres/conn.go`

---

## Category 5: Security

### 5.1 — 🟡 MEDIUM: License key decoded without signature validation

**File**: `platform/internal/license/info.go`

### 5.2 — 🟡 MEDIUM: Webhook handler uses context.Background()

**File**: `platform/internal/api/webhooks.go`, line ~79

### 5.3 — 🔵 LOW: No rate limiting on webhook endpoint

---

## Category 6: Database & SQL

### 6.1 — 🟠 HIGH: Multi-step operations without transactions

**Files**: `platform/internal/api/publish.go`, `platform/internal/api/versions.go`

Pipeline publish and version rollback perform multiple DB operations without a transaction.

### 6.2 — 🟡 MEDIUM: DurationMs int64 to int32 narrowing

**File**: `platform/internal/postgres/run_store.go`, line ~117

### 6.3 — 🟡 MEDIUM: Migrations lack per-migration transactions

**File**: `platform/internal/postgres/migrate.go`

### 6.4 — 🔵 LOW: fmt.Sprintf for SQL argument positions

---

## Category 7: Testing

### 7.1 — 🟡 MEDIUM: Test helpers missing some store interfaces

**File**: `platform/internal/api/testhelpers_test.go`

### 7.2 — 🟡 MEDIUM: No integration tests for database layer

**File**: `platform/internal/postgres/` (no `_test.go` files)

### 7.3 — 🔵 LOW: In-memory test stores don't enforce constraints

### 7.4 — 💡 SUGGESTION: Add table-driven tests for validation logic

---

## Category 8: gRPC / ConnectRPC

### 8.1 — 🟡 MEDIUM: query/client.go creates its own HTTP client

**File**: `platform/internal/query/client.go`

### 8.2 — 🔵 LOW: Plugin executor has unimplemented methods

**File**: `platform/internal/executor/plugin.go`

---

## Category 9: Logging

### 9.1 — 🟡 MEDIUM: Audit middleware logs after response is sent

**File**: `platform/internal/api/audit.go`

### 9.2 — 🔵 LOW: Inconsistent log levels for similar events

---

## Category 10: Configuration & Main

### 10.1 — 🟡 MEDIUM: main.go is 322 lines of sequential wiring

**File**: `platform/cmd/ratd/main.go`

### 10.2 — 🔵 LOW: No validation of environment variable values

---

## Category 11: Resource Management

### 11.1 — 🟠 HIGH: Detached goroutines in executor callbacks

**File**: `platform/cmd/ratd/main.go`, lines 173-178, 187-192

No mechanism to wait for in-flight `OnRunComplete` callbacks during shutdown.

### 11.2 — 🟡 MEDIUM: Reaper ticker interval never updates

**File**: `platform/internal/reaper/reaper.go`

---

## Category 12: Code Organization & Dependencies

### 12.1 — 🟡 MEDIUM: Type assertion to concrete type breaks abstraction

**File**: `platform/internal/api/sharing.go`

### 12.2 — 🟡 MEDIUM: Domain types have JSON tags but also serve as DB models

### 12.3 — 🔵 LOW: Large router.go file

### 12.4–12.11 — 💡 SUGGESTIONS: golangci-lint, errgroup, structured errors, health checks, request IDs, context-aware slog, OpenTelemetry

---

## Positive Observations

1. **Interface design**: Store interfaces defined at the consumer site — Go best practice
2. **Optional dependency pattern**: Every handler nil-checks optional dependencies
3. **Clean separation of concerns**: No circular dependencies
4. **Structured logging**: Consistent `slog` usage throughout
5. **Graceful shutdown**: Well-ordered shutdown sequence
6. **Reaper isolation**: `safeRun` with panic recovery per task
7. **Plugin system**: Health checks, graceful degradation, feature flags
8. **Test coverage**: 38 test files with in-memory stores
9. **Code volume**: ~19,750 lines for a complete data platform — impressively lean
10. **Consistent style**: Reads as if written by one person

---

## Prioritized Action Items

### Immediate (before production)
1. Fix nil-pointer panic in `HandleDeletePipeline` (1.1) — one-line fix
2. Add `WriteTimeout` to `http.Server` (3.1) — one-line fix
3. Extract `writeJSON` helper for unchecked `json.Encode` (1.2)

### Before stable release
4. Add transactions for multi-step DB operations (6.1)
5. Track fire-and-forget goroutines (2.1, 11.1)
6. Stop rate limiter cleanup goroutine on shutdown (2.2)
7. Push pagination to SQL (3.2)
8. Fix CreatePipeline error handling (1.3)

### Near-term
9. Add Postgres integration tests (7.2)
10. Add `golangci-lint` to CI (12.5)
11. Standardize error response format (3.5)
12. Add structured error types (12.7)

### Long-term
13. Add OpenTelemetry instrumentation (12.11)
14. Health endpoint dependency checks (12.8)
15. Refactor main.go into builder pattern (10.1)

---

*Review generated on 2026-02-16. All file paths are relative to `platform/`.*
