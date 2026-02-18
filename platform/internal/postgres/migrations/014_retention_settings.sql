-- Data retention settings and reaper status tracking.
-- platform_settings: key-value JSONB for system config (retention, etc.)
CREATE TABLE IF NOT EXISTS platform_settings (
    key         VARCHAR(63) PRIMARY KEY,
    value       JSONB NOT NULL,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Seed retention defaults (Strategy Doc #22 targets)
INSERT INTO platform_settings (key, value) VALUES ('retention', '{
    "runs_max_per_pipeline": 100,
    "runs_max_age_days": 90,
    "logs_max_age_days": 30,
    "quality_results_max_per_test": 100,
    "soft_delete_purge_days": 30,
    "stuck_run_timeout_minutes": 120,
    "audit_log_max_age_days": 365,
    "nessie_orphan_branch_max_age_hours": 6,
    "reaper_interval_minutes": 60,
    "iceberg_snapshot_max_age_days": 7,
    "iceberg_orphan_file_max_age_days": 3
}'::jsonb) ON CONFLICT DO NOTHING;

-- Per-pipeline retention overrides (null = use system default)
ALTER TABLE pipelines ADD COLUMN IF NOT EXISTS retention_config JSONB;

-- Landing zone lifecycle settings
ALTER TABLE landing_zones ADD COLUMN IF NOT EXISTS processed_max_age_days INT;
ALTER TABLE landing_zones ADD COLUMN IF NOT EXISTS auto_purge BOOLEAN NOT NULL DEFAULT false;

-- Reaper status tracking (singleton row)
CREATE TABLE IF NOT EXISTS reaper_status (
    id               INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    last_run_at      TIMESTAMPTZ,
    runs_pruned      INT NOT NULL DEFAULT 0,
    logs_pruned      INT NOT NULL DEFAULT 0,
    quality_pruned   INT NOT NULL DEFAULT 0,
    pipelines_purged INT NOT NULL DEFAULT 0,
    runs_failed      INT NOT NULL DEFAULT 0,
    branches_cleaned INT NOT NULL DEFAULT 0,
    lz_files_cleaned INT NOT NULL DEFAULT 0,
    audit_pruned     INT NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO reaper_status (id) VALUES (1) ON CONFLICT DO NOTHING;
