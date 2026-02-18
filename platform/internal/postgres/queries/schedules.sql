-- name: ListSchedules :many
SELECT id, pipeline_id, cron_expr, enabled, last_run_id, last_run_at,
       next_run_at, created_at, updated_at
FROM schedules
ORDER BY created_at DESC;

-- name: GetSchedule :one
SELECT id, pipeline_id, cron_expr, enabled, last_run_id, last_run_at,
       next_run_at, created_at, updated_at
FROM schedules
WHERE id = $1;

-- name: CreateSchedule :one
INSERT INTO schedules (pipeline_id, cron_expr, enabled)
VALUES ($1, $2, $3)
RETURNING id, pipeline_id, cron_expr, enabled, last_run_id, last_run_at,
          next_run_at, created_at, updated_at;

-- name: UpdateSchedule :one
UPDATE schedules
SET cron_expr = COALESCE(sqlc.narg('cron_expr'), cron_expr),
    enabled = COALESCE(sqlc.narg('enabled'), enabled),
    updated_at = now()
WHERE id = $1
RETURNING id, pipeline_id, cron_expr, enabled, last_run_id, last_run_at,
          next_run_at, created_at, updated_at;

-- name: UpdateScheduleRun :exec
UPDATE schedules
SET last_run_id = $2,
    last_run_at = $3,
    next_run_at = $4,
    updated_at = now()
WHERE id = $1;

-- name: DeleteSchedule :exec
DELETE FROM schedules
WHERE id = $1;
