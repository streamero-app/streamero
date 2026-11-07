-- +goose Up
ALTER TABLE items
    ADD COLUMN original_title         text,
    ADD COLUMN overview               text,
    ADD COLUMN tagline                text,
    ADD COLUMN genres                 text[] NOT NULL DEFAULT '{}',
    ADD COLUMN community_rating       real,
    ADD COLUMN official_rating        text,
    ADD COLUMN premiere_date          date,
    ADD COLUMN has_poster             boolean NOT NULL DEFAULT false,
    ADD COLUMN has_backdrop           boolean NOT NULL DEFAULT false,
    ADD COLUMN metadata_refreshed_at  timestamptz,
    ADD COLUMN metadata_error         text,
    -- The scanner owns title/year from file names; once metadata set a title the
    -- scanner must not overwrite it on the next scan.
    ADD COLUMN metadata_title         text;

-- Items still waiting for metadata (refresh job picks these first).
CREATE INDEX items_metadata_pending_idx ON items (library_id) WHERE metadata_refreshed_at IS NULL;

CREATE TABLE item_people (
    item_id    uuid NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    position   integer NOT NULL,
    kind       text NOT NULL CHECK (kind IN ('cast', 'crew')),
    name       text NOT NULL,
    role       text NOT NULL DEFAULT '',
    PRIMARY KEY (item_id, kind, position)
);

-- +goose Down
DROP TABLE item_people;
DROP INDEX items_metadata_pending_idx;
ALTER TABLE items
    DROP COLUMN original_title, DROP COLUMN overview, DROP COLUMN tagline, DROP COLUMN genres,
    DROP COLUMN community_rating, DROP COLUMN official_rating, DROP COLUMN premiere_date,
    DROP COLUMN has_poster, DROP COLUMN has_backdrop, DROP COLUMN metadata_refreshed_at,
    DROP COLUMN metadata_error, DROP COLUMN metadata_title;
