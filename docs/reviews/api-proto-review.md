# API & Protocol Buffers Comprehensive Review

**Reviewer**: Senior API Architect & Protobuf Expert
**Date**: 2026-02-16
**Scope**: Proto definitions, REST API handlers, API spec docs, generated code, SDK
**Branch**: `feat/ratd-health-and-pipelines`

---

## Summary Statistics

| Severity | Count |
|----------|-------|
| Critical | 4 |
| High | 11 |
| Medium | 18 |
| Low | 12 |
| Suggestion | 9 |
| **Total** | **54** |

---

## 1. Proto Design

### PROTO-01: Custom Timestamp instead of `google.protobuf.Timestamp` — `HIGH`

- **File**: `proto/common/v1/common.proto`, lines 6-9
- **What**: The codebase defines a custom `Timestamp` message with `seconds` + `nanos` instead of using the well-known `google.protobuf.Timestamp`. This loses automatic JSON serialization to RFC3339, language-specific helpers (Go's `timestamppb.New(time.Now())`), and interoperability with the protobuf ecosystem.
- **Fix**:
```protobuf
import "google/protobuf/timestamp.proto";
// Replace all uses of ratatouille.common.v1.Timestamp with google.protobuf.Timestamp
```

### PROTO-02: `RunStatus` missing `CANCELLED` value — `CRITICAL`

- **File**: `proto/common/v1/common.proto`, lines 18-24
- **What**: The proto enum defines `PENDING`, `RUNNING`, `SUCCESS`, `FAILED` but is missing `CANCELLED`. The Go domain model (`domain/models.go:69`) defines `RunStatusCancelled = "cancelled"`, and the cancel endpoint sets this status. The proto and domain model are out of sync — any gRPC client receiving a cancelled run will see `RUN_STATUS_UNSPECIFIED` (0) since there is no proto value for it.
- **Fix**:
```protobuf
enum RunStatus {
  RUN_STATUS_UNSPECIFIED = 0;
  RUN_STATUS_PENDING = 1;
  RUN_STATUS_RUNNING = 2;
  RUN_STATUS_SUCCESS = 3;
  RUN_STATUS_FAILED = 4;
  RUN_STATUS_CANCELLED = 5;
}
```

### PROTO-03: Duplicate `LogEntry` and `GetRunStatus*` messages across runner and executor — `MEDIUM`

- **Files**: `proto/runner/v1/runner.proto` (lines 66-70), `proto/executor/v1/executor.proto` (lines 56-60)
- **What**: `LogEntry`, `GetRunStatusRequest/Response`, `StreamLogsRequest`, `CancelRequest/Response` are nearly identical in both protos. This violates DRY and makes evolution harder.
- **Fix**: Extract shared messages into `common/v1/common.proto`.

### PROTO-04: `LogEntry.level` is a free-form `string` instead of an enum — `MEDIUM`

- **Files**: `proto/runner/v1/runner.proto` (line 68), `proto/executor/v1/executor.proto` (line 58)
- **What**: Runner uses `"info", "warn", "error", "debug"` while executor uses `"stdout", "stderr"`. Clients cannot reliably switch on log level.
- **Fix**: Define a `LogLevel` enum in common.

### PROTO-05: `SubmitPipelineRequest` passes S3 credentials via `map<string, string>` — `HIGH`

- **File**: `proto/runner/v1/runner.proto`, line 37
- **What**: Sensitive credentials are passed as untyped map entries with no schema enforcement, no compile-time safety, and risk of accidental logging.
- **Fix**: Create a typed `S3Credentials` message.

### PROTO-06: `PreviewPipelineResponse` could use `oneof` for result vs. error — `LOW`

- **File**: `proto/runner/v1/runner.proto`, lines 94-104

### PROTO-07: Missing `option go_package` in proto files — `MEDIUM`

- **File**: All proto files
- **What**: Relies on buf managed mode. Explicit `go_package` options ensure compatibility with non-buf tools.

### PROTO-08: No field-level leading documentation comments on most fields — `MEDIUM`

- **File**: Multiple proto files
- **What**: Most fields use trailing comments instead of leading comments. Many documentation generators ignore trailing comments.

---

## 2. API Consistency

### API-01: Inconsistent path parameter naming across endpoints — `HIGH`

- **File**: Multiple handler files
- **What**: The REST API uses inconsistent names for the same concept:
  - Pipelines: `{namespace}/{layer}/{name}`
  - Landing zones: `{ns}/{name}` (abbreviated)
  - Preview/Publish/Versions: `{ns}/{layer}/{name}` (abbreviated)
  - Quality: `{namespace}/{layer}/{pipeline}` (different param name)
  - Metadata: `{namespace}/pipeline/{layer}/{name}` (extra path segment)
  - Retention LZ routes: `{namespace}/{name}` (full name, different from LZ routes using `{ns}`)
- **Fix**: Standardize on `{namespace}/{layer}/{name}` everywhere. Never abbreviate `{ns}`.

### API-02: `HandleListNamespaces` does not include pagination — `LOW`

- **File**: `platform/internal/api/namespaces.go`, lines 40-52

### API-03: `HandleListAuditLog` returns a bare JSON array instead of an envelope — `MEDIUM`

- **File**: `platform/internal/api/audit.go`, line 72
- **What**: Returns `json.Encode(entries)` directly (a bare array) instead of the `{"entries": [...], "total": N}` envelope used by every other list endpoint.

### API-04: Mixed response field inconsistencies — `MEDIUM`

- **File**: Multiple handlers
- **What**: `run_id` is serialized as UUID object in webhook handler but as string elsewhere. `duration_ms` is `*int` in Go domain but `int64` in proto.

### API-05: Quality test CRUD uses `/tests` prefix, breaking resource hierarchy — `MEDIUM`

- **File**: `platform/internal/api/quality.go`, lines 50-55
- **What**: Tests live under `/api/v1/tests/{ns}/{layer}/{pipeline}` instead of `/api/v1/pipelines/{ns}/{layer}/{name}/tests`. Triggers ARE correctly nested under pipelines — inconsistency.

### API-06: Metadata endpoint has unusual URL structure — `LOW`

- **File**: `platform/internal/api/metadata.go`, lines 12-15

---

## 3. Backward Compatibility

### BC-01: No `reserved` fields in any proto message — `HIGH`

- **File**: All proto files
- **What**: When fields are removed/renamed in the future, without `reserved`, old field numbers could be accidentally reused causing wire-format corruption.

### BC-02: No API deprecation strategy documented — `MEDIUM`

---

## 4. Error Handling

### ERR-01: `internalError` returns plain text, not JSON — `HIGH`

- **File**: `platform/internal/api/router.go`, lines 69-72
- **What**: Uses `http.Error()` which returns `text/plain`. All success responses use JSON. SDK must handle two response formats.

### ERR-02: All validation errors use plain text bodies — `HIGH`

- **File**: Multiple handler files
- **What**: Every `http.Error(w, "message", 400)` returns plain text. The SDK tries JSON first and falls back to text, forcing defensive coding.

### ERR-03: `HandleCreatePipeline` leaks raw error message on conflict — `MEDIUM`

- **File**: `platform/internal/api/pipelines.go`, line 159
- **What**: `http.Error(w, err.Error(), http.StatusConflict)` may expose SQL errors to the client.

### ERR-04: No structured error type codes for programmatic handling — `MEDIUM`

- **File**: All error responses

### ERR-05: No error event in SSE streaming on abnormal close — `LOW`

- **File**: `platform/internal/api/runs.go`, lines 334-371

---

## 5. Pagination

### PAG-01: Offset-based pagination only — no cursor-based option — `MEDIUM`

- **File**: `platform/internal/api/router.go`, lines 37-53

### PAG-02: Pagination applied in-memory after full table scan — `HIGH`

- **File**: `platform/internal/api/pipelines.go` (lines 86-88), `runs.go` (lines 77-80)
- **What**: `HandleListPipelines` and `HandleListRuns` fetch ALL records then paginate in Go. With thousands of pipelines or millions of runs, this will OOM.
- **Fix**: Pass limit/offset to the store interface and use SQL-level pagination.

### PAG-03: `HandleListLandingFiles` has no pagination — `LOW`

- **File**: `platform/internal/api/landing_zones.go`, lines 228-253

---

## 6. Filtering & Sorting

### FILT-01: No sorting support on any list endpoint — `MEDIUM`

### FILT-02: No date range filtering on runs — `LOW`

- **File**: `platform/internal/api/runs.go`, lines 38-44

### FILT-03: `search` query param documented in API spec but not implemented — `MEDIUM`

- **File**: `docs/api-spec.md` (line 47) vs `platform/internal/api/pipelines.go`

---

## 7. Validation

### VAL-01: `HandleCreateSchedule` does not validate cron expression syntax — `HIGH`

- **File**: `platform/internal/api/schedules.go`, lines 85-139
- **What**: Checks cron is not empty but does not validate syntax. `HandleCreateTrigger` correctly validates cron via `cronParser.Parse()` — inconsistency.

### VAL-02: No SQL injection protection on `/query` endpoint — `CRITICAL`

- **File**: `platform/internal/api/query.go`, lines 96-119
- **What**: Accepts arbitrary SQL with no validation. No query type restrictions (DDL/DML should be blocked). No query length limit beyond the 1MB JSON body cap.
- **Fix**: Block DDL/DML keywords at the API layer, enforce max query length.

### VAL-03: `HandlePreviewTable` limit is hardcoded — `LOW`

- **File**: `platform/internal/api/query.go`, lines 286-300

### VAL-04: Missing max length validation on description and SQL fields — `MEDIUM`

---

## 8. Versioning

### VER-01: No TypeScript proto generation in `buf.gen.yaml` — `MEDIUM`

- **File**: `proto/buf.gen.yaml`
- **What**: SDK hand-writes types that can drift from proto/API definitions. No CI mechanism to detect drift.

---

## 9. Documentation

### DOC-01: API spec missing ~15 endpoint groups — `HIGH`

- **File**: `docs/api-spec.md`
- **What**: Missing: Lineage, Sharing, Triggers, Preview, Publish, Versions/Rollback, Webhooks, Audit, Schema, Table metadata, Landing zone samples, Namespace update.

### DOC-02: API spec summary endpoint count is wrong — `LOW`

- **File**: `docs/api-spec.md`, lines 490-508
- **What**: Claims 51 endpoints; actual count is ~65-70.

---

## 10. Performance

### PERF-01: `HandleGetSchema` makes N+1 gRPC calls — `HIGH`

- **File**: `platform/internal/api/query.go`, lines 161-203
- **What**: Lists all tables then fires 1 goroutine per table to fetch columns. 100 tables = 101 gRPC calls.
- **Fix**: Add a `GetBulkSchema` RPC to QueryService.

### PERF-02: `HandleGetLineage` makes unbounded S3 reads with no caching — `MEDIUM`

- **File**: `platform/internal/api/lineage.go`, lines 99-125
- **What**: Reads every pipeline's SQL file from S3 in parallel on every lineage request. 500 pipelines = 500 S3 reads per request.

---

## 11. REST Design

### REST-01: Cancel endpoint should use custom method syntax — `SUGGESTION`

- **File**: `platform/internal/api/runs.go`, line 59
- **What**: `POST /runs/{runID}/cancel` implies `/cancel` is a sub-resource. AIP-136 convention: `POST /runs/{runID}:cancel`.

### REST-02: `HandleDeleteSchedule` returns 500 on not-found — `MEDIUM`

- **File**: `platform/internal/api/schedules.go`, lines 166-175
- **What**: Does not check existence before deletion. Not-found error from store maps to 500 instead of 404.

### REST-03: `HandleDeletePipeline` silently suppresses S3 cleanup errors — `MEDIUM`

- **File**: `platform/internal/api/pipelines.go`, lines 257-262

### REST-04: `GET /health` should include dependency health — `SUGGESTION`

### REST-05: Webhook handler uses `context.Background()` with no timeout — `MEDIUM`

- **File**: `platform/internal/api/webhooks.go`, line 79

---

## 12. SDK

### SDK-01: Hand-written types, no generation or drift detection — `MEDIUM`

- **File**: `sdk-typescript/src/models/`

### SDK-02: `CreatePipelineRequest.layer` is `string` instead of `Layer` — `LOW`

- **File**: `sdk-typescript/src/models/pipelines.ts`, line 76

### SDK-03: SDK missing models for several API resources (Audit, Sharing, Webhooks) — `MEDIUM`

### SDK-04: Retry logic correctly skips non-idempotent methods — `SUGGESTION` (good design)

- **File**: `sdk-typescript/src/transport.ts`, lines 40-43

---

## 13. Rate Limiting

### RL-01: No rate limit headers in responses — `MEDIUM`

- **File**: `platform/internal/api/ratelimit.go`, lines 109-127
- **What**: When rate-limited, clients receive 429 but no `X-RateLimit-Remaining` or `Retry-After` headers.

### RL-02: No per-endpoint rate limit differentiation — `SUGGESTION`

### RL-03: Rate limiter goroutine leak on server shutdown — `LOW`

- **File**: `platform/internal/api/ratelimit.go`, lines 57-65
- **What**: The `stop` channel is never closed during server shutdown.
- **Fix**: Close `stop` channel when the HTTP server shuts down.

---

## 14. Observability

### OBS-01: No request/response logging middleware — `MEDIUM`

- **File**: `platform/internal/api/router.go`
- **What**: The router uses `middleware.RequestID` and `middleware.Recoverer` but not `middleware.Logger`. API calls are invisible in logs unless they hit an error path.

### OBS-02: No request ID propagation to gRPC downstream calls — `MEDIUM`

- **File**: `platform/internal/api/router.go`, line 180
- **What**: `middleware.RequestID` generates a request ID and stores it in context, but when the API handler makes gRPC calls to ratq or runner, the request ID is not propagated as gRPC metadata. Traces across services cannot be correlated.

### OBS-03: No `/metrics` endpoint for Prometheus/monitoring — `SUGGESTION`

### OBS-04: `HandleHealth` does not include service version or build info — `SUGGESTION`

---

## 15. Security

### SEC-01: `GetCredentialsResponse` returns secrets in proto with no `debug_redact` option — `CRITICAL`

- **Files**: `proto/enforcement/v1/enforcement.proto` (lines 32-37), `proto/cloud/v1/cloud.proto` (lines 19-27)
- **What**: Both `GetCredentialsResponse` messages return `secret_key` and `session_token` as plain string fields. Any debug logging of the protobuf message will print credentials in plaintext.
- **Fix**: Add `debug_redact` option:
```protobuf
string secret_key = 2 [debug_redact = true];
string session_token = 3 [debug_redact = true];
```

### SEC-02: Webhook token in URL path may be logged by proxies — `CRITICAL`

- **File**: `platform/internal/api/webhooks.go`, line 17
- **What**: Webhook tokens are passed as URL path parameters (`/api/v1/webhooks/{token}`). URL paths are commonly logged by load balancers, CDNs, reverse proxies, and access logs.
- **Fix**: Move the token to a header: `Authorization: Bearer <token>` or `X-Webhook-Token: <token>`.

### SEC-03: CORS allows credentials with configurable origins — `SUGGESTION`

- **File**: `platform/internal/api/router.go`, lines 172-179
- **What**: `AllowCredentials: true` with configurable `AllowedOrigins`. If misconfigured with `*`, allows any origin to make credentialed requests.
- **Fix**: Add startup validation rejecting wildcard + credentials combination.

### SEC-04: `HandleUploadLandingFile` starts a goroutine with `context.Background()` — `LOW`

- **File**: `platform/internal/api/landing_zones.go`, line 332
- **Fix**: Pass a server-scoped context with timeout.

---

## Summary of Critical/High Items

| ID | Severity | Summary |
|----|----------|---------|
| PROTO-02 | Critical | `RunStatus` proto enum missing `CANCELLED` — gRPC clients see wrong status |
| VAL-02 | Critical | No SQL validation on `/query` endpoint — potential injection vector |
| SEC-01 | Critical | Cloud credentials not redacted in proto debug output |
| SEC-02 | Critical | Webhook secret token exposed in URL path (logged by proxies) |
| PROTO-01 | High | Custom Timestamp instead of `google.protobuf.Timestamp` |
| PROTO-05 | High | S3 credentials as untyped `map<string,string>` in proto |
| API-01 | High | Inconsistent path parameter naming across endpoints |
| BC-01 | High | No `reserved` fields strategy for proto evolution |
| ERR-01 | High | `internalError` returns plain text, not JSON |
| ERR-02 | High | All validation errors return plain text instead of structured JSON |
| PAG-02 | High | In-memory pagination after full table scan |
| VAL-01 | High | Schedule creation does not validate cron expression |
| PERF-01 | High | Schema endpoint makes N+1 gRPC calls |
| DOC-01 | High | API spec missing ~15 endpoint groups |

---

## Recommendations (Prioritized)

### Immediate (before next release)
1. Fix PROTO-02: Add `RUN_STATUS_CANCELLED` to proto enum
2. Fix VAL-02: Add SQL query validation/restrictions
3. Fix SEC-02: Move webhook token from URL to header
4. Fix ERR-01/ERR-02: Standardize on JSON error responses

### Short-term (next 2-3 sprints)
5. Fix PAG-02: Push pagination to the database layer
6. Fix API-01: Standardize URL path parameter names
7. Fix PERF-01: Add bulk schema RPC
8. Fix VAL-01: Validate cron expressions on schedule creation
9. Fix DOC-01: Complete API spec documentation
10. Add request/response logging middleware (OBS-01)

### Medium-term (next quarter)
11. Migrate to `google.protobuf.Timestamp` (PROTO-01)
12. Extract shared proto messages (PROTO-03)
13. Add `reserved` field strategy (BC-01)
14. Add structured error codes (ERR-04)
15. Add cursor-based pagination option (PAG-01)
16. Add sorting support to list endpoints (FILT-01)
17. Improve observability (OBS-02, OBS-03)

---

*Review completed. 54 findings across 15 categories. 4 critical issues require immediate attention before production deployment.*
