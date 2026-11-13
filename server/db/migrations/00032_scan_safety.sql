-- +goose Up
-- Scan safety (D-043): files a scan did not find are kept when the removal looks like
-- an offline or half-mounted share; the library remembers how many until an admin
-- confirms the removal.
ALTER TABLE libraries ADD COLUMN held_removals integer NOT NULL DEFAULT 0;

-- When ffprobe last ran on a file: failed probes are retried at most once an hour.
ALTER TABLE media_files ADD COLUMN probed_at timestamptz;

-- Re-scans used to replace the provider IDs found by the metadata refresh with the
-- (usually empty) IDs from the file name. Titles that were matched but lost their
-- TMDb ID that way are refreshed again (the scan now keeps them).
UPDATE items SET metadata_refreshed_at = NULL
WHERE parent_id IS NULL AND kind IN ('movie', 'series') AND metadata_refreshed_at IS NOT NULL
    AND metadata_error IS NULL AND NOT metadata_locked AND NOT (provider_ids ? 'tmdb');

-- +goose Down
ALTER TABLE media_files DROP COLUMN probed_at;
ALTER TABLE libraries DROP COLUMN held_removals;
