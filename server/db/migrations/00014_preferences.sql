-- +goose Up
-- Playback preferences per viewer (profile, or the user for the main profile).
CREATE TABLE viewer_preferences (
    owner_id    uuid PRIMARY KEY,
    prefs       jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at  timestamptz NOT NULL DEFAULT now()
);
-- Next Up walks a viewer's played episodes.
CREATE INDEX user_item_data_played_idx ON user_item_data (owner_id, last_played_at DESC) WHERE played;

-- +goose Down
DROP INDEX user_item_data_played_idx;
DROP TABLE viewer_preferences;
