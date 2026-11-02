-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Sign-up by email, and 6-digit codes alongside email links (ADR 0014, ADR 0019).
--
-- A sign-up token has no account yet: the account is created only when the person
-- confirms their address, from the details kept in `signup`. Sign-in and sign-up
-- emails also carry a code for the same token, for when the link opens in the wrong
-- browser or outside the app. A code is guessable, so each token allows a few wrong
-- attempts and only the newest token for an address accepts codes.
ALTER TABLE email_tokens
    ALTER COLUMN account_id DROP NOT NULL,
    ADD COLUMN code_hash bytea CHECK (length(code_hash) = 32),
    ADD COLUMN attempts  integer NOT NULL DEFAULT 0,
    -- Sign-up only: the email, handle, and display name entered.
    ADD COLUMN signup    jsonb,
    DROP CONSTRAINT email_tokens_purpose_check,
    ADD CONSTRAINT email_tokens_purpose_check CHECK (purpose IN ('verify_email', 'sign_in', 'sign_up')),
    ADD CONSTRAINT email_tokens_signup_check
        CHECK ((purpose = 'sign_up') = (account_id IS NULL AND signup IS NOT NULL));
-- Codes are looked up by the address they were sent to.
CREATE INDEX email_tokens_email_idx ON email_tokens (email, purpose, id);

-- +goose Down
DELETE FROM email_tokens WHERE purpose = 'sign_up';
DROP INDEX email_tokens_email_idx;
ALTER TABLE email_tokens
    DROP CONSTRAINT email_tokens_signup_check,
    DROP CONSTRAINT email_tokens_purpose_check,
    ADD CONSTRAINT email_tokens_purpose_check CHECK (purpose IN ('verify_email', 'sign_in')),
    DROP COLUMN signup,
    DROP COLUMN attempts,
    DROP COLUMN code_hash,
    ALTER COLUMN account_id SET NOT NULL;
