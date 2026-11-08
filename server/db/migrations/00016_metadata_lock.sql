-- +goose Up
-- Hand-edited metadata is kept: refreshes skip locked items.
ALTER TABLE items ADD COLUMN metadata_locked boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE items DROP COLUMN metadata_locked;
