-- +goose Up
-- Playback state per viewer: owner_id is the active profile, or the user for the main profile.
CREATE TABLE user_item_data (
    owner_id        uuid NOT NULL,
    item_id         uuid NOT NULL REFERENCES items (id) ON DELETE CASCADE,
    position_ms     bigint NOT NULL DEFAULT 0,
    played          boolean NOT NULL DEFAULT false,
    play_count      integer NOT NULL DEFAULT 0,
    last_played_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, item_id)
);
-- "Continue watching".
CREATE INDEX user_item_data_resume_idx ON user_item_data (owner_id, last_played_at DESC)
    WHERE position_ms > 0 AND NOT played;

-- +goose Down
DROP TABLE user_item_data;
