-- Feature flags stored in platform_settings as JSONB.
-- Enables runtime toggling of platform behaviors without restarts.
--
-- Default flags: all features enabled for community edition.
-- Pro features (quotas, rate_limiting) default to false.
INSERT INTO platform_settings (key, value) VALUES ('feature_flags', '{
    "pipeline_preview": true,
    "quality_tests": true,
    "landing_zones": true,
    "pipeline_triggers": true,
    "pipeline_versions": true,
    "query_engine": true,
    "audit_log": true,
    "namespace_quotas": false,
    "distributed_rate_limiting": false
}'::jsonb) ON CONFLICT DO NOTHING;
