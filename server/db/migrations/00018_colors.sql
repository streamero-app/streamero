-- +goose Up
-- Two characteristic colours of an item's artwork (for the ambient interface); NULL = not computed yet.
ALTER TABLE items ADD COLUMN colors text[];

-- +goose Down
ALTER TABLE items DROP COLUMN colors;
