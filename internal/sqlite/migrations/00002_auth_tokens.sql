-- +goose Up
CREATE TABLE auth_keys (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    signing_key BLOB NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE refresh_tokens (
    token_hash BLOB PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id TEXT NOT NULL,
    original_login_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    csrf_hash BLOB NOT NULL,
    consumed_at TEXT,
    revoked_at TEXT
);
CREATE INDEX refresh_tokens_family ON refresh_tokens(family_id);
CREATE INDEX refresh_tokens_expiry ON refresh_tokens(expires_at);
DROP TABLE sessions;

-- +goose Down
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
DROP TABLE refresh_tokens;
DROP TABLE auth_keys;
