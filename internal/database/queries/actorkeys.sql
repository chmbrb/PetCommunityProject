-- name: SetActorKeys :execrows
-- Stores a new key pair, unless the actor already has one.
UPDATE actors
SET public_key_pem = $2, private_key_enc = $3, private_key_kid = $4, keys_created_at = now()
WHERE id = $1 AND public_key_pem IS NULL;

-- name: ListActorsWithoutKeys :many
SELECT id FROM actors
WHERE public_key_pem IS NULL
ORDER BY created_at
LIMIT $1;

-- name: GetActorKeys :one
SELECT public_key_pem, private_key_enc, private_key_kid
FROM actors
WHERE id = $1 AND public_key_pem IS NOT NULL;
