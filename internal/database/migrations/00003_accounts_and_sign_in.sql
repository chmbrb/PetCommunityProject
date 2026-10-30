-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Accounts, person actors, sign-in methods, and sessions (DOMAIN_MODEL.md; ADR 0014, ADR 0019).

-- A login. Public identity lives on the actor.
CREATE TABLE accounts (
    id                   uuid PRIMARY KEY,
    email                text NOT NULL CHECK (email = lower(email) AND length(email) <= 254),
    email_verified_at    timestamptz,
    -- Opaque, random WebAuthn user handle (64 bytes). Never derived from the account ID or email.
    webauthn_user_handle bytea NOT NULL UNIQUE CHECK (length(webauthn_user_handle) = 64),
    status               text NOT NULL DEFAULT 'active'
                         CHECK (status IN ('active', 'restricted', 'suspended', 'banned', 'deleted')),
    created_at           timestamptz NOT NULL DEFAULT now(),
    last_active_at       timestamptz
);
CREATE UNIQUE INDEX accounts_email_key ON accounts (email);

-- Anything that can post or be followed (ADR 0012). Local actor URIs are derived from
-- the instance domain and the ID, so they aren't stored.
CREATE TABLE actors (
    id           uuid PRIMARY KEY,
    type         text NOT NULL CHECK (type IN ('person', 'organization', 'group')),
    handle       text NOT NULL CHECK (handle ~ '^[a-z0-9_]{3,30}$'),
    display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 100),
    summary      text NOT NULL DEFAULT '' CHECK (length(summary) <= 2000),
    -- Set for person actors: the account this identity belongs to.
    account_id   uuid UNIQUE REFERENCES accounts (id) ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CHECK ((type = 'person') = (account_id IS NOT NULL))
);
CREATE UNIQUE INDEX actors_handle_key ON actors (handle);

-- One sign-in method attached to an account (DOMAIN_MODEL.md AuthIdentity).
CREATE TABLE auth_identities (
    id           uuid PRIMARY KEY,
    account_id   uuid NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    kind         text NOT NULL CHECK (kind IN ('passkey', 'password', 'magic_link', 'oidc')),
    -- OIDC only: issuer and the provider's stable user ID. Matched on these, never on email.
    provider     text,
    subject      text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    CHECK ((kind = 'oidc') = (provider IS NOT NULL AND subject IS NOT NULL))
);
CREATE INDEX auth_identities_account_idx ON auth_identities (account_id);
CREATE UNIQUE INDEX auth_identities_oidc_key ON auth_identities (provider, subject) WHERE kind = 'oidc';

-- Passkey details for an auth identity of kind 'passkey'.
CREATE TABLE passkey_credentials (
    identity_id   uuid PRIMARY KEY REFERENCES auth_identities (id) ON DELETE CASCADE,
    credential_id bytea NOT NULL UNIQUE,
    -- The full go-webauthn Credential (public key, sign count, flags, transports).
    credential    jsonb NOT NULL,
    -- User-facing label, e.g. "Phone".
    name          text NOT NULL DEFAULT '' CHECK (length(name) <= 100)
);

-- Pending WebAuthn ceremonies. Each row is single-use and short-lived.
CREATE TABLE webauthn_challenges (
    id         uuid PRIMARY KEY,
    purpose    text NOT NULL CHECK (purpose IN ('signup', 'login')),
    -- go-webauthn SessionData.
    session    jsonb NOT NULL,
    -- Signup only: the details entered before the passkey was created.
    signup     jsonb,
    expires_at timestamptz NOT NULL,
    CHECK ((purpose = 'signup') = (signup IS NOT NULL))
);
CREATE INDEX webauthn_challenges_expires_idx ON webauthn_challenges (expires_at);

-- Server-side sessions (ADR 0014). Only a hash of the token is stored.
CREATE TABLE sessions (
    id           uuid PRIMARY KEY,
    token_hash   bytea NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    account_id   uuid NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    client       text NOT NULL CHECK (client IN ('web', 'mobile')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz
);
CREATE INDEX sessions_account_idx ON sessions (account_id);

-- +goose Down
DROP TABLE sessions;
DROP TABLE webauthn_challenges;
DROP TABLE passkey_credentials;
DROP TABLE auth_identities;
DROP TABLE actors;
DROP TABLE accounts;
