-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- One-time tokens sent by email: address verification and sign-in links (ADR 0019).
-- Only a hash of each token is stored; the token itself exists only in the email.
CREATE TABLE email_tokens (
    id         uuid PRIMARY KEY,
    token_hash bytea NOT NULL UNIQUE CHECK (length(token_hash) = 32),
    account_id uuid NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    purpose    text NOT NULL CHECK (purpose IN ('verify_email', 'sign_in')),
    -- The address the token was sent to. A token stops working if the account's
    -- email changes, so a link sent to an old address can't verify a new one.
    email      text NOT NULL,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX email_tokens_account_idx ON email_tokens (account_id);
CREATE INDEX email_tokens_expires_idx ON email_tokens (expires_at);

-- +goose Down
DROP TABLE email_tokens;
