-- +goose Up
CREATE TABLE auto_update_policies (
 stack_id TEXT PRIMARY KEY REFERENCES stacks(id) ON DELETE CASCADE,
 enabled INTEGER NOT NULL DEFAULT 0, expression TEXT NOT NULL,
 revision INTEGER NOT NULL, next_run_at INTEGER NOT NULL,
 paused_reason TEXT NOT NULL DEFAULT ''
);
CREATE TABLE auto_update_runs (
 id TEXT PRIMARY KEY, stack_id TEXT NOT NULL, stack_name TEXT NOT NULL,
 policy_revision INTEGER NOT NULL, scheduled_at INTEGER NOT NULL,
 phase TEXT NOT NULL, outcome TEXT NOT NULL DEFAULT '', reason TEXT NOT NULL DEFAULT '',
 operation_id TEXT NOT NULL DEFAULT '',
 UNIQUE(stack_id,scheduled_at)
);
CREATE INDEX auto_update_runs_pending ON auto_update_runs(phase,scheduled_at);
CREATE INDEX auto_update_runs_stack ON auto_update_runs(stack_id,scheduled_at DESC);
-- +goose Down
DROP TABLE auto_update_runs;
DROP TABLE auto_update_policies;
