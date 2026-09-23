-- +goose Up
CREATE TABLE app_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    setup_state TEXT NOT NULL DEFAULT 'unregistered',
    repository_root TEXT,
    remote_name TEXT,
    remote_url_redacted TEXT,
    tracking_branch TEXT,
    git_author_name TEXT,
    git_author_email TEXT,
    last_fetch_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE repository_auth (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    auth_type TEXT NOT NULL,
    https_username TEXT,
    https_secret BLOB,
    ssh_key_path TEXT,
    known_hosts_path TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    password_changed_at TEXT NOT NULL
);

CREATE TABLE sessions (
    token_hash BLOB PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    csrf_hash BLOB NOT NULL,
    created_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    idle_expires_at TEXT NOT NULL,
    absolute_expires_at TEXT NOT NULL,
    revoked_at TEXT
);

CREATE TABLE stacks (
    id TEXT PRIMARY KEY,
    directory_name TEXT NOT NULL UNIQUE,
    compose_project_name TEXT NOT NULL UNIQUE,
    archived_at TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE stack_environment (
    stack_id TEXT NOT NULL REFERENCES stacks(id) ON DELETE CASCADE,
    key TEXT NOT NULL,
    value BLOB NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (stack_id, key)
);

CREATE TABLE operations (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    scope_type TEXT NOT NULL,
    scope_id TEXT,
    request_key TEXT UNIQUE,
    status TEXT NOT NULL,
    started_at TEXT,
    completed_at TEXT,
    exit_code INTEGER,
    error_code TEXT,
    output_tail BLOB,
    output_truncated INTEGER NOT NULL DEFAULT 0,
    initiated_by TEXT REFERENCES users(id)
);

CREATE TABLE deployments (
    id TEXT PRIMARY KEY,
    stack_id TEXT NOT NULL REFERENCES stacks(id),
    operation_id TEXT NOT NULL UNIQUE REFERENCES operations(id),
    git_commit TEXT,
    dirty INTEGER NOT NULL,
    diff_digest TEXT,
    compose_digest TEXT,
    status TEXT NOT NULL,
    started_at TEXT NOT NULL,
    completed_at TEXT,
    duration_ms INTEGER,
    error_code TEXT
);

CREATE TABLE audit_events (
    id TEXT PRIMARY KEY,
    actor_user_id TEXT REFERENCES users(id),
    action TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT,
    outcome TEXT NOT NULL,
    request_id TEXT NOT NULL,
    source_ip TEXT,
    occurred_at TEXT NOT NULL
);

INSERT INTO app_state(id, created_at, updated_at)
VALUES (1, strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'));

-- +goose Down
DROP TABLE audit_events;
DROP TABLE deployments;
DROP TABLE operations;
DROP TABLE stack_environment;
DROP TABLE stacks;
DROP TABLE sessions;
DROP TABLE users;
DROP TABLE repository_auth;
DROP TABLE app_state;
