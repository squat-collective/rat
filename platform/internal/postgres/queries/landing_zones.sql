-- name: ListLandingZones :many
SELECT lz.id, lz.namespace, lz.name, lz.description, lz.owner, lz.expected_schema,
       lz.created_at, lz.updated_at,
       COALESCE(COUNT(lf.id), 0)::bigint AS file_count,
       COALESCE(SUM(lf.size_bytes), 0)::bigint AS total_bytes
FROM landing_zones lz
LEFT JOIN landing_files lf ON lf.zone_id = lz.id
WHERE (sqlc.narg('filter_namespace')::text IS NULL OR lz.namespace = sqlc.narg('filter_namespace'))
GROUP BY lz.id
ORDER BY lz.created_at DESC;

-- name: GetLandingZone :one
SELECT lz.id, lz.namespace, lz.name, lz.description, lz.owner, lz.expected_schema,
       lz.created_at, lz.updated_at,
       COALESCE(COUNT(lf.id), 0)::bigint AS file_count,
       COALESCE(SUM(lf.size_bytes), 0)::bigint AS total_bytes
FROM landing_zones lz
LEFT JOIN landing_files lf ON lf.zone_id = lz.id
WHERE lz.namespace = $1 AND lz.name = $2
GROUP BY lz.id;

-- name: GetLandingZoneByID :one
SELECT id, namespace, name, description, owner, expected_schema, created_at, updated_at
FROM landing_zones
WHERE id = $1;

-- name: CreateLandingZone :one
INSERT INTO landing_zones (namespace, name, description, owner)
VALUES ($1, $2, $3, $4)
RETURNING id, namespace, name, description, owner, expected_schema, created_at, updated_at;

-- name: DeleteLandingZone :exec
DELETE FROM landing_zones
WHERE namespace = $1 AND name = $2;

-- name: UpdateLandingZone :one
UPDATE landing_zones
SET description = COALESCE(sqlc.narg('description'), description),
    owner = COALESCE(sqlc.narg('owner'), owner),
    expected_schema = COALESCE(sqlc.narg('expected_schema'), expected_schema),
    updated_at = NOW()
WHERE namespace = $1 AND name = $2
RETURNING id, namespace, name, description, owner, expected_schema, created_at, updated_at;

-- name: ListLandingFiles :many
SELECT id, zone_id, filename, s3_path, size_bytes, content_type, uploaded_by, uploaded_at
FROM landing_files
WHERE zone_id = $1
ORDER BY uploaded_at DESC;

-- name: CreateLandingFile :one
INSERT INTO landing_files (zone_id, filename, s3_path, size_bytes, content_type, uploaded_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id, zone_id, filename, s3_path, size_bytes, content_type, uploaded_by, uploaded_at;

-- name: GetLandingFile :one
SELECT id, zone_id, filename, s3_path, size_bytes, content_type, uploaded_by, uploaded_at
FROM landing_files
WHERE id = $1;

-- name: DeleteLandingFile :exec
DELETE FROM landing_files
WHERE id = $1;
