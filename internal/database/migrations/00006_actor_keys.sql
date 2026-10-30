-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Actors' signing keys for ActivityPub (ADR 0012). The worker generates them after
-- an actor is created; until then the columns are empty.
ALTER TABLE actors
    ADD COLUMN public_key_pem  text,
    -- AES-256-GCM, bound to the actor ID, under a key derived from PC_SECRET_KEY.
    ADD COLUMN private_key_enc bytea,
    -- Identifies the PC_SECRET_KEY that encrypted it, without revealing it.
    ADD COLUMN private_key_kid text,
    ADD COLUMN keys_created_at timestamptz,
    ADD CONSTRAINT actors_keys_complete CHECK (
        (public_key_pem IS NULL) = (private_key_enc IS NULL)
        AND (public_key_pem IS NULL) = (private_key_kid IS NULL)
        AND (public_key_pem IS NULL) = (keys_created_at IS NULL)
    );
CREATE INDEX actors_missing_keys_idx ON actors (created_at) WHERE public_key_pem IS NULL;

-- +goose Down
DROP INDEX actors_missing_keys_idx;
ALTER TABLE actors
    DROP CONSTRAINT actors_keys_complete,
    DROP COLUMN keys_created_at,
    DROP COLUMN private_key_kid,
    DROP COLUMN private_key_enc,
    DROP COLUMN public_key_pem;
