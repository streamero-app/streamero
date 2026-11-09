-- +goose Up
-- Account details a user can change themselves, and pictures for users and profiles.
ALTER TABLE users ADD COLUMN display_name text;
ALTER TABLE users ADD COLUMN email text;
ALTER TABLE users ADD COLUMN language text;
ALTER TABLE users ADD COLUMN has_avatar boolean NOT NULL DEFAULT false;
ALTER TABLE profiles ADD COLUMN has_avatar boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE profiles DROP COLUMN has_avatar;
ALTER TABLE users DROP COLUMN has_avatar;
ALTER TABLE users DROP COLUMN language;
ALTER TABLE users DROP COLUMN email;
ALTER TABLE users DROP COLUMN display_name;
