-- +goose Up
-- Pending Quick Connect sign-ins (live for a few minutes). The new device keeps
-- the secret (only its SHA-256 is stored); a signed-in user approves the code.
CREATE TABLE quick_connect (
    id           uuid PRIMARY KEY,
    secret_hash  bytea NOT NULL,
    code         text NOT NULL,
    device_name  text NOT NULL DEFAULT '',
    client       text NOT NULL DEFAULT '',
    user_id      uuid REFERENCES users (id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL
);
CREATE UNIQUE INDEX quick_connect_secret_idx ON quick_connect (secret_hash);
CREATE UNIQUE INDEX quick_connect_code_idx ON quick_connect (code);

-- +goose Down
DROP TABLE quick_connect;
