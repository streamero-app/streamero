-- +goose Up
-- Music: artists, albums and tracks are items too; tracks keep their own artist,
-- and every file remembers its tags so unchanged files are never probed again.
ALTER TABLE items DROP CONSTRAINT items_kind_check;
ALTER TABLE items ADD CONSTRAINT items_kind_check
    CHECK (kind IN ('movie', 'series', 'season', 'episode', 'artist', 'album', 'track'));
ALTER TABLE items ADD COLUMN artist text;
ALTER TABLE media_files ADD COLUMN tags jsonb NOT NULL DEFAULT '{}'::jsonb;
CREATE INDEX items_library_kind_idx ON items (library_id, kind, sort_title);

-- +goose Down
DROP INDEX items_library_kind_idx;
ALTER TABLE media_files DROP COLUMN tags;
ALTER TABLE items DROP COLUMN artist;
DELETE FROM items WHERE kind IN ('artist', 'album', 'track');
ALTER TABLE items DROP CONSTRAINT items_kind_check;
ALTER TABLE items ADD CONSTRAINT items_kind_check CHECK (kind IN ('movie', 'series', 'season', 'episode'));
