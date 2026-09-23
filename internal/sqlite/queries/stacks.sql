-- name: GetStackByID :one
SELECT id, directory_name, compose_project_name, archived_at, created_at, updated_at
FROM stacks
WHERE id = sqlc.arg(id);

-- name: GetStackByDirectory :one
SELECT id, directory_name, compose_project_name, archived_at, created_at, updated_at
FROM stacks
WHERE directory_name = sqlc.arg(directory_name);

-- name: ListActiveStacks :many
SELECT id, directory_name, compose_project_name, archived_at, created_at, updated_at
FROM stacks
WHERE archived_at IS NULL
ORDER BY directory_name;
