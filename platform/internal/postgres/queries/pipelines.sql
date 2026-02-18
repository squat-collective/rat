-- name: ListPipelines :many
SELECT id, namespace, layer, name, type, s3_path, description, owner,
       published_at, published_versions, draft_dirty,
       created_at, updated_at
FROM pipelines
WHERE deleted_at IS NULL
  AND (sqlc.narg('filter_namespace')::text IS NULL OR namespace = sqlc.narg('filter_namespace'))
  AND (sqlc.narg('filter_layer')::text IS NULL OR layer = sqlc.narg('filter_layer'))
ORDER BY created_at DESC;

-- name: GetPipeline :one
SELECT id, namespace, layer, name, type, s3_path, description, owner,
       published_at, published_versions, draft_dirty,
       created_at, updated_at
FROM pipelines
WHERE namespace = $1
  AND layer = $2
  AND name = $3
  AND deleted_at IS NULL;

-- name: CreatePipeline :one
INSERT INTO pipelines (namespace, layer, name, type, s3_path, description, owner)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, namespace, layer, name, type, s3_path, description, owner,
          published_at, published_versions, draft_dirty, created_at, updated_at;

-- name: UpdatePipeline :one
UPDATE pipelines
SET description = COALESCE(sqlc.narg('description'), description),
    type = COALESCE(sqlc.narg('type'), type),
    owner = COALESCE(sqlc.narg('owner'), owner),
    updated_at = now()
WHERE namespace = $1
  AND layer = $2
  AND name = $3
  AND deleted_at IS NULL
RETURNING id, namespace, layer, name, type, s3_path, description, owner,
          published_at, published_versions, draft_dirty, created_at, updated_at;

-- name: GetPipelineByID :one
SELECT id, namespace, layer, name, type, s3_path, description, owner,
       published_at, published_versions, draft_dirty,
       created_at, updated_at
FROM pipelines
WHERE id = $1
  AND deleted_at IS NULL;

-- name: SoftDeletePipeline :exec
UPDATE pipelines
SET deleted_at = now()
WHERE namespace = $1
  AND layer = $2
  AND name = $3
  AND deleted_at IS NULL;

-- name: SetDraftDirty :exec
UPDATE pipelines
SET draft_dirty = $4,
    updated_at = now()
WHERE namespace = $1
  AND layer = $2
  AND name = $3
  AND deleted_at IS NULL;

-- name: PublishPipeline :one
UPDATE pipelines
SET published_at = now(),
    published_versions = $4,
    draft_dirty = false,
    updated_at = now()
WHERE namespace = $1
  AND layer = $2
  AND name = $3
  AND deleted_at IS NULL
RETURNING id, namespace, layer, name, type, s3_path, description, owner,
          published_at, published_versions, draft_dirty, created_at, updated_at;
