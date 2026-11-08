-- +goose Up
-- Title logos (transparent PNG from TMDb), cast photos and frame-grab artwork.
ALTER TABLE items
    ADD COLUMN has_logo       boolean NOT NULL DEFAULT false,
    -- Set once a frame was tried for an item without artwork, so it is not retried every scan.
    ADD COLUMN art_grabbed_at timestamptz;
-- TMDb person id; the photo is cached as images/people/<id>.jpg when present.
ALTER TABLE item_people ADD COLUMN tmdb_person_id integer;
ALTER TABLE item_people ADD COLUMN has_image boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE item_people DROP COLUMN has_image, DROP COLUMN tmdb_person_id;
ALTER TABLE items DROP COLUMN art_grabbed_at, DROP COLUMN has_logo;
