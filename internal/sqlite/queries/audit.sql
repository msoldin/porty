-- name: ListAuditEvents :many
SELECT id, actor_user_id, action, target_type, target_id, outcome, request_id, source_ip, occurred_at
FROM audit_events
ORDER BY occurred_at DESC, rowid DESC
LIMIT sqlc.arg(limit) OFFSET sqlc.arg(offset);
