-- name: ListPipelineTriggers :many
SELECT id, pipeline_id, type, config, enabled, cooldown_seconds,
       last_triggered_at, last_run_id, created_at, updated_at
FROM pipeline_triggers
WHERE pipeline_id = $1
ORDER BY created_at DESC;

-- name: GetPipelineTrigger :one
SELECT id, pipeline_id, type, config, enabled, cooldown_seconds,
       last_triggered_at, last_run_id, created_at, updated_at
FROM pipeline_triggers
WHERE id = $1;

-- name: CreatePipelineTrigger :one
INSERT INTO pipeline_triggers (pipeline_id, type, config, enabled, cooldown_seconds)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, pipeline_id, type, config, enabled, cooldown_seconds,
          last_triggered_at, last_run_id, created_at, updated_at;

-- name: UpdatePipelineTrigger :one
UPDATE pipeline_triggers
SET config = COALESCE(sqlc.narg('config'), config),
    enabled = COALESCE(sqlc.narg('enabled'), enabled),
    cooldown_seconds = COALESCE(sqlc.narg('cooldown_seconds'), cooldown_seconds),
    updated_at = now()
WHERE id = $1
RETURNING id, pipeline_id, type, config, enabled, cooldown_seconds,
          last_triggered_at, last_run_id, created_at, updated_at;

-- name: DeletePipelineTrigger :exec
DELETE FROM pipeline_triggers
WHERE id = $1;

-- name: FindTriggersByLandingZone :many
SELECT id, pipeline_id, type, config, enabled, cooldown_seconds,
       last_triggered_at, last_run_id, created_at, updated_at
FROM pipeline_triggers
WHERE type = 'landing_zone_upload'
  AND enabled = true
  AND config @> $1::jsonb;

-- name: UpdateTriggerFired :exec
UPDATE pipeline_triggers
SET last_triggered_at = now(),
    last_run_id = $2,
    updated_at = now()
WHERE id = $1;

-- name: UpdateTriggerFiredCAS :one
-- Compare-and-swap fire of a trigger. Only updates when the current
-- last_triggered_at matches the expected value (or both are NULL).
-- Returns the updated row on success; pgx.ErrNoRows when another
-- evaluation path already fired the trigger (the race-loser silently
-- skips submission). IS NOT DISTINCT FROM handles the NULL == NULL case
-- correctly so the very first fire (expected=NULL) succeeds.
UPDATE pipeline_triggers
SET last_triggered_at = sqlc.arg('new_triggered_at')::timestamptz,
    last_run_id = $2,
    updated_at = now()
WHERE id = $1
  AND last_triggered_at IS NOT DISTINCT FROM sqlc.narg('expected_last_triggered_at')::timestamptz
RETURNING id, pipeline_id, type, config, enabled, cooldown_seconds,
          last_triggered_at, last_run_id, created_at, updated_at;

-- name: FindTriggersByType :many
SELECT id, pipeline_id, type, config, enabled, cooldown_seconds,
       last_triggered_at, last_run_id, created_at, updated_at
FROM pipeline_triggers
WHERE type = $1 AND enabled = true;

-- name: FindTriggerByWebhookToken :one
SELECT id, pipeline_id, type, config, enabled, cooldown_seconds,
       last_triggered_at, last_run_id, created_at, updated_at
FROM pipeline_triggers
WHERE type = 'webhook' AND enabled = true
  AND config->>'token_hash' = sqlc.arg('token')::text;

-- name: FindTriggersByPipelineSuccess :many
SELECT id, pipeline_id, type, config, enabled, cooldown_seconds,
       last_triggered_at, last_run_id, created_at, updated_at
FROM pipeline_triggers
WHERE type = 'pipeline_success' AND enabled = true
  AND config->>'namespace' = sqlc.arg('namespace')::text
  AND config->>'layer' = sqlc.arg('layer')::text
  AND config->>'pipeline' = sqlc.arg('pipeline')::text;

-- name: FindTriggersByFilePattern :many
SELECT id, pipeline_id, type, config, enabled, cooldown_seconds,
       last_triggered_at, last_run_id, created_at, updated_at
FROM pipeline_triggers
WHERE type = 'file_pattern' AND enabled = true
  AND config->>'namespace' = sqlc.arg('namespace')::text
  AND config->>'zone_name' = sqlc.arg('zone_name')::text;
