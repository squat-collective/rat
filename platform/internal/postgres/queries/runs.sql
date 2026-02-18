-- name: ListRuns :many
SELECT r.id, r.pipeline_id, r.status, r.trigger, r.started_at, r.finished_at,
       r.duration_ms, r.rows_written, r.error, r.logs_s3_path, r.created_at
FROM runs r
JOIN pipelines p ON r.pipeline_id = p.id
WHERE (sqlc.narg('filter_namespace')::text IS NULL OR p.namespace = sqlc.narg('filter_namespace'))
  AND (sqlc.narg('filter_layer')::text IS NULL OR p.layer = sqlc.narg('filter_layer'))
  AND (sqlc.narg('filter_pipeline')::text IS NULL OR p.name = sqlc.narg('filter_pipeline'))
  AND (sqlc.narg('filter_status')::text IS NULL OR r.status = sqlc.narg('filter_status'))
ORDER BY r.created_at DESC;

-- name: GetRun :one
SELECT id, pipeline_id, status, trigger, started_at, finished_at,
       duration_ms, rows_written, error, logs_s3_path, created_at
FROM runs
WHERE id = $1;

-- name: CreateRun :one
INSERT INTO runs (pipeline_id, status, trigger)
VALUES ($1, $2, $3)
RETURNING id, pipeline_id, status, trigger, started_at, finished_at,
          duration_ms, rows_written, error, logs_s3_path, created_at;

-- name: UpdateRunStatus :exec
UPDATE runs
SET status = @status::varchar(20),
    error = @error,
    started_at = CASE
        WHEN @status::varchar(20) = 'running' AND started_at IS NULL THEN now()
        ELSE started_at
    END,
    finished_at = CASE
        WHEN @status::varchar(20) IN ('success', 'failed', 'cancelled') THEN now()
        ELSE finished_at
    END,
    duration_ms = CASE
        WHEN sqlc.narg('duration_ms')::int IS NOT NULL THEN sqlc.narg('duration_ms')::int
        WHEN @status::varchar(20) IN ('success', 'failed', 'cancelled') AND started_at IS NOT NULL
        THEN EXTRACT(EPOCH FROM (now() - started_at))::int * 1000
        ELSE duration_ms
    END,
    rows_written = CASE
        WHEN sqlc.narg('rows_written')::bigint IS NOT NULL THEN sqlc.narg('rows_written')::bigint
        ELSE rows_written
    END
WHERE id = @id;

-- name: SaveRunLogs :exec
UPDATE runs SET logs = @logs WHERE id = @id;

-- name: GetRunLogsByID :one
SELECT logs FROM runs WHERE id = @id;
