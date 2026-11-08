-- +goose Up
-- Chapters, trickplay thumbnails and skippable segments (intro, credits…) per media file.
ALTER TABLE media_files
    ADD COLUMN chapters     jsonb NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN trickplay    jsonb,
    -- Set once analysis ran for the file (also when it found nothing), so it is not repeated.
    ADD COLUMN analyzed_at  timestamptz;

CREATE TABLE media_segments (
    file_id   uuid NOT NULL REFERENCES media_files (id) ON DELETE CASCADE,
    kind      text NOT NULL CHECK (kind IN ('intro', 'credits', 'recap', 'preview')),
    start_ms  bigint NOT NULL,
    end_ms    bigint NOT NULL CHECK (end_ms > start_ms),
    source    text NOT NULL CHECK (source IN ('chapter', 'fingerprint', 'manual')),
    PRIMARY KEY (file_id, kind)
);

-- +goose Down
DROP TABLE media_segments;
ALTER TABLE media_files DROP COLUMN analyzed_at, DROP COLUMN trickplay, DROP COLUMN chapters;
