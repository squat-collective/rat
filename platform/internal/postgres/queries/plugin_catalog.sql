-- name: ListPlugins :many
SELECT id, name, kind, version, status, error, descriptor, config, config_version,
       addr, healthy, registered_at, enabled_at, updated_at
FROM plugin_catalog
WHERE (sqlc.narg('filter_status')::text IS NULL OR status = sqlc.narg('filter_status'))
  AND (sqlc.narg('filter_kind')::text IS NULL OR kind = sqlc.narg('filter_kind'))
ORDER BY registered_at DESC;

-- name: GetPluginByName :one
SELECT id, name, kind, version, status, error, descriptor, config, config_version,
       addr, healthy, registered_at, enabled_at, updated_at
FROM plugin_catalog
WHERE name = $1;

-- name: UpsertPlugin :one
-- Registers or re-registers a plugin. Re-registration (the common case
-- on container restart) must NOT clobber the persisted config — that
-- field is owned by UpdatePluginConfig and tracks plugin-managed state
-- (e.g. rat-plugin-secrets stores its encrypted secret list there).
-- Pass config = NULL from the Go side on re-register; COALESCE keeps
-- whatever the plugin previously persisted. config_version is preserved
-- across re-registrations (only UpdatePluginConfig bumps it).
INSERT INTO plugin_catalog (name, kind, version, status, error, descriptor, config, addr, healthy)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (name) DO UPDATE
SET kind = EXCLUDED.kind,
    version = EXCLUDED.version,
    status = EXCLUDED.status,
    error = EXCLUDED.error,
    descriptor = EXCLUDED.descriptor,
    config = COALESCE(EXCLUDED.config, plugin_catalog.config),
    addr = EXCLUDED.addr,
    healthy = EXCLUDED.healthy,
    updated_at = now()
RETURNING id, name, kind, version, status, error, descriptor, config, config_version,
          addr, healthy, registered_at, enabled_at, updated_at;

-- name: UpdatePluginStatus :exec
UPDATE plugin_catalog
SET status = $2,
    error = $3,
    enabled_at = CASE WHEN $2 = 'enabled' THEN now() ELSE enabled_at END,
    updated_at = now()
WHERE name = $1;

-- name: UpdatePluginConfig :one
-- Optimistic concurrency: when expected_version is non-NULL, the UPDATE
-- only fires if config_version matches; the CTE then SELECTs the row in
-- the same statement so the caller can distinguish three outcomes in one
-- round-trip:
--   * row returned, was_updated = true   → success, new config_version is N+1
--   * row returned, was_updated = false  → version mismatch, current config_version is returned
--   * no row returned                    → plugin not found
WITH updated AS (
    UPDATE plugin_catalog AS pc
    SET config = $2,
        config_version = pc.config_version + 1,
        updated_at = now()
    WHERE pc.name = $1
      AND (sqlc.narg('expected_version')::bigint IS NULL
           OR pc.config_version = sqlc.narg('expected_version'))
    RETURNING pc.id, pc.name, pc.kind, pc.version, pc.status, pc.error, pc.descriptor, pc.config, pc.config_version,
              pc.addr, pc.healthy, pc.registered_at, pc.enabled_at, pc.updated_at
)
SELECT u.id, u.name, u.kind, u.version, u.status, u.error, u.descriptor, u.config, u.config_version,
       u.addr, u.healthy, u.registered_at, u.enabled_at, u.updated_at,
       true AS was_updated
FROM updated u
UNION ALL
SELECT p.id, p.name, p.kind, p.version, p.status, p.error, p.descriptor, p.config, p.config_version,
       p.addr, p.healthy, p.registered_at, p.enabled_at, p.updated_at,
       false AS was_updated
FROM plugin_catalog p
WHERE p.name = $1
  AND NOT EXISTS (SELECT 1 FROM updated);

-- name: UpdatePluginHealth :exec
UPDATE plugin_catalog
SET healthy = $2,
    error = $3,
    updated_at = now()
WHERE name = $1;

-- name: DeletePlugin :exec
DELETE FROM plugin_catalog
WHERE name = $1;

-- ── Plugin Sources ─────────────────────────────────────────────

-- name: ListPluginSources :many
SELECT id, type, url, trusted, enabled, created_at
FROM plugin_sources
ORDER BY created_at DESC;

-- name: CreatePluginSource :one
INSERT INTO plugin_sources (type, url, trusted, enabled)
VALUES ($1, $2, $3, $4)
RETURNING id, type, url, trusted, enabled, created_at;

-- name: DeletePluginSource :exec
DELETE FROM plugin_sources
WHERE id = $1;

-- ── Plugin Policies ────────────────────────────────────────────

-- name: ListPluginPolicies :many
SELECT id, rule, pattern, kind, created_at
FROM plugin_policies
ORDER BY created_at;

-- name: CreatePluginPolicy :one
INSERT INTO plugin_policies (rule, pattern, kind)
VALUES ($1, $2, $3)
RETURNING id, rule, pattern, kind, created_at;

-- name: DeletePluginPolicy :exec
DELETE FROM plugin_policies
WHERE id = $1;
