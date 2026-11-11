-- +goose Up
-- Single sign-on (OpenID Connect) providers and LDAP sign-in (D-037).
-- Accounts created by SSO or LDAP have no local password: password_hash is ''.

CREATE TABLE oidc_providers (
    id             text PRIMARY KEY,               -- slug, part of the redirect URI
    label          text NOT NULL,                  -- button label
    issuer         text NOT NULL,
    client_id      text NOT NULL,
    client_secret  text NOT NULL DEFAULT '',       -- never returned by the API
    scopes         text[] NOT NULL DEFAULT '{openid,profile,email}',
    username_claim text NOT NULL DEFAULT 'preferred_username',
    email_claim    text NOT NULL DEFAULT 'email',
    name_claim     text NOT NULL DEFAULT 'name',
    groups_claim   text NOT NULL DEFAULT 'groups',
    admin_group    text NOT NULL DEFAULT '',
    allowed_group  text NOT NULL DEFAULT '',
    auto_create    boolean NOT NULL DEFAULT false,
    link_by_email  boolean NOT NULL DEFAULT true,
    trust_email    boolean NOT NULL DEFAULT false,
    enabled        boolean NOT NULL DEFAULT true,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

-- An external account (OIDC subject or LDAP entry) linked to a user.
CREATE TABLE user_identities (
    id            uuid PRIMARY KEY,
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind          text NOT NULL CHECK (kind IN ('oidc', 'ldap')),
    provider_id   text NOT NULL DEFAULT '',        -- oidc_providers.id; '' for LDAP
    subject       text NOT NULL,                   -- OIDC "sub" / LDAP entryUUID, objectGUID or DN
    label         text NOT NULL DEFAULT '',        -- e-mail or name, for display
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz
);
CREATE UNIQUE INDEX user_identities_subject_idx ON user_identities (kind, provider_id, subject);
CREATE UNIQUE INDEX user_identities_user_provider_idx ON user_identities (user_id, kind, provider_id);

-- Pending sign-ins (single use; the state itself is only stored hashed).
CREATE TABLE oidc_flows (
    state_hash    bytea PRIMARY KEY,
    binding_hash  bytea NOT NULL,                  -- hash of the browser cookie
    provider_id   text NOT NULL REFERENCES oidc_providers (id) ON DELETE CASCADE,
    nonce         text NOT NULL,
    pkce_verifier text NOT NULL,
    client        text NOT NULL CHECK (client IN ('web', 'app', 'link')),
    app_redirect  text NOT NULL DEFAULT '',
    app_challenge text NOT NULL DEFAULT '',
    link_user_id  uuid REFERENCES users (id) ON DELETE CASCADE,
    expires_at    timestamptz NOT NULL
);
CREATE INDEX oidc_flows_expires_idx ON oidc_flows (expires_at);

-- One-time codes handed to apps after the callback, redeemed with the app's PKCE verifier.
CREATE TABLE oidc_handoffs (
    code_hash   bytea PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider_id text NOT NULL,
    challenge   text NOT NULL,
    expires_at  timestamptz NOT NULL
);
CREATE INDEX oidc_handoffs_expires_idx ON oidc_handoffs (expires_at);

-- E-mail lookups when linking by verified e-mail.
CREATE INDEX users_email_lower_idx ON users (lower(email)) WHERE email IS NOT NULL;

-- +goose Down
DROP INDEX users_email_lower_idx;
DROP TABLE oidc_handoffs;
DROP TABLE oidc_flows;
DROP TABLE user_identities;
DROP TABLE oidc_providers;
