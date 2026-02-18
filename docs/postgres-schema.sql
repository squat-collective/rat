-- 🐀 RAT v2 — Postgres Schema
-- Platform state for ratd. All data lives in S3/Iceberg — Postgres is metadata only.
--
-- Conventions:
--   - UUIDs for all primary keys (gen_random_uuid)
--   - snake_case for columns
--   - created_at / updated_at on all mutable tables
--   - soft deletes where needed (deleted_at)
--   - indexes on common query patterns

-- ============================================================
-- Namespaces
-- Community: single "default" namespace, auto-created on startup
-- Pro: multiple namespaces, admin-managed
-- ============================================================

CREATE TABLE namespaces (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(63) UNIQUE NOT NULL,          -- "default", "ecommerce", etc.
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  VARCHAR(255)                           -- NULL for Community (single user)
);

-- Seed the default namespace on first startup
INSERT INTO namespaces (name) VALUES ('default') ON CONFLICT DO NOTHING;

-- ============================================================
-- Pipelines
-- Registered when created via API. S3 is source of truth for code,
-- Postgres tracks metadata, ownership, and relationships.
-- ============================================================

CREATE TABLE pipelines (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    namespace   VARCHAR(63) NOT NULL REFERENCES namespaces(name) ON DELETE CASCADE,
    layer       VARCHAR(10) NOT NULL CHECK (layer IN ('bronze', 'silver', 'gold')),
    name        VARCHAR(255) NOT NULL,
    type        VARCHAR(10) NOT NULL DEFAULT 'sql' CHECK (type IN ('sql', 'python')),
    s3_path     VARCHAR(1024) NOT NULL,                -- full S3 key prefix
    description TEXT DEFAULT '',
    owner       VARCHAR(255),                          -- NULL for Community
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ,                           -- soft delete
    UNIQUE(namespace, layer, name)
);

CREATE INDEX idx_pipelines_namespace ON pipelines(namespace);
CREATE INDEX idx_pipelines_layer ON pipelines(namespace, layer);
CREATE INDEX idx_pipelines_owner ON pipelines(owner) WHERE owner IS NOT NULL;

-- ============================================================
-- Runs
-- Every pipeline execution is tracked. Logs stored in S3,
-- Postgres holds status + metrics for fast querying.
-- ============================================================

CREATE TABLE runs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id   UUID NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    status        VARCHAR(20) NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'running', 'success', 'failed', 'cancelled')),
    trigger       VARCHAR(100) NOT NULL DEFAULT 'manual',  -- manual, schedule:hourly, sensor:upstream
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ,
    duration_ms   INT,
    rows_written  BIGINT,
    error         TEXT,
    logs_s3_path  VARCHAR(1024),                       -- S3 path to full log file
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_runs_pipeline ON runs(pipeline_id, created_at DESC);
CREATE INDEX idx_runs_status ON runs(status) WHERE status IN ('pending', 'running');

-- ============================================================
-- Schedules
-- Cron-based triggers. ratd's scheduler evaluates these.
-- ============================================================

CREATE TABLE schedules (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id   UUID NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    cron_expr     VARCHAR(100) NOT NULL,               -- "0 * * * *", "*/5 * * * *"
    enabled       BOOLEAN NOT NULL DEFAULT true,
    last_run_id   UUID REFERENCES runs(id),
    last_run_at   TIMESTAMPTZ,
    next_run_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_schedules_next_run ON schedules(next_run_at) WHERE enabled = true;

-- ============================================================
-- Quality Tests
-- Registered tests. SQL stored in S3, metadata in Postgres.
-- ============================================================

CREATE TABLE quality_tests (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id   UUID NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    name          VARCHAR(255) NOT NULL,
    description   TEXT DEFAULT '',
    severity      VARCHAR(10) NOT NULL DEFAULT 'error' CHECK (severity IN ('error', 'warn')),
    s3_path       VARCHAR(1024) NOT NULL,              -- path to test SQL file
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(pipeline_id, name)
);

-- ============================================================
-- Quality Results
-- Latest test results per test. Updated after each quality run.
-- ============================================================

CREATE TABLE quality_results (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    test_id       UUID NOT NULL REFERENCES quality_tests(id) ON DELETE CASCADE,
    run_id        UUID REFERENCES runs(id),
    status        VARCHAR(10) NOT NULL CHECK (status IN ('passed', 'failed', 'warned', 'error')),
    value         NUMERIC,                             -- actual count/metric
    expected      NUMERIC,                             -- expected count/metric
    duration_ms   INT,
    ran_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_quality_results_test ON quality_results(test_id, ran_at DESC);

-- ============================================================
-- Ownership (Pro only — unused in Community)
-- Per-object ownership. Every pipeline/table has an owner.
-- ============================================================

CREATE TABLE ownership (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    object_type   VARCHAR(20) NOT NULL CHECK (object_type IN ('pipeline', 'table', 'namespace')),
    object_id     UUID NOT NULL,
    owner         VARCHAR(255) NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(object_type, object_id)
);

CREATE INDEX idx_ownership_owner ON ownership(owner);

-- ============================================================
-- Shares (Pro only — unused in Community)
-- Per-object access grants. Owner shares with users or roles.
-- ============================================================

CREATE TABLE shares (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    object_type   VARCHAR(20) NOT NULL CHECK (object_type IN ('pipeline', 'table', 'namespace')),
    object_id     UUID NOT NULL,
    shared_with   VARCHAR(255) NOT NULL,               -- username or "role:data-engineer"
    access_level  VARCHAR(10) NOT NULL CHECK (access_level IN ('read', 'write')),
    granted_by    VARCHAR(255) NOT NULL,
    granted_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(object_type, object_id, shared_with)
);

CREATE INDEX idx_shares_shared_with ON shares(shared_with);

-- ============================================================
-- Projects (Pro only — soft grouping)
-- Labels for organizing pipelines/tables. No isolation.
-- ============================================================

CREATE TABLE projects (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          VARCHAR(255) UNIQUE NOT NULL,
    description   TEXT DEFAULT '',
    created_by    VARCHAR(255),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE project_members (
    project_id    UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    object_type   VARCHAR(20) NOT NULL CHECK (object_type IN ('pipeline', 'table')),
    object_id     UUID NOT NULL,
    added_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, object_type, object_id)
);

-- ============================================================
-- Landing Zones
-- Standalone file drop areas for raw data uploads.
-- Independent of pipelines — users upload, preview via DuckDB,
-- then reference in pipeline SQL manually.
-- ============================================================

CREATE TABLE landing_zones (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    namespace   VARCHAR(63) NOT NULL REFERENCES namespaces(name) ON DELETE CASCADE,
    name        VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    owner       VARCHAR(255),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(namespace, name)
);

CREATE INDEX idx_landing_zones_namespace ON landing_zones(namespace);

CREATE TABLE landing_files (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    zone_id       UUID NOT NULL REFERENCES landing_zones(id) ON DELETE CASCADE,
    filename      VARCHAR(1024) NOT NULL,
    s3_path       VARCHAR(1024) NOT NULL,
    size_bytes    BIGINT NOT NULL DEFAULT 0,
    content_type  VARCHAR(128) NOT NULL DEFAULT '',
    uploaded_by   VARCHAR(255),
    uploaded_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_landing_files_zone ON landing_files(zone_id, uploaded_at DESC);

-- ============================================================
-- Plugins
-- Tracks registered plugins and their health status.
-- ============================================================

CREATE TABLE plugins (
    name          VARCHAR(63) PRIMARY KEY,
    slot          VARCHAR(20) NOT NULL,                -- auth, sharing, executor, audit, etc.
    image         VARCHAR(500) NOT NULL,               -- container image reference
    status        VARCHAR(20) NOT NULL DEFAULT 'unknown'
                  CHECK (status IN ('healthy', 'degraded', 'down', 'unknown')),
    config        JSONB DEFAULT '{}',
    last_health   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- Audit Log (Pro only — written by audit plugin)
-- ============================================================

CREATE TABLE audit_log (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor         VARCHAR(255) NOT NULL,               -- username
    action        VARCHAR(100) NOT NULL,               -- "pipeline.create", "run.trigger", etc.
    object_type   VARCHAR(20),
    object_id     UUID,
    details       JSONB,
    timestamp     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_log_actor ON audit_log(actor, timestamp DESC);
CREATE INDEX idx_audit_log_action ON audit_log(action, timestamp DESC);
CREATE INDEX idx_audit_log_time ON audit_log(timestamp DESC);
