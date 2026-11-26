-- +goose Up
-- Galleries (image sets) of adult libraries, favourite performers and hidden tags
-- (D-058). Like the rest of the adult section (D-053) they live only in adult_*
-- tables, so no query over items / media_files / user_item_data can return them.

-- A gallery is a folder of pictures (or a zip/cbz archive) in an adult library.
CREATE TABLE adult_galleries (
    id                uuid PRIMARY KEY,
    library_id        uuid NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    -- The folder, or the archive file, that holds the pictures.
    path              text NOT NULL,
    archive           boolean NOT NULL DEFAULT false,
    title             text NOT NULL,
    sort_title        text NOT NULL,
    release_date      date,
    year              integer,
    studio_id         uuid REFERENCES adult_studios (id) ON DELETE SET NULL,
    -- The scene the gallery belongs to (same studio and date), when one matches.
    scene_id          uuid REFERENCES adult_scenes (id) ON DELETE SET NULL,
    tags              text[] NOT NULL DEFAULT '{}',
    parsed            jsonb NOT NULL DEFAULT '{}'::jsonb,
    image_count       integer NOT NULL DEFAULT 0,
    cover_image_id    uuid,
    added_at          timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX adult_galleries_library_path_idx ON adult_galleries (library_id, path);
CREATE INDEX adult_galleries_date_idx ON adult_galleries (release_date DESC NULLS LAST, sort_title);
CREATE INDEX adult_galleries_added_idx ON adult_galleries (added_at DESC);
CREATE INDEX adult_galleries_studio_idx ON adult_galleries (studio_id, release_date DESC);
CREATE INDEX adult_galleries_scene_idx ON adult_galleries (scene_id);

CREATE TABLE adult_gallery_performers (
    gallery_id    uuid NOT NULL REFERENCES adult_galleries (id) ON DELETE CASCADE,
    performer_id  uuid NOT NULL REFERENCES adult_performers (id) ON DELETE CASCADE,
    position      integer NOT NULL DEFAULT 0,
    PRIMARY KEY (gallery_id, performer_id)
);
CREATE INDEX adult_gallery_performers_performer_idx ON adult_gallery_performers (performer_id);

-- One picture of a gallery: a file, or an entry of the gallery's archive.
CREATE TABLE adult_images (
    id          uuid PRIMARY KEY,
    gallery_id  uuid NOT NULL REFERENCES adult_galleries (id) ON DELETE CASCADE,
    library_id  uuid NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    path        text NOT NULL,
    entry       text NOT NULL DEFAULT '',
    position    integer NOT NULL DEFAULT 0,
    size        bigint NOT NULL,
    mtime       timestamptz NOT NULL,
    width       integer,
    height      integer,
    -- EXIF orientation 1–8 (0 = none); width and height are as stored.
    orientation integer NOT NULL DEFAULT 0,
    format      text NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX adult_images_library_path_idx ON adult_images (library_id, path, entry);
CREATE INDEX adult_images_gallery_idx ON adult_images (gallery_id, position);

CREATE INDEX adult_scenes_tags_idx ON adult_scenes USING gin (tags);
CREATE INDEX adult_galleries_tags_idx ON adult_galleries USING gin (tags);

CREATE TABLE adult_favorite_performers (
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    performer_id  uuid NOT NULL REFERENCES adult_performers (id) ON DELETE CASCADE,
    added_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, performer_id)
);

ALTER TABLE adult_prefs ADD COLUMN hidden_tags text[] NOT NULL DEFAULT '{}';
ALTER TABLE adult_prefs ADD COLUMN hide_tagged boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE adult_prefs DROP COLUMN hide_tagged;
ALTER TABLE adult_prefs DROP COLUMN hidden_tags;
DROP TABLE adult_favorite_performers;
DROP INDEX adult_scenes_tags_idx;
DROP TABLE adult_images;
DROP TABLE adult_gallery_performers;
DROP TABLE adult_galleries;
