-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Two-step sign-in: TOTP authenticators, recovery codes, and pending second steps
-- (ADR 0014).

-- An authenticator app or password manager entry. One per account. Unconfirmed until
-- the person enters a valid code; only confirmed factors turn on two-step sign-in.
CREATE TABLE totp_factors (
    account_id       uuid PRIMARY KEY REFERENCES accounts (id) ON DELETE CASCADE,
    -- AES-256-GCM under a key derived from the API's MFA key, bound to the account ID.
    secret_encrypted bytea NOT NULL,
    -- Identifies the MFA key that encrypted the secret, to detect a wrong or rotated key.
    key_id           text NOT NULL,
    -- The last time step a code was accepted for; codes for it or earlier are refused,
    -- so a code can't be replayed.
    last_used_step   bigint NOT NULL DEFAULT 0,
    confirmed_at     timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    last_used_at     timestamptz
);

-- Single-use codes for when the authenticator isn't available. Only hashes are stored.
CREATE TABLE recovery_codes (
    id         uuid PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    code_hash  bytea NOT NULL CHECK (length(code_hash) = 32),
    created_at timestamptz NOT NULL DEFAULT now(),
    used_at    timestamptz,
    UNIQUE (account_id, code_hash)
);

-- A sign-in whose first step (email or password) succeeded and that waits for the
-- second. The client holds the token; only its hash is stored.
CREATE TABLE mfa_challenges (
    id         uuid PRIMARY KEY,
    token_hash bytea NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    account_id uuid NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    client     text NOT NULL CHECK (client IN ('web', 'mobile')),
    attempts   integer NOT NULL DEFAULT 0,
    expires_at timestamptz NOT NULL
);
CREATE INDEX mfa_challenges_expires_idx ON mfa_challenges (expires_at);

-- +goose Down
DROP TABLE mfa_challenges;
DROP TABLE recovery_codes;
DROP TABLE totp_factors;
