-- name: GetPersonActorID :one
SELECT id FROM actors WHERE account_id = $1;

-- name: CreateMediaAsset :exec
INSERT INTO media_assets (id, owner_actor_id, purpose, upload_bytes)
VALUES ($1, $2, $3, $4);

-- name: GetMediaAsset :one
SELECT id, owner_actor_id, purpose, status, width, height, variants, rejection_reason, created_at
FROM media_assets
WHERE id = $1;

-- name: MarkMediaReady :execrows
UPDATE media_assets
SET status = 'ready', format = $2, width = $3, height = $4, sha256 = $5, phash = $6,
    variants = $7, scan_verdict = $8, similar_to = $9, processed_at = now()
WHERE id = $1 AND status = 'pending';

-- name: MarkMediaRejected :execrows
UPDATE media_assets
SET status = 'rejected', rejection_reason = $2
WHERE id = $1 AND status = 'pending';

-- name: MarkMediaQuarantined :execrows
-- A match against known abuse imagery: kept unpublished, with the original retained
-- as evidence.
UPDATE media_assets
SET status = 'quarantined', scan_verdict = 'match'
WHERE id = $1 AND status = 'pending';

-- name: FindSimilarMedia :one
-- Finds an earlier ready image owned by another actor whose perceptual hash differs
-- in at most 6 of 64 bits.
SELECT id FROM media_assets
WHERE status = 'ready'
  AND owner_actor_id <> sqlc.arg(owner_actor_id)
  AND bit_count((phash # sqlc.arg(phash)::bigint)::bit(64)) <= 6
ORDER BY created_at
LIMIT 1;

-- name: ListStalePendingMedia :many
SELECT id FROM media_assets
WHERE status = 'pending' AND created_at < now() - interval '1 hour'
ORDER BY created_at
LIMIT 100;

-- name: SetActorAvatar :exec
UPDATE actors SET avatar_media_id = $2 WHERE id = $1;
