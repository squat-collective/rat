-- Add phase_profiles column to runs table for storing per-phase timing data on completion.
ALTER TABLE runs ADD COLUMN IF NOT EXISTS phase_profiles JSONB;
