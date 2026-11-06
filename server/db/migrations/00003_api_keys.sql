-- +goose Up
-- Keys for integrations (Seerr/Jellyseerr, *arr webhooks, scripts). Only the
-- SHA-256 of the secret is stored; the key acts as the admin who created it.
CREATE TABLE api_keys (
    id            uuid PRIMARY KEY,
    name          text NOT NULL,
    token_hash    bytea NOT NULL,
    prefix        text NOT NULL,
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_used_at  timestamptz
);
CREATE UNIQUE INDEX api_keys_token_hash_idx ON api_keys (token_hash);

-- +goose Down
DROP TABLE api_keys;
