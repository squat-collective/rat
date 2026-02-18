# RAT v2 — Architecture & Scalability Review

**Reviewer**: Principal Software Architect
**Date**: 2026-02-16
**Scope**: Full platform review — ratd, runner, ratq, portal, infrastructure
**Branch**: `feat/ratd-health-and-pipelines`

---

## Executive Summary

RAT v2 is a well-designed data platform for its stated target: a single-user, self-hosted "SQLite of data platforms." The Go-based API server (ratd), Python execution sidecars (runner, ratq), and Next.js portal form a clean microservices architecture with good separation of concerns. The plugin system is one of the strongest architectural decisions — it cleanly separates Community and Pro editions without contaminating the core.

However, there are significant architectural gaps that will become friction points as the platform scales toward multi-user Pro deployments. The system is predominantly synchronous and pull-based, lacking event-driven patterns that distributed data platforms require. There is no message queue, no distributed tracing, no circuit breakers, and several single-points-of-failure. The Iceberg merge strategy (full table overwrite) is a scalability time-bomb. The in-memory run registry in the runner is fragile against crashes.

**Overall Assessment**: Strong foundation for Community Edition. **Not production-ready for multi-user Pro deployments** without addressing the critical and high-severity findings below.

### Finding Summary

| Severity | Count |
|----------|-------|
| Critical | 4 |
| High | 9 |
| Medium | 11 |
| Low | 6 |
| Suggestion | 7 |
| **Total** | **37** |

---

## Architecture Diagram Assessment

The current architecture is a classic hub-and-spoke with ratd as the central coordinator:

```
Portal --> ratd --> runner   (gRPC)
                --> ratq     (gRPC)
                --> postgres  (SQL)
                --> minio     (S3 API)
                --> nessie    (REST)
                --> plugins   (gRPC)
```

**Strengths**:
- Single entry point (ratd) simplifies auth, routing, and observability
- gRPC (ConnectRPC) for internal communication is the right choice
- Stateless services (ratd, portal) enable horizontal scaling in theory
- Plugin-as-container model provides strong isolation boundaries

**Weaknesses**:
- ratd is a God Service — API server, scheduler, trigger evaluator, reaper, plugin host all in one process
- No async communication channel between services (no message queue)
- All traffic funnels through ratd, creating a bottleneck
- No service mesh or sidecar pattern for cross-cutting concerns

---

## Detailed Findings

### 1. System Design

#### F-01: ratd Monolith Risk — `HIGH`

- **Current State**: ratd runs the HTTP API server, cron scheduler, trigger evaluator, reaper daemon, and plugin host all in a single Go process. These are started as goroutines in `platform/cmd/ratd/main.go` (lines 207-232).
- **Risk**: A panic in the reaper or scheduler can destabilize the API. You cannot scale the API independently from the scheduler — if you run 3 ratd replicas, you get 3 schedulers firing duplicate runs.
- **Recommendation**: Extract scheduler/reaper into a separate `ratd-worker` process. Use Postgres advisory locks for leader election. Short-term: add `SCHEDULER_ENABLED=true/false` env var.

#### F-02: Runner In-Memory State is Fragile — `CRITICAL`

- **Current State**: The runner stores all active run state in a Python dict (`runner/src/rat_runner/server.py` line 68). If the runner crashes, all in-flight state is lost.
- **Risk**: OOM, segfault, or restart silently drops runs. They appear stuck until the reaper times them out (default 120 min).
- **Recommendation**: Persist minimal run state to Redis/SQLite/S3. Add startup reconciliation. Reduce stuck timeout to 15-30 min.

#### F-03: Tight Coupling via Polling — `MEDIUM`

- **Current State**: ratd polls runner every 5s per active run (`platform/internal/executor/warmpool.go` line 42).
- **Risk**: 100 concurrent runs = 100 gRPC calls/5s just for status. Up to 5s latency for status changes.
- **Recommendation**: Add `StreamRunStatus` push-based gRPC RPC.

#### F-04: No Service Discovery — `LOW`

- **Current State**: Service addresses are hardcoded via environment variables (`RUNNER_ADDR`, `QUERY_ADDR`). This is appropriate for Compose but does not scale to dynamic environments.
- **Recommendation**: For Kubernetes, use DNS-based service discovery. Document the expected deployment patterns.

---

### 2. Scalability

#### F-05: Single Runner is a Hard Bottleneck — `CRITICAL`

- **Current State**: One runner with `ThreadPoolExecutor(max_workers=4)`. All pipeline execution funnels through 4 Python threads.
- **Risk**: 50+ scheduled pipelines saturate the runner. DuckDB holds GIL for native operations.
- **Recommendation**: Support multiple runner replicas. Allow `RUNNER_ADDR` as comma-separated list with round-robin dispatch. Make `MAX_WORKERS` configurable.

#### F-06: ratq Single Connection — `MEDIUM`

- **Current State**: Single DuckDB connection in `query/src/rat_query/engine.py`. No per-query timeout or memory limit.
- **Risk**: One large query can exhaust memory for all other queries.
- **Recommendation**: Add per-query timeout, memory tracking, and consider multiple ratq replicas for Pro.

#### F-07: Postgres as Bottleneck — `LOW`

- **Current State**: No connection pool configuration (`postgres/conn.go`). Default pgxpool settings used.
- **Recommendation**: Configure `MaxConns`, `MinConns`, `MaxConnLifetime` based on deployment size.

#### F-08: Portal SSR Adds Latency — `SUGGESTION`

- **Current State**: Server components fetch data from ratd on every render with `cache: "no-store"`.
- **Recommendation**: Use `next.revalidate` for time-based caching on slowly-changing data.

---

### 3. Reliability & Fault Tolerance

#### F-09: No Circuit Breakers — `HIGH`

- **Current State**: Direct HTTP/gRPC calls to runner, ratq, Nessie, MinIO with no circuit breaker, retry, or backoff. Nessie calls use 10s timeout but no retry.
- **Risk**: A flaky Nessie or S3 cascades into total platform failure.
- **Recommendation**: Add `sony/gobreaker` for Go calls, retry with exponential backoff on runner Nessie/S3 calls, request-level timeouts on all gRPC client calls.

#### F-10: Scheduler Has No Distributed Locking — `HIGH`

- **Current State**: The scheduler runs in every ratd instance. Multiple instances = duplicate pipeline runs.
- **Recommendation**: Use Postgres advisory locks (`pg_advisory_lock`) for leader election.

#### F-11: Nessie Branch Accumulation — `HIGH`

- **Current State**: Ephemeral branches `run-{run_id}` accumulate between reaper runs (default 60 min). A burst of failed pipelines can create hundreds of orphan branches.
- **Recommendation**: Make `create_branch` idempotent, reduce orphan cleanup frequency.

#### F-12: No Backpressure — `HIGH`

- **Current State**: Unlimited run submissions. Runner queues internally with no feedback. The scheduler keeps submitting even when the runner is saturated.
- **Recommendation**: Add pending run limits per pipeline. Return `RESOURCE_EXHAUSTED` when the limit is reached. Use a proper work queue instead of in-process ThreadPoolExecutor.

---

### 4. Data Architecture

#### F-13: Iceberg Merge Does Full Table Rewrites — `CRITICAL`

- **Current State**: All strategies except `append_only` read the full existing table, merge in DuckDB, and `table.overwrite()` the entire result (`runner/src/rat_runner/iceberg.py`). For 10M rows + 1K new rows = write 10M+1K rows.
- **Risk**: Write amplification grows linearly with table size. Impractical above 100M rows.
- **Recommendation**: Use PyIceberg native row-level deletes + append for `incremental` and `delete_insert`. Partition tables for scoped overwrites.

#### F-14: No Partitioning Strategy — `HIGH`

- **Current State**: Tables created without partition specs. No way for users to define partitioning.
- **Recommendation**: Add `partition_by` to `config.yaml`, pass to `catalog.create_table()`.

#### F-15: ref() Uses Parquet Glob, Not Iceberg — `MEDIUM`

- **Current State**: `ref()` resolves to `read_parquet('s3://.../**/*.parquet')` which reads orphaned files and ignores Iceberg metadata (snapshots, deletes).
- **Recommendation**: Migrate to `iceberg_scan()` with metadata file path from the catalog.

#### F-16: Watermark Scans Full Table — `MEDIUM`

- **Current State**: `read_watermark` loads entire table via PyIceberg scan to compute `MAX(column)`.
- **Recommendation**: Use Iceberg column statistics or DuckDB `iceberg_scan` push-down.

---

### 5. Consistency

#### F-17: Run Status Split-Brain — `HIGH`

- **Current State**: Status lives in Postgres (ratd) and runner memory simultaneously. Network partition causes divergence.
- **Recommendation**: Make Postgres single source of truth. Runner pushes status to ratd instead of ratd polling.

#### F-18: No Transaction Boundaries — `MEDIUM`

- **Current State**: Pipeline creation writes Postgres then S3 as separate operations. S3 failure leaves orphaned DB record.
- **Recommendation**: Create Postgres record last (after S3), or use saga pattern with compensation.

---

### 6. Event-Driven Patterns

#### F-19: No Message Queue or Event Bus — `HIGH`

- **Current State**: All communication is synchronous. Trigger evaluator polls every 30s. No way to add consumers without modifying ratd.
- **Recommendation**: Postgres `LISTEN/NOTIFY` for events (`run.completed`, `pipeline.created`). Zero new dependencies for Community. Enables trigger evaluator to react instantly.

---

### 7. Caching

#### F-20: No Caching Layer — `MEDIUM`

- **Current State**: Every API request hits Postgres. ratq re-registers all views every 30s regardless of changes. Lineage endpoint reads every pipeline's SQL from S3 on every request.
- **Recommendation**: In-memory cache for slowly-changing data (namespaces, pipeline metadata). ETags for API responses. Check Nessie commit hash before re-registering views.

#### F-21: View Re-registration is Wasteful — `LOW`

- **Current State**: `_register_tables` in ratq creates `CREATE OR REPLACE VIEW` for every discovered table every 30s.
- **Recommendation**: Compare the Nessie response against the last known state. Only re-register if changed.

---

### 8. Observability

#### F-22: No Distributed Tracing — `HIGH`

- **Current State**: There is no trace ID propagation across services. When a portal request goes through ratd → runner → S3 → Nessie, there is no way to correlate logs across these services.
- **Risk**: Debugging production issues requires manually correlating timestamps across 5+ service log streams.
- **Recommendation**: Add OpenTelemetry tracing. Propagate `request_id` (from chi middleware) via gRPC metadata headers. Include in all log messages.

#### F-23: Health Check is Too Shallow — `MEDIUM`

- **Current State**: The `/health` endpoint returns `{"status": "healthy"}` unconditionally. It does not check Postgres connectivity, S3 reachability, runner availability, or ratq health.
- **Recommendation**: Add dependency health checks. Separate `/health/live` (is the process running?) and `/health/ready` (can it serve requests?) endpoints.

#### F-24: Structured Logging is Good but Incomplete — `LOW`

- **Current State**: ratd uses `slog` with JSON handler. Runner and ratq use Python's `logging` module. No correlation between them.
- **Recommendation**: Standardize log field names across Go and Python: `run_id`, `pipeline`, `namespace`, `trace_id`.

---

### 9. Deployment & Operations

#### F-25: No Database Migration Locking — `MEDIUM`

- **Current State**: Migrations use a custom embedded FS approach. No rollback mechanism, no migration locking, no checksums. Two ratd instances starting simultaneously could apply migrations concurrently.
- **Recommendation**: Add `pg_advisory_lock` around migration execution. Consider `goose` or `golang-migrate`.

#### F-26: No Blue-Green or Canary Deployment Support — `LOW`

- **Current State**: Docker Compose is the only deployment model. Upgrades require downtime.
- **Recommendation**: For Community, acceptable. For Pro, document Kubernetes deployment with rolling updates.

#### F-27: No Feature Flags for Gradual Rollout — `SUGGESTION`

- **Current State**: Features are binary on/off via plugins. No mechanism for A/B testing or kill switches.
- **Recommendation**: Extend `platform_settings` table for feature flags.

---

### 10. Multi-Tenancy

#### F-28: No Resource Quotas or Isolation — `HIGH` (Pro)

- **Current State**: All users share the same runner, ratq, and storage. No CPU/memory quotas per user, no query timeout per user, no storage quota per namespace.
- **Risk**: Classic noisy neighbor problem.
- **Recommendation**: Per-namespace resource quotas: max concurrent runs, max storage size, max query duration. Store in `namespaces` table.

---

### 11. Plugin Architecture

#### F-29: No Plugin Hot-Reload — `LOW`

- **Current State**: Plugins loaded once at startup. If a plugin becomes unhealthy after startup, ratd continues using the stale reference.
- **Recommendation**: Add periodic health checks (every 30s) for loaded plugins. If unhealthy, set to disabled. Re-enable on recovery.

#### F-30: Plugin Versioning Not Enforced — `SUGGESTION`

- **Current State**: No version negotiation between ratd and plugins.
- **Recommendation**: Add `version` field to `HealthCheckResponse`. Check compatibility at startup.

---

### 12. Data Pipeline Patterns

#### F-31: No DAG-Based Execution Ordering — `MEDIUM`

- **Current State**: Pipeline dependencies expressed through `ref()` but execution ordering is user-managed. No cycle detection. No automated "run after upstream completes."
- **Recommendation**: Build dependency graph from `ref()` calls. Validate for cycles. Add `pipeline_success` trigger type.

#### F-32: No Retry Logic for Failed Pipelines — `MEDIUM`

- **Current State**: Failed runs stay failed. No automatic retry with backoff.
- **Recommendation**: Add configurable retry settings per pipeline: `max_retries`, `retry_delay_seconds`, `retry_backoff_multiplier`.

#### F-33: Pipeline Idempotency Depends on Merge Strategy — `SUGGESTION`

- **Current State**: `full_refresh` and `incremental` with dedup are idempotent. `append_only` is NOT — re-running appends duplicates.
- **Recommendation**: Document idempotency guarantees per merge strategy. Add optional dedup for `append_only`.

---

### 13. Storage

#### F-34: S3 Operations Lack Retry and Timeout — `MEDIUM`

- **Current State**: MinIO SDK used with default settings. No custom timeout, no retry configuration.
- **Recommendation**: Configure explicit timeouts (10s metadata, 60s data). Enable SDK-level retries with exponential backoff.

---

### 14. API Gateway

#### F-35: Rate Limiting is Per-Process, Not Distributed — `MEDIUM` (Pro)

- **Current State**: In-memory token bucket per IP per ratd process.
- **Risk**: With N replicas, effective rate limit is Nx configured value.
- **Recommendation**: For Pro, use Redis-backed rate limiter shared across replicas.

---

### 15. Disaster Recovery

#### F-36: No Backup Strategy Documented or Implemented — `CRITICAL`

- **Current State**: No backup mechanism for Postgres, MinIO, or Nessie. Volume corruption or host failure = total data loss. No RTO/RPO defined.
- **Recommendation**:
  1. Document backup strategy: Postgres `pg_dump` daily, MinIO `mc mirror`.
  2. Add `make backup` target.
  3. Test restore procedure regularly.
  4. For Pro: integrate with cloud-native backup (RDS snapshots, S3 cross-region replication).

---

### 16. Security

#### F-37: Python Pipeline exec() is a Code Execution Risk — `HIGH` (Pro multi-user)

- **Current State**: Python pipelines execute arbitrary code via `exec()` with no sandboxing beyond blocked builtins.
- **Risk**: In single-user Community mode, acceptable. In multi-user Pro mode, one user could execute arbitrary OS commands.
- **Recommendation**: For Pro with ContainerExecutor: add `--security-opt=no-new-privileges`, drop capabilities, read-only filesystem, non-root user. Add `python_exec_enabled` feature flag defaulting to off in Pro multi-user mode.

---

## Summary of Recommended Priority Actions

### Phase 1 — Immediate (Before Next Major Release)

1. **F-36**: Implement backup strategy (Postgres + MinIO)
2. **F-02**: Add runner crash recovery (persist minimal run state)
3. **F-13**: Fix Iceberg merge to avoid full table rewrites for incremental/delete_insert
4. **F-09**: Add circuit breakers and retries on Nessie/S3 calls
5. **F-12**: Implement backpressure (pending run limits)

### Phase 2 — Before Pro GA

6. **F-01**: Separate scheduler/reaper from API server (leader election)
7. **F-17**: Make Postgres the single source of truth for run status
8. **F-19**: Add event bus (Postgres LISTEN/NOTIFY)
9. **F-22**: Add distributed tracing (OpenTelemetry)
10. **F-28**: Implement resource quotas for multi-tenancy
11. **F-37**: Harden Python exec() for multi-user

### Phase 3 — Scale & Polish

12. **F-05**: Support multiple runner replicas
13. **F-20**: Add caching layer
14. **F-23**: Improve health checks (deep dependency checks)
15. **F-14**: Add Iceberg partitioning support
16. **F-31**: Build dependency DAG validation
17. **F-15**: Migrate ref() to iceberg_scan()

---

## Appendix: Files Reviewed

| File | Purpose |
|------|---------|
| `docs/v2-strategy.md` | Architecture vision and decisions |
| `docs/api-spec.md` | REST API specification (51 endpoints) |
| `docs/postgres-schema.sql` | Database schema (14 tables) |
| `docs/adr/001-015` | All Architecture Decision Records |
| `platform/cmd/ratd/main.go` | Service wiring and startup |
| `platform/internal/api/router.go` | HTTP router and middleware |
| `platform/internal/api/health.go` | Health check endpoint |
| `platform/internal/api/ratelimit.go` | Per-IP rate limiting |
| `platform/internal/api/audit.go` | Audit logging middleware |
| `platform/internal/api/runs.go` | Run management endpoints |
| `platform/internal/executor/warmpool.go` | Community executor (polling) |
| `platform/internal/scheduler/scheduler.go` | Cron scheduler |
| `platform/internal/trigger/evaluator.go` | Trigger evaluator |
| `platform/internal/reaper/reaper.go` | Data retention daemon |
| `platform/internal/plugins/loader.go` | Plugin registry and health |
| `platform/internal/postgres/conn.go` | Connection pool setup |
| `platform/internal/postgres/migrate.go` | Migration system |
| `platform/internal/storage/s3store.go` | S3 storage implementation |
| `platform/internal/transport/grpc.go` | gRPC client factory |
| `runner/src/rat_runner/server.py` | Runner gRPC service |
| `runner/src/rat_runner/executor.py` | Pipeline execution lifecycle |
| `runner/src/rat_runner/engine.py` | Runner DuckDB engine |
| `runner/src/rat_runner/iceberg.py` | All Iceberg write strategies |
| `runner/src/rat_runner/nessie.py` | Nessie REST client |
| `query/src/rat_query/server.py` | Query gRPC service |
| `query/src/rat_query/engine.py` | Query DuckDB engine |
| `query/src/rat_query/catalog.py` | Nessie catalog discovery |
| `infra/docker-compose.yml` | Service orchestration |

---

*Review completed 2026-02-16. Next review recommended after addressing Phase 1 items.*
