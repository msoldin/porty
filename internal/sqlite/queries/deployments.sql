-- name: GetLatestDeployment :one
SELECT id, stack_id, operation_id, git_commit, dirty, diff_digest, compose_digest, status, started_at, completed_at, duration_ms, error_code
FROM deployments
WHERE stack_id = sqlc.arg(stack_id)
ORDER BY started_at DESC
LIMIT 1;

-- name: HasSuccessfulDeployment :one
SELECT EXISTS(
    SELECT 1 FROM deployments
    WHERE stack_id = sqlc.arg(stack_id) AND status = 'succeeded'
) AS has_deployed;

-- name: ListDeployments :many
SELECT id, stack_id, operation_id, git_commit, dirty, diff_digest, compose_digest, status, started_at, completed_at, duration_ms, error_code
FROM deployments
WHERE stack_id = sqlc.arg(stack_id)
ORDER BY started_at DESC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);
