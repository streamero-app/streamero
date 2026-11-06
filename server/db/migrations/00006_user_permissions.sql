-- +goose Up
-- Per-user permissions; missing keys fall back to defaults in code (auth.DefaultPermissions).
ALTER TABLE users ADD COLUMN permissions jsonb NOT NULL DEFAULT '{}'::jsonb;

-- +goose Down
ALTER TABLE users DROP COLUMN permissions;
