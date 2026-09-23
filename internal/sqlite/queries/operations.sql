-- name: GetOperation :one
SELECT id, kind, scope_type, scope_id, request_key, status, started_at, completed_at, exit_code, error_code, output_tail, output_truncated, initiated_by
FROM operations
WHERE id = sqlc.arg(id);

-- name: ListOperations :many
SELECT id, kind, scope_type, scope_id, request_key, status, started_at, completed_at, exit_code, error_code, output_tail, output_truncated, initiated_by
FROM operations
ORDER BY COALESCE(started_at, '') DESC, rowid DESC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);
