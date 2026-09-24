-- +goose Up
ALTER TABLE stack_environment ADD COLUMN secret INTEGER NOT NULL DEFAULT 0 CHECK (secret IN (0, 1));

-- +goose Down
ALTER TABLE stack_environment DROP COLUMN secret;
