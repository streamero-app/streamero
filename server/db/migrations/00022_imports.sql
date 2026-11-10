-- +goose Up
-- Imports of watch history from another media server. The API key lives in `secret`
-- only while the run is queued or running; the worker clears it when the run ends.
CREATE TABLE import_runs (
    id           uuid PRIMARY KEY,
    created_by   uuid,
    status       text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed')),
    dry_run      boolean NOT NULL,
    source_url   text NOT NULL,
    secret       text,
    options      jsonb NOT NULL DEFAULT '{}'::jsonb,
    progress     jsonb NOT NULL DEFAULT '{}'::jsonb,
    report       jsonb,
    error        text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    started_at   timestamptz,
    finished_at  timestamptz
);
CREATE INDEX import_runs_created_idx ON import_runs (created_at DESC);
-- At most one import is queued or running at a time.
CREATE UNIQUE INDEX import_runs_one_active_idx ON import_runs ((true)) WHERE status IN ('queued', 'running');

-- Playlists created by an import, so running it again updates them instead of
-- creating copies. source_server is the other server's id, source_id its playlist id.
CREATE TABLE import_playlists (
    source_server  text NOT NULL,
    source_id      text NOT NULL,
    owner_id       uuid NOT NULL,
    playlist_id    uuid NOT NULL REFERENCES playlists (id) ON DELETE CASCADE,
    PRIMARY KEY (source_server, source_id, owner_id)
);

-- +goose Down
DROP TABLE import_playlists;
DROP TABLE import_runs;
