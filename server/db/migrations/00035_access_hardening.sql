-- +goose Up
-- Parental limits are applied everywhere items are shown (D-053). The set of
-- rating labels in use is read on each request of a limited profile, and lists
-- exclude items by label: both use this index.
CREATE INDEX items_official_rating_idx ON items (official_rating) WHERE official_rating IS NOT NULL;

-- Addresses an account signed in from before. While the account is locked
-- after wrong passwords, a sign-in from one of them is still checked, with its
-- own failure count, so guessing a username's password from elsewhere cannot
-- keep its owner out (D-053).
CREATE TABLE login_clients (
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    address       text NOT NULL,
    last_login_at timestamptz NOT NULL DEFAULT now(),
    failed_logins integer NOT NULL DEFAULT 0,
    locked_until  timestamptz,
    PRIMARY KEY (user_id, address)
);

-- +goose Down
DROP TABLE login_clients;
DROP INDEX items_official_rating_idx;
