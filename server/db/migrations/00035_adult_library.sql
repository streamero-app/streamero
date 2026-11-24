-- +goose Up
-- Optional adult libraries (D-053). Their content lives only in the adult_* tables,
-- never in items / media_files / user_item_data, so no existing query (home, search,
-- compat API, DLNA, downloads, stats, collections …) can return it.
ALTER TABLE libraries DROP CONSTRAINT libraries_kind_check;
ALTER TABLE libraries ADD CONSTRAINT libraries_kind_check
    CHECK (kind IN ('movies', 'shows', 'music', 'mixed', 'books', 'audiobooks', 'photos', 'adult'));

-- Adult access is a permission of its own, outside users.permissions: defaults,
-- invites, sign-in providers and allLibraries can never set it.
CREATE TABLE adult_access (
    user_id     uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    granted_by  uuid REFERENCES users (id) ON DELETE SET NULL,
    granted_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE adult_prefs (
    user_id   uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    blur      boolean NOT NULL DEFAULT true,
    pin_hash  text
);

CREATE TABLE adult_studios (
    id            uuid PRIMARY KEY,
    name          text NOT NULL,
    name_key      text NOT NULL UNIQUE,
    parent        text,
    url           text,
    provider_ids  jsonb NOT NULL DEFAULT '{}'::jsonb,
    has_image     boolean NOT NULL DEFAULT false,
    refreshed_at  timestamptz
);

CREATE TABLE adult_performers (
    id            uuid PRIMARY KEY,
    name          text NOT NULL,
    name_key      text NOT NULL UNIQUE,
    aliases       text[] NOT NULL DEFAULT '{}',
    gender        text,
    birthdate     text,
    country       text,
    description   text,
    provider_ids  jsonb NOT NULL DEFAULT '{}'::jsonb,
    has_image     boolean NOT NULL DEFAULT false,
    refreshed_at  timestamptz
);

CREATE TABLE adult_scenes (
    id                     uuid PRIMARY KEY,
    library_id             uuid NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    kind                   text NOT NULL CHECK (kind IN ('scene', 'movie')),
    path                   text NOT NULL,
    title                  text NOT NULL,
    sort_title             text NOT NULL,
    release_date           date,
    year                   integer,
    studio_id              uuid REFERENCES adult_studios (id) ON DELETE SET NULL,
    description            text,
    url                    text,
    tags                   text[] NOT NULL DEFAULT '{}',
    provider_ids           jsonb NOT NULL DEFAULT '{}'::jsonb,
    has_poster             boolean NOT NULL DEFAULT false,
    has_backdrop           boolean NOT NULL DEFAULT false,
    -- What the file name said (studio, performers), kept so a rescan does not
    -- overwrite matched metadata and an unmatched scene keeps its parsed names.
    parsed                 jsonb NOT NULL DEFAULT '{}'::jsonb,
    metadata_refreshed_at  timestamptz,
    metadata_error         text,
    added_at               timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX adult_scenes_library_path_idx ON adult_scenes (library_id, path);
CREATE INDEX adult_scenes_date_idx ON adult_scenes (release_date DESC NULLS LAST, sort_title);
CREATE INDEX adult_scenes_added_idx ON adult_scenes (added_at DESC);
CREATE INDEX adult_scenes_studio_idx ON adult_scenes (studio_id, release_date DESC);

CREATE TABLE adult_scene_performers (
    scene_id      uuid NOT NULL REFERENCES adult_scenes (id) ON DELETE CASCADE,
    performer_id  uuid NOT NULL REFERENCES adult_performers (id) ON DELETE CASCADE,
    position      integer NOT NULL DEFAULT 0,
    PRIMARY KEY (scene_id, performer_id)
);
CREATE INDEX adult_scene_performers_performer_idx ON adult_scene_performers (performer_id);

CREATE TABLE adult_files (
    id             uuid PRIMARY KEY,
    scene_id       uuid NOT NULL REFERENCES adult_scenes (id) ON DELETE CASCADE,
    library_id     uuid NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    path           text NOT NULL,
    size           bigint NOT NULL,
    mtime          timestamptz NOT NULL,
    container      text NOT NULL DEFAULT '',
    duration_ms    bigint,
    width          integer,
    height         integer,
    video_codec    text NOT NULL DEFAULT '',
    video_profile  text NOT NULL DEFAULT '',
    video_level    integer NOT NULL DEFAULT 0,
    video_bit_depth integer NOT NULL DEFAULT 0,
    hdr            text NOT NULL DEFAULT '',
    audio          jsonb NOT NULL DEFAULT '[]'::jsonb,
    subtitles      jsonb NOT NULL DEFAULT '[]'::jsonb,
    probe_error    text
);
CREATE UNIQUE INDEX adult_files_library_path_idx ON adult_files (library_id, path);
CREATE INDEX adult_files_scene_idx ON adult_files (scene_id);

CREATE TABLE adult_progress (
    user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    scene_id        uuid NOT NULL REFERENCES adult_scenes (id) ON DELETE CASCADE,
    position_ms     bigint NOT NULL DEFAULT 0,
    played          boolean NOT NULL DEFAULT false,
    last_played_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, scene_id)
);

-- Answers of ThePornDB / StashDB (search 1 h, details 12 h).
CREATE TABLE adult_meta_cache (
    key         text PRIMARY KEY,
    body        bytea NOT NULL,
    fetched_at  timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE adult_meta_cache;
DROP TABLE adult_progress;
DROP TABLE adult_files;
DROP TABLE adult_scene_performers;
DROP TABLE adult_scenes;
DROP TABLE adult_performers;
DROP TABLE adult_studios;
DROP TABLE adult_prefs;
DROP TABLE adult_access;
DELETE FROM libraries WHERE kind = 'adult';
ALTER TABLE libraries DROP CONSTRAINT libraries_kind_check;
ALTER TABLE libraries ADD CONSTRAINT libraries_kind_check
    CHECK (kind IN ('movies', 'shows', 'music', 'mixed', 'books', 'audiobooks', 'photos'));
