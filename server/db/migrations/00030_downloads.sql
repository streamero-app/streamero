-- +goose Up
-- Offline downloads (D-040). A transcode is one file of the download cache: a media
-- file converted to a mobile-friendly MP4 in one quality and with one audio track,
-- shared by everyone who asks for the same thing. A download is one profile's request
-- for a file in a quality (original files need no transcode). Clients delete their
-- request once the file is on the device; unused transcodes are cleaned up later.
CREATE TABLE download_transcodes (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    file_id        uuid NOT NULL REFERENCES media_files (id) ON DELETE CASCADE,
    quality        text NOT NULL CHECK (quality IN ('source', '1080p', '720p')),
    audio_index    integer NOT NULL,
    state          text NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'transcoding', 'ready', 'failed')),
    progress       real NOT NULL DEFAULT 0,
    size_bytes     bigint NOT NULL DEFAULT 0,
    -- Size of the source when the transcode was made: a replaced file starts it again.
    source_size    bigint NOT NULL DEFAULT 0,
    error          text NOT NULL DEFAULT '',
    attempts       integer NOT NULL DEFAULT 0,
    created_at     timestamptz NOT NULL DEFAULT now(),
    started_at     timestamptz,
    finished_at    timestamptz,
    -- Written while ffmpeg runs; a stale heartbeat means the worker died.
    heartbeat_at   timestamptz,
    last_access_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (file_id, quality, audio_index)
);
CREATE INDEX download_transcodes_state_idx ON download_transcodes (state, created_at);

CREATE TABLE downloads (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- Profile or user whose library access, parental limit and progress apply.
    owner_id     uuid NOT NULL,
    item_id      uuid NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    file_id      uuid NOT NULL REFERENCES media_files (id) ON DELETE CASCADE,
    quality      text NOT NULL CHECK (quality IN ('original', 'source', '1080p', '720p')),
    audio_index  integer NOT NULL DEFAULT -1,
    transcode_id uuid REFERENCES download_transcodes (id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, file_id, quality, audio_index)
);
CREATE INDEX downloads_owner_idx ON downloads (owner_id, created_at DESC);
CREATE INDEX downloads_transcode_idx ON downloads (transcode_id);

-- +goose Down
DROP TABLE downloads;
DROP TABLE download_transcodes;
