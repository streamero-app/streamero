-- +goose Up
-- Per-library access (D-054). A non-admin either sees every library
-- (permissions.allLibraries, the default, unchanged for existing accounts) or only
-- the libraries listed here. Adult libraries are never reached through this table
-- or through allLibraries: they have their own grant (adult_access, D-053).
CREATE TABLE user_library_access (
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    library_id  uuid NOT NULL REFERENCES libraries (id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, library_id)
);
CREATE INDEX user_library_access_library_idx ON user_library_access (library_id);

-- Accounts with a chosen list get libraries created later only when this is on.
ALTER TABLE users ADD COLUMN new_libraries boolean NOT NULL DEFAULT false;

-- Invites carry the list to the account they create (used only when the invite's
-- allLibraries permission is off).
ALTER TABLE invites ADD COLUMN library_ids uuid[] NOT NULL DEFAULT '{}';
ALTER TABLE invites ADD COLUMN new_libraries boolean NOT NULL DEFAULT false;

-- visible_library_ids is the one place that decides which libraries a viewer may
-- reach. p_owner is what every item query already carries: a profile id or a user
-- id (profiles inherit their account's libraries and can never widen them).
-- A missing allLibraries key means true (auth.DefaultPermissions). The all-ones
-- uuid (library.EveryLibrary) is server-side only, never derived from a request:
-- it sees every non-adult library, like an administrator.
-- +goose StatementBegin
CREATE FUNCTION visible_library_ids(p_owner uuid) RETURNS SETOF uuid
LANGUAGE sql STABLE PARALLEL SAFE AS $$
    SELECT l.id FROM libraries l
    WHERE p_owner = 'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid AND l.kind <> 'adult'
    UNION ALL
    SELECT l.id
    FROM users u
    JOIN libraries l ON l.kind <> 'adult'
    WHERE u.id = COALESCE((SELECT p.user_id FROM profiles p WHERE p.id = p_owner), p_owner)
      AND NOT u.disabled
      AND (u.is_admin
           OR COALESCE((u.permissions ->> 'allLibraries')::boolean, true)
           OR EXISTS (SELECT 1 FROM user_library_access a WHERE a.user_id = u.id AND a.library_id = l.id))
$$;
-- +goose StatementEnd

-- +goose Down
DROP FUNCTION visible_library_ids(uuid);
ALTER TABLE invites DROP COLUMN new_libraries;
ALTER TABLE invites DROP COLUMN library_ids;
ALTER TABLE users DROP COLUMN new_libraries;
DROP TABLE user_library_access;
