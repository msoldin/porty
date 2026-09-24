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

-- name: ListLatestDeploymentTimes :many
SELECT s.id AS stack_id, d.started_at
FROM stacks AS s
JOIN deployments AS d ON d.id = (
    SELECT latest.id
    FROM deployments AS latest
    WHERE latest.stack_id = s.id
    ORDER BY latest.started_at DESC, latest.id DESC
    LIMIT 1
)
WHERE s.archived_at IS NULL;
