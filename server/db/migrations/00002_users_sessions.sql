-- +goose Up
CREATE TABLE users (
    id              uuid PRIMARY KEY,
    username        text NOT NULL,
    password_hash   text NOT NULL,
    is_admin        boolean NOT NULL DEFAULT false,
    disabled        boolean NOT NULL DEFAULT false,
    failed_logins   integer NOT NULL DEFAULT 0,
    locked_until    timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    last_login_at   timestamptz
);
-- Usernames are case-insensitive.
CREATE UNIQUE INDEX users_username_lower_idx ON users (lower(username));

-- One row per signed-in device. Only the SHA-256 of the token is stored.
CREATE TABLE sessions (
    id            uuid PRIMARY KEY,
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash    bytea NOT NULL,
    device_name   text NOT NULL DEFAULT '',
    client        text NOT NULL DEFAULT '',
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL
);
CREATE UNIQUE INDEX sessions_token_hash_idx ON sessions (token_hash);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);

-- Security-relevant events (logins, user and session changes).
CREATE TABLE audit_log (
    id         bigserial PRIMARY KEY,
    at         timestamptz NOT NULL DEFAULT now(),
    user_id    uuid,
    action     text NOT NULL,
    detail     jsonb NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX audit_log_at_idx ON audit_log (at DESC);

-- +goose Down
DROP TABLE audit_log;
DROP TABLE sessions;
DROP TABLE users;
