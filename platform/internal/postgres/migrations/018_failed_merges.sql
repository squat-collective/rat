-- 018_failed_merges.sql
-- Audit table for Phase 5 (branch merge → main) terminal failures.
--
-- When the runner cannot merge an ephemeral branch into main and has
-- exhausted its retry budget, it leaves the branch in place (so a human
-- can recover the data) and inserts a row here describing what happened.
-- The reaper consults this table to avoid auto-reaping branches that are
-- pending operator attention.
CREATE TABLE IF NOT EXISTS failed_merges (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID NOT NULL,
    branch_name     TEXT NOT NULL,
    source_hash     TEXT,
    target_hash     TEXT,
    error_kind      TEXT NOT NULL,
    error_message   TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_failed_merges_run_id ON failed_merges (run_id);
CREATE INDEX IF NOT EXISTS idx_failed_merges_created_at ON failed_merges (created_at DESC);
