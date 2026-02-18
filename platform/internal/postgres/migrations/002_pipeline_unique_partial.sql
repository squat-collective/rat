-- 002_pipeline_unique_partial.sql
-- Replace the unconditional UNIQUE(namespace, layer, name) with a partial
-- unique index that only applies to non-deleted rows, allowing re-creation
-- of soft-deleted pipelines with the same name.

ALTER TABLE pipelines DROP CONSTRAINT IF EXISTS pipelines_namespace_layer_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS idx_pipelines_unique_active
    ON pipelines(namespace, layer, name)
    WHERE deleted_at IS NULL;
