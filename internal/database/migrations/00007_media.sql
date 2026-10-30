-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Uploaded images (ADR 0011, DOMAIN_MODEL.md MediaAsset). The raw upload waits in the
-- quarantine bucket; only re-encoded variants are ever published.
CREATE TABLE media_assets (
    id               uuid PRIMARY KEY,
    owner_actor_id   uuid NOT NULL REFERENCES actors (id) ON DELETE CASCADE,
    purpose          text NOT NULL CHECK (purpose IN ('avatar')),
    status           text NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'ready', 'rejected', 'quarantined')),
    upload_bytes     bigint NOT NULL CHECK (upload_bytes > 0),
    -- Filled in by processing.
    format           text,
    width            integer,
    height           integer,
    sha256           bytea CHECK (length(sha256) = 32),
    -- 64-bit perceptual hash, for finding the same photo uploaded by someone else.
    phash            bigint,
    -- [{name, width, height, content_type, key}] in the public media bucket.
    variants         jsonb NOT NULL DEFAULT '[]'::jsonb,
    scan_verdict     text CHECK (scan_verdict IN ('not_scanned', 'clean', 'match')),
    -- An earlier, near-identical image owned by another actor (possible stolen photo).
    similar_to       uuid REFERENCES media_assets (id) ON DELETE SET NULL,
    rejection_reason text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    processed_at     timestamptz,
    CHECK ((status = 'ready') = (processed_at IS NOT NULL AND scan_verdict IS NOT NULL AND scan_verdict <> 'match'))
);
CREATE INDEX media_assets_owner_idx ON media_assets (owner_actor_id);
CREATE INDEX media_assets_pending_idx ON media_assets (created_at) WHERE status = 'pending';

ALTER TABLE actors ADD COLUMN avatar_media_id uuid REFERENCES media_assets (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE actors DROP COLUMN avatar_media_id;
DROP TABLE media_assets;
