-- 017_plugin_config_version.sql
-- Add config_version for optimistic concurrency on PUT /config.
-- Bumped on every successful UpdatePluginConfig; clients pass it
-- back via If-Match to prevent lost updates.
ALTER TABLE plugin_catalog
    ADD COLUMN IF NOT EXISTS config_version BIGINT NOT NULL DEFAULT 1;
