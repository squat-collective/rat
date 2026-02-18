-- Add logs column to runs table for persisting pipeline logs on completion.
ALTER TABLE runs ADD COLUMN IF NOT EXISTS logs JSONB;
