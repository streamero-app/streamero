-- +goose Up
-- Netflix-style profiles inside an account. The account itself is the main
-- profile; each session may switch to one of these.
CREATE TABLE profiles (
    id            uuid PRIMARY KEY,
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name          text NOT NULL,
    color         text NOT NULL,
    pin_hash      text,
    is_kid        boolean NOT NULL DEFAULT false,
    max_rating    integer,
    failed_pins   integer NOT NULL DEFAULT 0,
    locked_until  timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX profiles_user_idx ON profiles (user_id);

ALTER TABLE sessions ADD COLUMN profile_id uuid REFERENCES profiles (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE sessions DROP COLUMN profile_id;
DROP TABLE profiles;
