-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Optional passwords, lockouts, and recognized devices (ADR 0014).

ALTER TABLE accounts
    -- argon2id in the PHC string format, e.g. $argon2id$v=19$m=19456,t=2,p=1$salt$hash.
    ADD COLUMN password_hash            text,
    ADD COLUMN password_changed_at      timestamptz,
    -- Consecutive wrong passwords; reset by a correct one or when a lockout starts.
    ADD COLUMN failed_password_attempts integer NOT NULL DEFAULT 0,
    -- Password sign-in is refused until then. Other sign-in methods still work.
    ADD COLUMN password_locked_until    timestamptz;

-- new_device: the code that confirms a password sign-in from an unrecognized device.
ALTER TABLE email_tokens
    DROP CONSTRAINT email_tokens_purpose_check,
    ADD CONSTRAINT email_tokens_purpose_check CHECK (purpose IN ('verify_email', 'sign_in', 'sign_up', 'new_device'));

-- Devices (browsers and app installs) that have signed in to an account. A password
-- sign-in from any other device needs an emailed code. Only a hash of the device's
-- token is stored; the token lives in a cookie or the app's storage.
CREATE TABLE known_devices (
    id           uuid PRIMARY KEY,
    account_id   uuid NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    token_hash   bytea NOT NULL CHECK (length(token_hash) = 32),
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    UNIQUE (account_id, token_hash)
);
CREATE INDEX known_devices_expires_idx ON known_devices (expires_at);

-- +goose Down
DROP TABLE known_devices;
DELETE FROM email_tokens WHERE purpose = 'new_device';
ALTER TABLE email_tokens
    DROP CONSTRAINT email_tokens_purpose_check,
    ADD CONSTRAINT email_tokens_purpose_check CHECK (purpose IN ('verify_email', 'sign_in', 'sign_up'));
ALTER TABLE accounts
    DROP COLUMN password_locked_until,
    DROP COLUMN failed_password_attempts,
    DROP COLUMN password_changed_at,
    DROP COLUMN password_hash;
