-- +goose Up
-- Books (EPUB, PDF, MOBI/AZW3, comics), audiobooks and photos (D-039).
ALTER TABLE libraries DROP CONSTRAINT libraries_kind_check;
ALTER TABLE libraries ADD CONSTRAINT libraries_kind_check
    CHECK (kind IN ('movies', 'shows', 'music', 'mixed', 'books', 'audiobooks', 'photos'));
ALTER TABLE items DROP CONSTRAINT items_kind_check;
ALTER TABLE items ADD CONSTRAINT items_kind_check
    CHECK (kind IN ('movie', 'series', 'season', 'episode', 'artist', 'album', 'track',
                    'book', 'audiobook', 'photoalbum', 'photo', 'video'));

-- What the scanner read from the file itself (OPF, ComicInfo.xml, tags, EXIF); owned
-- by the scanner and rewritten when the file changes. The columns below are copies of
-- the parts that are filtered or sorted on.
ALTER TABLE items
    ADD COLUMN media_meta   jsonb,
    ADD COLUMN authors      text[] NOT NULL DEFAULT '{}',
    ADD COLUMN series_name  text,
    ADD COLUMN series_index real,
    ADD COLUMN taken_at     timestamptz;
-- Authors tab and "books by this author".
CREATE INDEX items_authors_idx ON items USING gin (authors) WHERE kind IN ('book', 'audiobook');
-- Series tab and "more in this series".
CREATE INDEX items_series_idx ON items (library_id, series_name, series_index) WHERE series_name IS NOT NULL;
-- Photos of an album in the order they were taken.
CREATE INDEX items_parent_taken_idx ON items (parent_id, taken_at, sort_title) WHERE kind IN ('photo', 'video');
-- Every photo and video of a library by date (the "all photos" timeline).
CREATE INDEX items_library_taken_idx ON items (library_id, taken_at DESC) WHERE kind IN ('photo', 'video');

-- Reading position per viewer: the page (PDF, comics), an EPUB CFI locator and the
-- overall fraction read; audiobooks keep using position_ms.
ALTER TABLE user_item_data
    ADD COLUMN page        integer,
    ADD COLUMN locator     text,
    ADD COLUMN progression real;

-- +goose Down
ALTER TABLE user_item_data DROP COLUMN progression, DROP COLUMN locator, DROP COLUMN page;
DROP INDEX items_library_taken_idx;
DROP INDEX items_parent_taken_idx;
DROP INDEX items_series_idx;
DROP INDEX items_authors_idx;
ALTER TABLE items DROP COLUMN taken_at, DROP COLUMN series_index, DROP COLUMN series_name,
    DROP COLUMN authors, DROP COLUMN media_meta;
DELETE FROM items WHERE kind IN ('book', 'audiobook', 'photoalbum', 'photo', 'video');
DELETE FROM libraries WHERE kind IN ('books', 'audiobooks', 'photos');
ALTER TABLE items DROP CONSTRAINT items_kind_check;
ALTER TABLE items ADD CONSTRAINT items_kind_check
    CHECK (kind IN ('movie', 'series', 'season', 'episode', 'artist', 'album', 'track'));
ALTER TABLE libraries DROP CONSTRAINT libraries_kind_check;
ALTER TABLE libraries ADD CONSTRAINT libraries_kind_check CHECK (kind IN ('movies', 'shows', 'music', 'mixed'));
