-- +goose Up
CREATE TABLE on_demand_groups (
 id TEXT PRIMARY KEY,
 stack_id TEXT NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
 policy_json TEXT NOT NULL CHECK(json_valid(policy_json) AND length(policy_json)<=4096),
 revision INTEGER NOT NULL CHECK(revision>0),
 phase TEXT NOT NULL CHECK(phase IN ('running','sleeping','starting','stopping','unknown')),
 hold_reason TEXT NOT NULL DEFAULT '' CHECK(length(hold_reason)<=256),
 paused_reason TEXT NOT NULL DEFAULT '' CHECK(length(paused_reason)<=256),
 evidence_json TEXT NOT NULL CHECK(json_valid(evidence_json) AND length(evidence_json)<=65536),
 operation_id TEXT REFERENCES operations(id),
 last_operation_id TEXT NOT NULL DEFAULT '',
 UNIQUE(id,stack_id)
);
CREATE INDEX on_demand_groups_stack ON on_demand_groups(stack_id);
CREATE TABLE on_demand_members (
 group_id TEXT NOT NULL,
 stack_id TEXT NOT NULL,
 service TEXT NOT NULL,
 PRIMARY KEY(stack_id,service),
 FOREIGN KEY(group_id,stack_id) REFERENCES on_demand_groups(id,stack_id) ON DELETE CASCADE
);
-- +goose Down
DROP TABLE on_demand_members;
DROP TABLE on_demand_groups;
