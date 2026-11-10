-- +goose Up
-- The video stream's codec profile, level and bit depth (from ffprobe). Players
-- describe what they decode by profile and level (e.g. H.264 "high" up to level
-- 5.2, HEVC "main" only), so direct play can be refused before it fails.
-- Files scanned before this migration keep the defaults until they change.
ALTER TABLE media_files
    ADD COLUMN video_profile   text NOT NULL DEFAULT '',
    ADD COLUMN video_level     integer,
    ADD COLUMN video_bit_depth integer;

-- +goose Down
ALTER TABLE media_files DROP COLUMN video_bit_depth, DROP COLUMN video_level, DROP COLUMN video_profile;
