-- 016_plugin_catalog.sql
-- RAT v2 — Open plugin catalog, replacing the slot-based plugins table from 001.
--
-- The old plugins table (001_initial.sql) used a fixed slot model:
--   name PK, slot, image, status, config, last_health, created_at
-- It is empty in all community deployments and incompatible with the new
-- dynamic registry. Drop it and create the open catalog tables.

-- ── Drop old slot-based plugins table ──────────────────────────
DROP TABLE IF EXISTS plugins;

-- ── Plugin Catalog ─────────────────────────────────────────────
-- Core registry for all plugins that have phoned-home or been registered
-- via config. Each plugin has a unique name, a kind (platform/runner/portal),
-- and a lifecycle status (registered → enabled → disabled / error).
CREATE TABLE IF NOT EXISTS plugin_catalog (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT UNIQUE NOT NULL,
    kind            TEXT NOT NULL CHECK (kind IN ('platform', 'runner', 'portal')),
    version         TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'registered'
                    CHECK (status IN ('registered', 'enabled', 'disabled', 'error')),
    error           TEXT DEFAULT '',
    descriptor      JSONB DEFAULT '{}',
    config          JSONB DEFAULT '{}',
    addr            TEXT NOT NULL,
    healthy         BOOLEAN NOT NULL DEFAULT true,
    registered_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    enabled_at      TIMESTAMPTZ,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_plugin_catalog_status ON plugin_catalog(status);
CREATE INDEX IF NOT EXISTS idx_plugin_catalog_kind ON plugin_catalog(kind);
CREATE INDEX IF NOT EXISTS idx_plugin_catalog_healthy ON plugin_catalog(healthy) WHERE healthy = false;

-- ── Plugin Sources ─────────────────────────────────────────────
-- Repositories from which plugins can be discovered (OCI registries,
-- local directories, git repos). Phase 4 — management UI uses these.
CREATE TABLE IF NOT EXISTS plugin_sources (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type        TEXT NOT NULL CHECK (type IN ('oci', 'local', 'git')),
    url         TEXT NOT NULL,
    trusted     BOOLEAN NOT NULL DEFAULT false,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ── Plugin Policies ────────────────────────────────────────────
-- Allow/deny rules governing which plugins may register. Evaluated
-- in order (first match wins). Empty table = allow all.
CREATE TABLE IF NOT EXISTS plugin_policies (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rule        TEXT NOT NULL CHECK (rule IN ('allow', 'deny')),
    pattern     TEXT NOT NULL,
    kind        TEXT,  -- NULL = applies to all kinds
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
