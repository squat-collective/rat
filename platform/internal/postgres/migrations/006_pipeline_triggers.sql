CREATE TABLE IF NOT EXISTS pipeline_triggers (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_id       UUID NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    type              VARCHAR(50) NOT NULL,
    config            JSONB NOT NULL DEFAULT '{}',
    enabled           BOOLEAN NOT NULL DEFAULT true,
    cooldown_seconds  INT NOT NULL DEFAULT 0,
    last_triggered_at TIMESTAMPTZ,
    last_run_id       UUID REFERENCES runs(id),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_pipeline_triggers_pipeline ON pipeline_triggers(pipeline_id);
CREATE INDEX IF NOT EXISTS idx_pipeline_triggers_type ON pipeline_triggers(type) WHERE enabled = true;
CREATE INDEX IF NOT EXISTS idx_pipeline_triggers_config
    ON pipeline_triggers USING gin (config jsonb_path_ops) WHERE enabled = true;
