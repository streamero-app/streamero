-- +goose Up
-- Live TV and DVR (D-041).

-- Tuner sources: an M3U playlist (URL or file) or an HDHomeRun device.
CREATE TABLE livetv_tuners (
    id              uuid PRIMARY KEY,
    kind            text NOT NULL CHECK (kind IN ('m3u', 'hdhomerun')),
    name            text NOT NULL,
    url             text NOT NULL DEFAULT '',
    file_path       text NOT NULL DEFAULT '',
    tuner_count     integer NOT NULL DEFAULT 0, -- 0 = unlimited (m3u)
    user_agent      text NOT NULL DEFAULT '',
    enabled         boolean NOT NULL DEFAULT true,
    device_id       text NOT NULL DEFAULT '',
    model           text NOT NULL DEFAULT '',
    last_refresh_at timestamptz,
    error           text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now()
);

-- XMLTV guide sources.
CREATE TABLE livetv_guides (
    id              uuid PRIMARY KEY,
    name            text NOT NULL,
    url             text NOT NULL DEFAULT '',
    file_path       text NOT NULL DEFAULT '',
    timezone        text NOT NULL DEFAULT '',
    refresh_hours   integer NOT NULL DEFAULT 12,
    enabled         boolean NOT NULL DEFAULT true,
    etag            text NOT NULL DEFAULT '',
    last_modified   text NOT NULL DEFAULT '',
    content_hash    text NOT NULL DEFAULT '',
    last_refresh_at timestamptz,
    last_changed_at timestamptz,
    last_result     text NOT NULL DEFAULT '',
    error           text NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now()
);

-- Channels a guide source describes.
CREATE TABLE livetv_guide_channels (
    guide_id    uuid NOT NULL REFERENCES livetv_guides (id) ON DELETE CASCADE,
    xmltv_id    text NOT NULL,
    names       text[] NOT NULL DEFAULT '{}',
    icon_url    text NOT NULL DEFAULT '',
    PRIMARY KEY (guide_id, xmltv_id)
);

-- Channels of a tuner source, with their guide mapping.
CREATE TABLE livetv_channels (
    id                uuid PRIMARY KEY,
    tuner_id          uuid NOT NULL REFERENCES livetv_tuners (id) ON DELETE CASCADE,
    remote_id         text NOT NULL,           -- stable key within the source
    number            text NOT NULL DEFAULT '',
    sort_number       double precision NOT NULL DEFAULT 0,
    name              text NOT NULL,
    tvg_id            text NOT NULL DEFAULT '',
    logo_url          text NOT NULL DEFAULT '',
    group_title       text NOT NULL DEFAULT '',
    category          text NOT NULL DEFAULT '', -- from the source (normalised)
    category_override text NOT NULL DEFAULT '',
    stream_url        text NOT NULL,
    hd                boolean NOT NULL DEFAULT false,
    hidden            boolean NOT NULL DEFAULT false,
    mapping_mode      text NOT NULL DEFAULT 'auto' CHECK (mapping_mode IN ('auto', 'manual', 'none')),
    guide_id          uuid REFERENCES livetv_guides (id) ON DELETE SET NULL,
    guide_channel     text NOT NULL DEFAULT '',
    video_codec       text NOT NULL DEFAULT '',
    audio_codec       text NOT NULL DEFAULT '',
    height            integer,
    probed_at         timestamptz,
    UNIQUE (tuner_id, remote_id)
);
CREATE INDEX livetv_channels_guide_idx ON livetv_channels (guide_id, guide_channel);
CREATE INDEX livetv_channels_sort_idx ON livetv_channels (sort_number, name);

-- Programmes. The id stays the same across refreshes of the same airing
-- (guide, channel, start), so scheduled recordings keep pointing at it.
CREATE TABLE livetv_programs (
    id            uuid PRIMARY KEY,
    guide_id      uuid NOT NULL REFERENCES livetv_guides (id) ON DELETE CASCADE,
    xmltv_channel text NOT NULL,
    start_at      timestamptz NOT NULL,
    end_at        timestamptz NOT NULL,
    title         text NOT NULL,
    subtitle      text NOT NULL DEFAULT '',
    description   text NOT NULL DEFAULT '',
    categories    text[] NOT NULL DEFAULT '{}',
    category      text NOT NULL DEFAULT '',
    season        integer,
    episode       integer,
    episode_key   text NOT NULL DEFAULT '',
    year          integer,
    is_new        boolean NOT NULL DEFAULT false,
    is_premiere   boolean NOT NULL DEFAULT false,
    is_repeat     boolean NOT NULL DEFAULT false,
    is_live       boolean NOT NULL DEFAULT false,
    image_url     text NOT NULL DEFAULT '',
    hash          text NOT NULL,
    UNIQUE (guide_id, xmltv_channel, start_at)
);
CREATE INDEX livetv_programs_end_idx ON livetv_programs (end_at);
CREATE INDEX livetv_programs_title_idx ON livetv_programs (lower(title), start_at);

-- Favourite channels per viewer (profile or account).
CREATE TABLE livetv_favorites (
    owner_id   uuid NOT NULL,
    channel_id uuid NOT NULL REFERENCES livetv_channels (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, channel_id)
);

CREATE TABLE livetv_series_rules (
    id           uuid PRIMARY KEY,
    title        text NOT NULL,
    channel_id   uuid REFERENCES livetv_channels (id) ON DELETE CASCADE,
    new_only     boolean NOT NULL DEFAULT true,
    priority     integer NOT NULL DEFAULT 0,
    pre_padding  integer,
    post_padding integer,
    keep_last    integer NOT NULL DEFAULT 0,
    created_by   uuid,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE livetv_recordings (
    id                 uuid PRIMARY KEY,
    channel_id         uuid REFERENCES livetv_channels (id) ON DELETE SET NULL,
    channel_name       text NOT NULL DEFAULT '',
    channel_number     text NOT NULL DEFAULT '',
    program_id         uuid,
    series_rule_id     uuid REFERENCES livetv_series_rules (id) ON DELETE SET NULL,
    title              text NOT NULL,
    subtitle           text NOT NULL DEFAULT '',
    description        text NOT NULL DEFAULT '',
    categories         text[] NOT NULL DEFAULT '{}',
    season             integer,
    episode            integer,
    episode_key        text NOT NULL DEFAULT '',
    year               integer,
    image_url          text NOT NULL DEFAULT '',
    start_at           timestamptz NOT NULL,
    end_at             timestamptz NOT NULL,
    pre_padding        integer NOT NULL DEFAULT 0,
    post_padding       integer NOT NULL DEFAULT 0,
    priority           integer NOT NULL DEFAULT 0,
    status             text NOT NULL DEFAULT 'scheduled'
        CHECK (status IN ('scheduled', 'conflict', 'recording', 'completed', 'failed', 'cancelled', 'deleted')),
    error              text NOT NULL DEFAULT '',
    path               text NOT NULL DEFAULT '',
    size_bytes         bigint,
    post_process_error text NOT NULL DEFAULT '',
    created_by         uuid,
    created_at         timestamptz NOT NULL DEFAULT now(),
    finished_at        timestamptz
);
CREATE INDEX livetv_recordings_status_idx ON livetv_recordings (status, start_at);
CREATE UNIQUE INDEX livetv_recordings_rule_program_idx ON livetv_recordings (series_rule_id, program_id)
    WHERE series_rule_id IS NOT NULL AND program_id IS NOT NULL;

-- +goose Down
DROP TABLE livetv_recordings;
DROP TABLE livetv_series_rules;
DROP TABLE livetv_favorites;
DROP TABLE livetv_programs;
DROP TABLE livetv_channels;
DROP TABLE livetv_guide_channels;
DROP TABLE livetv_guides;
DROP TABLE livetv_tuners;
