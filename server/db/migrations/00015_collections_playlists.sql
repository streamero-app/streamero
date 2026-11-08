-- +goose Up
-- Collections (box sets): from TMDb (tmdb_id) or made by an administrator.
CREATE TABLE collections (
    id            uuid PRIMARY KEY,
    name          text NOT NULL,
    overview      text,
    tmdb_id       integer UNIQUE,
    has_poster    boolean NOT NULL DEFAULT false,
    has_backdrop  boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE collection_items (
    collection_id  uuid NOT NULL REFERENCES collections (id) ON DELETE CASCADE,
    item_id        uuid NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    PRIMARY KEY (collection_id, item_id)
);
CREATE INDEX collection_items_item_idx ON collection_items (item_id);

-- Playlists belong to a viewer (profile or user); public ones are visible to everyone.
CREATE TABLE playlists (
    id          uuid PRIMARY KEY,
    owner_id    uuid NOT NULL,
    name        text NOT NULL,
    public      boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX playlists_owner_idx ON playlists (owner_id, updated_at DESC);
CREATE TABLE playlist_items (
    playlist_id  uuid NOT NULL REFERENCES playlists (id) ON DELETE CASCADE,
    position     integer NOT NULL,
    item_id      uuid NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    PRIMARY KEY (playlist_id, position)
);

-- +goose Down
DROP TABLE playlist_items;
DROP TABLE playlists;
DROP TABLE collection_items;
DROP TABLE collections;
