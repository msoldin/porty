-- +goose Up
CREATE INDEX deployments_stack_status_idx ON deployments(stack_id, status);

-- +goose Down
DROP INDEX deployments_stack_status_idx;
