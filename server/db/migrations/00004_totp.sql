-- +goose Up
-- TOTP two-factor authentication. A row with enabled = false is a setup in progress.
CREATE TABLE user_totp (
    user_id     uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    secret      bytea NOT NULL,
    enabled     boolean NOT NULL DEFAULT false,
    last_step   bigint NOT NULL DEFAULT 0, -- last accepted time step (replay protection)
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- One-time recovery codes (SHA-256 only).
CREATE TABLE recovery_codes (
    id         bigserial PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code_hash  bytea NOT NULL,
    used_at    timestamptz
);
CREATE INDEX recovery_codes_user_idx ON recovery_codes (user_id);

-- +goose Down
DROP TABLE recovery_codes;
DROP TABLE user_totp;
