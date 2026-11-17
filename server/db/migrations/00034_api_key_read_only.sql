-- +goose Up
-- Read-only API keys (D-052): a key for a monitoring dashboard can read but never
-- change anything. Existing keys keep full rights.
ALTER TABLE api_keys ADD COLUMN read_only boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE api_keys DROP COLUMN read_only;
