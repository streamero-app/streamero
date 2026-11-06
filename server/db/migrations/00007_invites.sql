-- +goose Up
-- Invite links: whoever has the link may create an ordinary (non-admin) account
-- with preset permissions, until it expires or runs out of uses.
CREATE TABLE invites (
    id           uuid PRIMARY KEY,
    token_hash   bytea NOT NULL,
    note         text NOT NULL DEFAULT '',
    created_by   uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    max_uses     integer NOT NULL,
    uses         integer NOT NULL DEFAULT 0,
    permissions  jsonb NOT NULL DEFAULT '{}'::jsonb
);
CREATE UNIQUE INDEX invites_token_hash_idx ON invites (token_hash);

-- +goose Down
DROP TABLE invites;
