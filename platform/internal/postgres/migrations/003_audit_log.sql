-- 003_audit_log.sql
-- Audit log for tracking API actions.

CREATE TABLE IF NOT EXISTS audit_log (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     VARCHAR(255) NOT NULL DEFAULT 'anonymous',
    action      VARCHAR(50) NOT NULL,
    resource    VARCHAR(100) NOT NULL,
    detail      TEXT DEFAULT '',
    ip          VARCHAR(45),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_log_user ON audit_log(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_created ON audit_log(created_at DESC);
