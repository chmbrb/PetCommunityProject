-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Account settings: adding passkeys to an existing account, and a readable list of
-- signed-in devices.

-- Challenges for adding a passkey belong to the signed-in account that started them.
ALTER TABLE webauthn_challenges DROP CONSTRAINT webauthn_challenges_purpose_check;
ALTER TABLE webauthn_challenges
    ADD CONSTRAINT webauthn_challenges_purpose_check CHECK (purpose IN ('signup', 'login', 'add_passkey')),
    ADD COLUMN account_id uuid REFERENCES accounts (id) ON DELETE CASCADE,
    ADD CONSTRAINT webauthn_challenges_account_check CHECK ((purpose = 'add_passkey') = (account_id IS NOT NULL));

-- The browser or app that signed in, so people can recognize their devices. Kept only
-- as long as the session.
ALTER TABLE sessions ADD COLUMN user_agent text NOT NULL DEFAULT '' CHECK (length(user_agent) <= 256);

-- +goose Down
ALTER TABLE sessions DROP COLUMN user_agent;
DELETE FROM webauthn_challenges WHERE purpose = 'add_passkey';
ALTER TABLE webauthn_challenges
    DROP CONSTRAINT webauthn_challenges_account_check,
    DROP COLUMN account_id,
    DROP CONSTRAINT webauthn_challenges_purpose_check;
ALTER TABLE webauthn_challenges
    ADD CONSTRAINT webauthn_challenges_purpose_check CHECK (purpose IN ('signup', 'login'));
