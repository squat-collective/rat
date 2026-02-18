-- name: GetTableMetadata :one
SELECT id, namespace, layer, name, description, owner, column_descriptions, created_at, updated_at
FROM table_metadata
WHERE namespace = $1 AND layer = $2 AND name = $3;

-- name: UpsertTableMetadata :one
INSERT INTO table_metadata (namespace, layer, name, description, owner, column_descriptions)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (namespace, layer, name) DO UPDATE
SET description = EXCLUDED.description,
    owner = EXCLUDED.owner,
    column_descriptions = EXCLUDED.column_descriptions,
    updated_at = NOW()
RETURNING id, namespace, layer, name, description, owner, column_descriptions, created_at, updated_at;

-- name: ListTableMetadataByNamespace :many
SELECT id, namespace, layer, name, description, owner, column_descriptions, created_at, updated_at
FROM table_metadata
WHERE namespace = $1;

-- name: ListAllTableMetadata :many
SELECT id, namespace, layer, name, description, owner, column_descriptions, created_at, updated_at
FROM table_metadata;
