-- Pipeline version history: stores a snapshot of published_versions for each publish.
-- Rollback creates a new version that re-pins the old snapshot (git-revert style).

CREATE TABLE pipeline_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id UUID NOT NULL REFERENCES pipelines(id),
    version_number INTEGER NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    published_versions JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (pipeline_id, version_number)
);

CREATE INDEX idx_pipeline_versions_pipeline ON pipeline_versions (pipeline_id, version_number DESC);

-- Per-pipeline retention limit (default 50).
ALTER TABLE pipelines ADD COLUMN IF NOT EXISTS max_versions INTEGER NOT NULL DEFAULT 50;
