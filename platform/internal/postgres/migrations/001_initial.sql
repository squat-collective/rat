-- 001_initial.sql
-- RAT v2 — initial schema
-- Adapted from docs/postgres-schema.sql with IF NOT EXISTS for idempotency.

-- ── Namespaces ──────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS namespaces (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(63) UNIQUE NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_by  VARCHAR(255)
);

INSERT INTO namespaces (name) VALUES ('default') ON CONFLICT DO NOTHING;

-- ── Pipelines ───────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS pipelines (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    namespace   VARCHAR(63) NOT NULL REFERENCES namespaces(name) ON DELETE CASCADE,
    layer       VARCHAR(10) NOT NULL CHECK (layer IN ('bronze', 'silver', 'gold')),
    name        VARCHAR(255) NOT NULL,
    type        VARCHAR(10) NOT NULL DEFAULT 'sql' CHECK (type IN ('sql', 'python')),
    s3_path     VARCHAR(1024) NOT NULL,
    description TEXT DEFAULT '',
    owner       VARCHAR(255),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ,
    UNIQUE(namespace, layer, name)
);

CREATE INDEX IF NOT EXISTS idx_pipelines_namespace ON pipelines(namespace);
CREATE INDEX IF NOT EXISTS idx_pipelines_layer ON pipelines(namespace, layer);
CREATE INDEX IF NOT EXISTS idx_pipelines_owner ON pipelines(owner) WHERE owner IS NOT NULL;

-- ── Runs ────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS runs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id   UUID NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    status        VARCHAR(20) NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending', 'running', 'success', 'failed', 'cancelled')),
    trigger       VARCHAR(100) NOT NULL DEFAULT 'manual',
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ,
    duration_ms   INT,
    rows_written  BIGINT,
    error         TEXT,
    logs_s3_path  VARCHAR(1024),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_runs_pipeline ON runs(pipeline_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(status) WHERE status IN ('pending', 'running');

-- ── Schedules ───────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS schedules (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id   UUID NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    cron_expr     VARCHAR(100) NOT NULL,
    enabled       BOOLEAN NOT NULL DEFAULT true,
    last_run_id   UUID REFERENCES runs(id),
    last_run_at   TIMESTAMPTZ,
    next_run_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_schedules_next_run ON schedules(next_run_at) WHERE enabled = true;

-- ── Quality Tests ───────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS quality_tests (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id   UUID NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    name          VARCHAR(255) NOT NULL,
    description   TEXT DEFAULT '',
    severity      VARCHAR(10) NOT NULL DEFAULT 'error' CHECK (severity IN ('error', 'warn')),
    s3_path       VARCHAR(1024) NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(pipeline_id, name)
);

-- ── Quality Results ─────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS quality_results (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    test_id       UUID NOT NULL REFERENCES quality_tests(id) ON DELETE CASCADE,
    run_id        UUID REFERENCES runs(id),
    status        VARCHAR(10) NOT NULL CHECK (status IN ('passed', 'failed', 'warned', 'error')),
    value         NUMERIC,
    expected      NUMERIC,
    duration_ms   INT,
    ran_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_quality_results_test ON quality_results(test_id, ran_at DESC);

-- ── Plugins ─────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS plugins (
    name          VARCHAR(63) PRIMARY KEY,
    slot          VARCHAR(20) NOT NULL,
    image         VARCHAR(500) NOT NULL,
    status        VARCHAR(20) NOT NULL DEFAULT 'unknown'
                  CHECK (status IN ('healthy', 'degraded', 'down', 'unknown')),
    config        JSONB DEFAULT '{}',
    last_health   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
