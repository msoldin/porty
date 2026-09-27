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
CREATE TABLE update_executions (
 run_id TEXT PRIMARY KEY REFERENCES auto_update_runs(id),
 operation_id TEXT NOT NULL UNIQUE REFERENCES operations(id),
 phase TEXT NOT NULL, source_digest TEXT NOT NULL,
 changes_json TEXT NOT NULL, baseline_json TEXT NOT NULL,
 results_json TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE deployment_images (
 stack_id TEXT NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
 service TEXT NOT NULL, source_reference TEXT NOT NULL, target_reference TEXT NOT NULL,
 platform TEXT NOT NULL, image_id TEXT NOT NULL,
 PRIMARY KEY(stack_id,service)
);
-- +goose Down
DROP TABLE deployment_images;
DROP TABLE update_executions;
DROP TABLE auto_update_runs;
DROP TABLE auto_update_policies;
