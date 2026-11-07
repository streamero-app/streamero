-- +goose Up
CREATE TABLE libraries (
    id            uuid PRIMARY KEY,
    name          text NOT NULL,
    kind          text NOT NULL CHECK (kind IN ('movies', 'shows', 'music', 'mixed')),
    paths         text[] NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    scan_state    text NOT NULL DEFAULT 'idle' CHECK (scan_state IN ('idle', 'queued', 'scanning', 'failed')),
    scan_error    text,
    last_scan_at  timestamptz,
    files_seen    integer NOT NULL DEFAULT 0
);

-- Movies, series, seasons and episodes. `path` is the folder (movie/series/season)
-- or file (episode) the item was built from; it is unique per library.
CREATE TABLE items (
    id                  uuid PRIMARY KEY,
    library_id          uuid NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    parent_id           uuid REFERENCES items (id) ON DELETE CASCADE,
    kind                text NOT NULL CHECK (kind IN ('movie', 'series', 'season', 'episode')),
    path                text NOT NULL,
    title               text NOT NULL,
    sort_title          text NOT NULL,
    year                integer,
    season_number       integer,
    episode_number      integer,
    episode_number_end  integer,
    provider_ids        jsonb NOT NULL DEFAULT '{}'::jsonb,
    added_at            timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX items_library_path_idx ON items (library_id, path);
-- Library grid (top-level items sorted by title / year / added).
CREATE INDEX items_top_title_idx ON items (library_id, sort_title) WHERE parent_id IS NULL;
CREATE INDEX items_top_added_idx ON items (library_id, added_at DESC) WHERE parent_id IS NULL;
CREATE INDEX items_parent_idx ON items (parent_id, season_number, episode_number);

-- Video/audio files and their ffprobe results. Several files = versions of one item.
CREATE TABLE media_files (
    id            uuid PRIMARY KEY,
    item_id       uuid NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    library_id    uuid NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    path          text NOT NULL,
    label         text NOT NULL DEFAULT '',
    size          bigint NOT NULL,
    mtime         timestamptz NOT NULL,
    container     text NOT NULL DEFAULT '',
    duration_ms   bigint,
    width         integer,
    height        integer,
    video_codec   text NOT NULL DEFAULT '',
    hdr           text NOT NULL DEFAULT '',
    audio         jsonb NOT NULL DEFAULT '[]'::jsonb,
    subtitles     jsonb NOT NULL DEFAULT '[]'::jsonb,
    probe_error   text
);
CREATE UNIQUE INDEX media_files_library_path_idx ON media_files (library_id, path);
CREATE INDEX media_files_item_idx ON media_files (item_id);

-- +goose Down
DROP TABLE media_files;
DROP TABLE items;
DROP TABLE libraries;
