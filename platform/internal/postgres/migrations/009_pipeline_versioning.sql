-- Add draft/published versioning columns to pipelines table.
-- published_versions maps file path → S3 version ID (pinned snapshot).
-- draft_dirty is true when the HEAD differs from published versions.
ALTER TABLE pipelines ADD COLUMN IF NOT EXISTS published_at TIMESTAMPTZ;
ALTER TABLE pipelines ADD COLUMN IF NOT EXISTS published_versions JSONB NOT NULL DEFAULT '{}';
ALTER TABLE pipelines ADD COLUMN IF NOT EXISTS draft_dirty BOOLEAN NOT NULL DEFAULT false;
