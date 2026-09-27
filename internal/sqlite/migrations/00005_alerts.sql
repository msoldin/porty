-- +goose Up
CREATE TABLE alerts (
 id TEXT PRIMARY KEY,
 stack_id TEXT NOT NULL,
 problem TEXT NOT NULL,
 target TEXT NOT NULL,
 stack_name TEXT NOT NULL,
 revision INTEGER NOT NULL,
 episode INTEGER NOT NULL,
 occurrence_count INTEGER NOT NULL,
 summary TEXT NOT NULL,
 operation_id TEXT NOT NULL DEFAULT '',
 first_at TEXT NOT NULL,
 latest_at TEXT NOT NULL,
 acknowledged_at TEXT,
 acknowledged_by TEXT NOT NULL DEFAULT '',
 resolved_at TEXT,
 resolved_by TEXT NOT NULL DEFAULT '',
 resolution TEXT NOT NULL DEFAULT '',
 can_resolve_manually INTEGER NOT NULL DEFAULT 0,
 UNIQUE(stack_id,problem,target)
);
CREATE INDEX alerts_stack_latest ON alerts(stack_id,latest_at DESC,id DESC);
CREATE INDEX alerts_attention ON alerts(acknowledged_at,resolved_at,latest_at DESC);
CREATE TABLE alert_occurrences (
 alert_id TEXT NOT NULL REFERENCES alerts(id),
 occurrence_id TEXT NOT NULL,
 PRIMARY KEY(alert_id,occurrence_id)
);
CREATE TABLE alert_events (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,
 id TEXT NOT NULL UNIQUE,
 alert_id TEXT NOT NULL REFERENCES alerts(id),
 episode INTEGER NOT NULL,
 kind TEXT NOT NULL,
 actor_id TEXT NOT NULL DEFAULT '',
 occurred_at TEXT NOT NULL,
 note TEXT NOT NULL DEFAULT '',
 operation_id TEXT NOT NULL DEFAULT ''
);
CREATE INDEX alert_events_history ON alert_events(alert_id,sequence DESC);
CREATE TABLE operation_alert_context (
 operation_id TEXT PRIMARY KEY REFERENCES operations(id) ON DELETE CASCADE,
 trigger_kind TEXT NOT NULL,
 stack_name TEXT NOT NULL,
 targets_json TEXT NOT NULL
);

-- +goose Down
DROP TABLE operation_alert_context;
DROP TABLE alert_events;
DROP TABLE alert_occurrences;
DROP TABLE alerts;
