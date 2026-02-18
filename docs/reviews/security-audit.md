# RAT v2 Security Audit Report

**Date**: 2026-02-16
**Auditor**: Senior Security Engineer (Penetration Test & Architecture Review)
**Scope**: Full codebase — ratd (Go), runner (Python), ratq (Python), portal (Next.js), infrastructure (Docker Compose), proto definitions
**Branch**: `feat/ratd-health-and-pipelines`

---

## Executive Summary

This audit covers the RAT v2 data platform — a self-hostable system comprising a Go API server (ratd), two Python services (runner, ratq), a Next.js portal, PostgreSQL, MinIO (S3), and Nessie (Iceberg catalog). The platform uses gRPC for inter-service communication and REST for the public-facing API.

### Overall Risk Rating: **MODERATE-HIGH**

The codebase demonstrates security awareness in several areas (input validation, Jinja sandboxing, SQL statement blocking, rate limiting, path traversal prevention). However, critical vulnerabilities exist in the Python exec() sandbox, SQL injection surface in the Iceberg merge layer, absence of authentication in community mode, and cleartext inter-service communication.

### Risk Summary

| Severity | Count | Description |
|----------|-------|-------------|
| CRITICAL | 3 | Python sandbox escape, DuckDB SQL injection via column names, hardcoded credentials |
| HIGH | 6 | No auth in community mode, insecure gRPC, query service SQL injection, SSRF vectors, unrestricted file listing, webhook token timing attack |
| MEDIUM | 8 | Missing security headers, CORS misconfiguration potential, log information leakage, unbounded SSE connections, missing ReadTimeout, S3 credential caching, Nessie branch name injection, YAML schema validation |
| LOW | 5 | Docker image pinning, TLS minimum version, missing CSP, verbose error messages, missing request ID logging |

---

## CRITICAL Findings

---

### SEC-001: Python exec() Sandbox Escape via Built-in Type Introspection

**Severity**: CRITICAL | **CVSS**: 9.8 | **OWASP**: A03:2021 - Injection
**File**: `runner/src/rat_runner/python_exec.py`, lines 20-136

The `execute_python_pipeline()` function uses `exec()` to run user-supplied Python code with a restricted builtins dictionary. While common dangerous functions are blocked (`eval`, `exec`, `open`, `getattr`, etc.), the sandbox is fundamentally bypassable through Python object introspection.

**Attack Scenario**:
```python
# Bypass via __subclasses__ through object hierarchy
result_class = "".__class__.__mro__[1].__subclasses__()
# Find os._wrap_close or similar, then access os.system

# Bypass via injected function globals
ref.__globals__  # ref is an injected function - its __globals__ leaks the real builtins

# Bypass via exposed duckdb_conn
duckdb_conn.execute("COPY (SELECT 1) TO '/etc/cron.d/pwned' (HEADER FALSE)")
```

**Remediation**:
1. Block `type`, `__class__`, `__mro__`, `__subclasses__`, `__globals__`, `__code__`, `__builtins__` attribute access via AST visitor.
2. Medium-term: run Python pipelines in isolated subprocesses with seccomp filters or in ephemeral containers.
3. Long-term: Consider RestrictedPython or a proper sandbox runtime.

---

### SEC-002: SQL Injection via Unsanitized Column Names in Iceberg Merge Operations

**Severity**: CRITICAL | **CVSS**: 9.1 | **OWASP**: A03:2021 - Injection
**File**: `runner/src/rat_runner/iceberg.py`, lines 163-188 (and similar at 277-285, 368-409, 467-474)

The `merge_iceberg()`, `delete_insert_iceberg()`, `scd2_iceberg()`, `snapshot_iceberg()`, and `read_watermark()` functions construct SQL queries by directly interpolating column names from `unique_key`, `watermark_column`, `partition_column`, `valid_from_col`, and `valid_to_col` parameters without validation or quoting. These values originate from user-controlled `config.yaml` or code annotations.

**Attack Scenario**: A malicious `config.yaml` with `unique_key: "id; DROP TABLE existing; --"` injects arbitrary SQL into merge operations. While DuckDB's in-memory nature limits persistent damage, attackers can exfiltrate cross-namespace data via UNION injection.

**Remediation**: Validate and quote all column names:
```python
_SAFE_IDENTIFIER = re.compile(r"^[a-zA-Z_][a-zA-Z0-9_]*$")
def _quote_identifier(name: str) -> str:
    if not _SAFE_IDENTIFIER.match(name):
        raise ValueError(f"Invalid SQL identifier: {name!r}")
    return f'"{name}"'
```

---

### SEC-003: Hardcoded Default Credentials Across All Services

**Severity**: CRITICAL | **CVSS**: 9.0 | **OWASP**: A07:2021 - Identification and Authentication Failures
**Files**: `runner/src/rat_runner/config.py` (lines 20-23), `infra/docker-compose.yml` (lines 18-24, 116-117, 136-137)

S3Config defaults to `minioadmin`/`minioadmin`. Docker-compose uses `rat`/`rat` for Postgres. If environment variables are not set, the platform silently uses these known credentials.

**Remediation**:
1. Remove hardcoded defaults; require explicit credential configuration.
2. Add startup warnings for default credentials.
3. Use Docker secrets or `.env` file (gitignored) with `.env.example` committed.

---

## HIGH Findings

---

### SEC-004: No Authentication in Community Edition

**Severity**: HIGH | **CVSS**: 8.2 | **OWASP**: A01:2021 - Broken Access Control
**Files**: `platform/internal/auth/middleware.go` (lines 10-14), `platform/internal/api/authorizer.go` (lines 17-21)

All API endpoints are completely open in community mode. Any network-reachable client can execute arbitrary SQL queries, upload/download files, trigger pipeline runs (including Python code execution), and modify all configuration.

**Remediation**: Add basic API key authentication even in community mode; bind ratd to `127.0.0.1` by default.

---

### SEC-005: Insecure gRPC Communication (No TLS by Default)

**Severity**: HIGH | **CVSS**: 7.5 | **OWASP**: A02:2021 - Cryptographic Failures
**Files**: `runner/src/rat_runner/server.py` (line 377), `query/src/rat_query/server.py` (line 241)

All gRPC communication uses `add_insecure_port()`. S3 credentials, pipeline code, and query results are transmitted in cleartext. The Python services have no TLS option at all.

**Remediation**: Add TLS support to Python gRPC servers; make TLS the default for production.

---

### SEC-006: DuckDB Query Service Allows Dangerous SQL Functions

**Severity**: HIGH | **CVSS**: 7.8 | **OWASP**: A03:2021 - Injection
**File**: `query/src/rat_query/engine.py`, lines 167-188

The blocklist-based approach has gaps. DuckDB functions like `read_parquet()`, `read_csv_auto()` allow reading arbitrary S3 paths (cross-namespace data access). The `httpfs` extension enables outbound HTTP for data exfiltration. CTE-based bypass of the first-keyword check is possible.

**Remediation**: Switch to an allowlist approach (only `SELECT` and `WITH...SELECT`); block DuckDB read functions in user queries; add namespace-scoped query isolation.

---

### SEC-007: SSRF via Nessie Client URL Construction

**Severity**: HIGH | **CVSS**: 7.0 | **OWASP**: A10:2021 - SSRF
**File**: `runner/src/rat_runner/nessie.py`, lines 84, 98

Branch names are interpolated directly into URL paths without encoding. A branch name like `main/../../../api/v2/config` could cause path traversal against the Nessie REST API.

**Remediation**: Validate branch names against a strict pattern; URL-encode all path parameters.

---

### SEC-008: Unrestricted S3 File Listing via Prefix Parameter

**Severity**: HIGH | **CVSS**: 7.0 | **OWASP**: A01:2021 - Broken Access Control
**File**: `platform/internal/api/storage.go`, lines 62-97

`HandleListFiles` accepts an arbitrary prefix and lists all matching S3 objects with no namespace-scoping or access control. `GET /api/v1/files?prefix=` returns ALL files across ALL namespaces.

**Remediation**: Enforce namespace-scoped access on the prefix parameter.

---

### SEC-009: Webhook Token Vulnerable to Timing Attack

**Severity**: HIGH | **CVSS**: 6.5 | **OWASP**: A02:2021 - Cryptographic Failures
**File**: `platform/internal/api/webhooks.go`, line 29

Webhook tokens are looked up via database string comparison (non-constant-time), enabling timing side-channel brute-force.

**Remediation**: Hash webhook tokens before storage (SHA-256); look up by hash. Use `crypto/subtle.ConstantTimeCompare()` if comparing directly.

---

## MEDIUM Findings

---

### SEC-010: Missing HTTP Security Headers
**File**: `platform/internal/api/router.go`
Missing `X-Content-Type-Options`, `X-Frame-Options`, HSTS, CSP headers.

### SEC-011: CORS Configuration Allows Credential Leakage
**File**: `platform/internal/api/router.go` (lines 168-179)
`AllowCredentials: true` with configurable origins via unvalidated env var. A wildcard origin with credentials would be dangerous.

### SEC-012: Sensitive Data Logged in Pipeline Execution
**File**: `runner/src/rat_runner/executor.py` (line 238)
Compiled SQL with S3 paths logged at INFO; quality test violation rows with potential PII logged. These logs are persisted to Postgres and accessible via API.

### SEC-013: Unbounded SSE Streaming Connections
**File**: `platform/internal/api/runs.go` (lines 288-372)
No max duration, per-user limit, or global SSE connection cap. DoS vector.

### SEC-014: Missing ReadTimeout/WriteTimeout on HTTP Server
**File**: `platform/cmd/ratd/main.go` (lines 253-258)
No `ReadTimeout` or `WriteTimeout` set. Slowloris attack vector.

**Remediation**:
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

### SEC-015: S3 Credentials Cached in lru_cache
**File**: `runner/src/rat_runner/config.py` (lines 175-189)
STS session tokens persisted in memory indefinitely via `@functools.lru_cache`.

### SEC-016: Nessie Branch Name Injection in URL Paths
**Files**: `runner/src/rat_runner/nessie.py`, `platform/internal/reaper/nessie_client.go`
No URL-encoding of path parameters. Path traversal possible with crafted branch names.

### SEC-017: YAML Config Not Schema-Validated
**File**: `runner/src/rat_runner/config.py` (line 108)
Uses `safe_load` (good) but accepts any key-value pair silently. Unknown keys not flagged.

---

## LOW Findings

---

### SEC-018: Docker Images Use `latest` Tags and Unpinned Base Images
**Files**: `infra/docker-compose.yml`, all Dockerfiles
`minio/minio:latest`, `minio/mc:latest`, `ghcr.io/projectnessie/nessie:latest` use unpinned tags. Base images use semi-pinned tags without SHA256 digests.

### SEC-019: TLS Minimum Version Could Be Strengthened
**File**: `platform/internal/transport/grpc.go`, line 70
TLS 1.2 minimum — TLS 1.3 preferred for new deployments.

### SEC-020: Portal Next.js Missing Content-Security-Policy
**File**: `portal/next.config.mjs`
No CSP, X-Content-Type-Options, or X-Frame-Options headers configured.

### SEC-021: Verbose Error Messages Expose Internal Details
**Files**: `query/src/rat_query/server.py` (DuckDB errors returned to clients), `runner/src/rat_runner/server.py`
Query service returns DuckDB error messages directly to clients, exposing table names, S3 paths, and version info.

### SEC-022: Missing Request ID Propagation in gRPC Calls
ratd uses `middleware.RequestID` for HTTP but does not propagate to gRPC calls. Trace correlation across services impossible during incident response.

---

## Positive Security Practices Identified

1. **Input validation**: `validName()` regex and `ValidatePathParams` middleware
2. **Path traversal prevention**: `validateFilePath()` blocks `..`, absolute paths, null bytes
3. **Jinja sandboxing**: `SandboxedEnvironment` for SQL template rendering
4. **Rate limiting**: Per-IP token bucket with configurable parameters
5. **Request body size limits**: JSON 1MB, uploads 32MB via `MaxBytesReader`
6. **Non-root containers**: All Dockerfiles create and use non-root users
7. **Parameterized SQL**: DuckDB engine uses `?` parameters for config
8. **sqlc for Postgres**: Type-safe parameterized SQL eliminates SQL injection in platform DB
9. **Audit logging**: Middleware and `audit_log` table
10. **Graceful shutdown**: Proper signal handling and ordered cleanup

---

## Risk Matrix

| ID | Finding | Severity | Exploitability | Impact | CVSS | Priority |
|----|---------|----------|---------------|--------|------|----------|
| SEC-001 | Python exec() sandbox escape | CRITICAL | High | Critical | 9.8 | P0 |
| SEC-002 | SQL injection in Iceberg merge | CRITICAL | High | High | 9.1 | P0 |
| SEC-003 | Hardcoded default credentials | CRITICAL | High | Critical | 9.0 | P0 |
| SEC-004 | No auth in community edition | HIGH | High | High | 8.2 | P1 |
| SEC-005 | Insecure gRPC (no TLS) | HIGH | Medium | High | 7.5 | P1 |
| SEC-006 | Query service SQL injection | HIGH | High | Medium | 7.8 | P1 |
| SEC-007 | SSRF via Nessie client | HIGH | Medium | High | 7.0 | P1 |
| SEC-008 | Unrestricted S3 file listing | HIGH | High | Medium | 7.0 | P1 |
| SEC-009 | Webhook token timing attack | HIGH | Medium | Medium | 6.5 | P2 |
| SEC-010 | Missing security headers | MEDIUM | Low | Medium | 5.0 | P2 |
| SEC-011 | CORS misconfiguration risk | MEDIUM | Medium | Medium | 5.5 | P2 |
| SEC-012 | Sensitive data in logs | MEDIUM | Low | Medium | 5.0 | P2 |
| SEC-013 | Unbounded SSE connections | MEDIUM | High | Low | 5.5 | P2 |
| SEC-014 | Missing ReadTimeout | MEDIUM | Medium | Low | 5.0 | P3 |
| SEC-015 | S3 credentials in LRU cache | MEDIUM | Low | Medium | 4.5 | P3 |
| SEC-016 | Nessie branch name injection | MEDIUM | Low | Medium | 4.5 | P3 |
| SEC-017 | YAML schema validation | MEDIUM | Low | Low | 3.5 | P3 |
| SEC-018 | Unpinned Docker images | LOW | Low | Medium | 3.0 | P3 |
| SEC-019 | TLS minimum version | LOW | Low | Low | 2.5 | P4 |
| SEC-020 | Missing CSP in portal | LOW | Low | Low | 3.0 | P4 |
| SEC-021 | Verbose error messages | LOW | Low | Low | 2.5 | P4 |
| SEC-022 | Missing request ID propagation | LOW | N/A | Low | 2.0 | P4 |

---

## Recommended Remediation Timeline

### Immediate (P0 — within 1 week)
- SEC-001: Harden Python exec() sandbox, add attribute access restrictions
- SEC-002: Add SQL identifier validation/quoting in `iceberg.py`
- SEC-003: Remove hardcoded default credentials, require explicit configuration

### Short-term (P1 — within 2 weeks)
- SEC-004: Add basic API key auth for community edition
- SEC-005: Add TLS support to Python gRPC servers
- SEC-006: Switch query engine to allowlist-based SQL filtering
- SEC-007: Add branch name validation and URL encoding
- SEC-008: Add namespace-scoped access control on file listing

### Medium-term (P2 — within 1 month)
- SEC-009: Hash webhook tokens at rest
- SEC-010: Add security headers middleware
- SEC-011: Add CORS origin validation
- SEC-012: Add log sanitization layer
- SEC-013: Add SSE connection limits

### Long-term (P3-P4 — within 3 months)
- SEC-014 through SEC-022: Infrastructure hardening, dependency auditing, monitoring improvements

---

*Report generated by security audit on 2026-02-16. All findings should be verified with penetration testing before and after remediation.*
