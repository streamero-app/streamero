-- +goose Up
-- Studios / networks behind a title, and when its local .nfo file was last read.
ALTER TABLE items ADD COLUMN studios text[] NOT NULL DEFAULT '{}';
ALTER TABLE items ADD COLUMN nfo_read_at timestamptz;

-- +goose Down
ALTER TABLE items DROP COLUMN nfo_read_at;
ALTER TABLE items DROP COLUMN studios;
