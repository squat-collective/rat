# ADR-015: Data Retention & Landing Zone Cleanup

## Status: Accepted

## Context

RAT accumulates data across 7+ dimensions — run history, S3 log files, Iceberg snapshots, Nessie branches, quality results, audit logs, and processed landing zone files — with zero automated cleanup. Strategy doc decision #22 set targets (100 runs/pipeline, 30 day logs) but none were implemented. All storage grows unboundedly, eventually degrading performance and inflating costs.

## Decision

### Hybrid retention system

Two complementary components handle cleanup:

1. **System Reaper** (Go, `platform/internal/reaper/`) — background goroutine in ratd that handles operational cleanup on a configurable interval
2. **Iceberg Maintenance** (Python, `runner/src/rat_runner/maintenance.py`) — runs after each successful pipeline execution to maintain table-level health

### System Reaper tasks

| Task | Description |
|------|-------------|
| Prune run history | Delete runs beyond per-pipeline limit and age threshold |
| Fail stuck runs | Mark pending/running runs older than timeout as failed |
| Purge soft-deleted pipelines | Hard-delete pipelines and S3 files after grace period |
| Clean orphan Nessie branches | Delete `run-*` branches with no active run |
| Purge processed LZ files | Delete `_processed/` files per zone auto_purge settings |
| Prune audit log | Delete audit entries older than retention period |

### Iceberg Maintenance tasks

| Task | Description |
|------|-------------|
| Expire snapshots | Remove Iceberg snapshots older than configured age (PyIceberg) |
| Remove orphan files | Delete S3 data files not referenced by any snapshot |

### Configuration

All config is UI-first, stored in Postgres:

- **System defaults**: `platform_settings` table, key `"retention"` (JSONB)
- **Per-pipeline overrides**: `pipelines.retention_config` column (JSONB, nullable)
- **Per-zone lifecycle**: `landing_zones.auto_purge` + `processed_max_age_days` columns

The reaper reads merged config (system + overrides) at each tick. No restart required.

### API

8 new REST endpoints:

- `GET/PUT /api/v1/admin/retention/config` — system retention config
- `GET /api/v1/admin/retention/status` — reaper last-run statistics
- `POST /api/v1/admin/retention/run` — trigger manual reaper run
- `GET/PUT /api/v1/pipelines/{ns}/{layer}/{name}/retention` — pipeline overrides
- `GET/PUT /api/v1/landing-zones/{ns}/{name}/lifecycle` — zone lifecycle

### S3 lifecycle

MinIO ILM rule expires noncurrent S3 object versions after 7 days, added via `minio-init` entrypoint.

## Consequences

### Positive

- Storage stops growing unboundedly — all 7 data dimensions have automated cleanup
- Configurable per-pipeline — hot pipelines can retain more history
- Best-effort design — cleanup failures don't affect pipeline execution
- UI-first config — no YAML files or env vars needed, all managed from portal
- Reaper follows existing scheduler pattern — consistent codebase

### Negative

- Adds complexity to ratd (new background goroutine, new store methods)
- Iceberg maintenance adds latency to pipeline runs (mitigated: best-effort, only on success)
- PyIceberg dependency for maintenance tasks (already a dependency for Iceberg writes)
- `platform_settings` table is a new general-purpose config store that may attract feature creep

## Alternatives Considered

1. **Cron job / external script** — rejected: adds operational complexity, breaks self-hosted simplicity
2. **Config via rat.yaml** — rejected: UI-first principle, per-pipeline overrides need database storage
3. **S3 lifecycle only** — rejected: doesn't cover Postgres data (runs, quality results, audit log)
