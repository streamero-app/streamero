-- +goose Up
-- Intro/credits detection as its own task (D-035): audio fingerprints of the start
-- and end of every episode are kept, so a new episode is compared with its season
-- without decoding the others again. A row is stale when the file's size or mtime
-- no longer match (the file was replaced).
CREATE TABLE media_fingerprints (
    file_id           uuid PRIMARY KEY REFERENCES media_files (id) ON DELETE CASCADE,
    size              bigint NOT NULL,
    mtime             timestamptz NOT NULL,
    -- Chromaprint values (little-endian uint32) of [0, intro_ms) and [credits_offset_ms, end).
    intro             bytea,
    credits           bytea,
    intro_ms          bigint NOT NULL DEFAULT 0,
    credits_offset_ms bigint NOT NULL DEFAULT 0,
    -- Black-frame fallback, computed only when the season's audio finds no credits:
    -- NULL = not checked yet, -1 = checked and nothing found.
    black_credits_ms  bigint,
    created_at        timestamptz NOT NULL DEFAULT now()
);

-- Segments found from black frames. Precedence when several sources find the same
-- kind: manual > chapter > fingerprint > blackframe (enforced by the writers).
ALTER TABLE media_segments DROP CONSTRAINT media_segments_source_check;
ALTER TABLE media_segments ADD CONSTRAINT media_segments_source_check
    CHECK (source IN ('chapter', 'fingerprint', 'blackframe', 'manual'));

-- +goose Down
DELETE FROM media_segments WHERE source = 'blackframe';
ALTER TABLE media_segments DROP CONSTRAINT media_segments_source_check;
ALTER TABLE media_segments ADD CONSTRAINT media_segments_source_check
    CHECK (source IN ('chapter', 'fingerprint', 'manual'));
DROP TABLE media_fingerprints;
