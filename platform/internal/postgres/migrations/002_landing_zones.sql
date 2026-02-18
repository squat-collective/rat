-- 002_landing_zones.sql
-- Landing zones: standalone file drop areas for raw data uploads.

CREATE TABLE IF NOT EXISTS landing_zones (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    namespace   VARCHAR(63) NOT NULL REFERENCES namespaces(name) ON DELETE CASCADE,
    name        VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    owner       VARCHAR(255),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(namespace, name)
);

CREATE INDEX IF NOT EXISTS idx_landing_zones_namespace ON landing_zones(namespace);

CREATE TABLE IF NOT EXISTS landing_files (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    zone_id       UUID NOT NULL REFERENCES landing_zones(id) ON DELETE CASCADE,
    filename      VARCHAR(1024) NOT NULL,
    s3_path       VARCHAR(1024) NOT NULL,
    size_bytes    BIGINT NOT NULL DEFAULT 0,
    content_type  VARCHAR(128) NOT NULL DEFAULT '',
    uploaded_by   VARCHAR(255),
    uploaded_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_landing_files_zone ON landing_files(zone_id, uploaded_at DESC);
