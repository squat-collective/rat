-- name: ListNamespaces :many
SELECT name, description, created_by, created_at
FROM namespaces
ORDER BY created_at;

-- name: CreateNamespace :exec
INSERT INTO namespaces (name, created_by)
VALUES ($1, $2);

-- name: DeleteNamespace :exec
DELETE FROM namespaces
WHERE name = $1;

-- name: UpdateNamespace :exec
UPDATE namespaces SET description = $2 WHERE name = $1;
