-- Key rotation (ADR 0025): how much data each key holds, and moving it to a new key.
-- Every replace checks the value is still under the key it was read with, so a value
-- changed meanwhile (a new authenticator, say) is left alone.

-- name: CountActorKeysByKeyID :many
SELECT private_key_kid AS key_id, count(*) AS n
FROM actors
WHERE private_key_kid IS NOT NULL
GROUP BY private_key_kid;

-- name: ListActorKeysUnderKey :many
SELECT id, private_key_enc
FROM actors
WHERE private_key_kid = sqlc.arg(key_id)::text
ORDER BY id
LIMIT sqlc.arg(batch);

-- name: ReplaceActorPrivateKey :execrows
UPDATE actors SET private_key_enc = sqlc.arg(sealed), private_key_kid = sqlc.arg(new_key_id)::text
WHERE id = sqlc.arg(id) AND private_key_kid = sqlc.arg(old_key_id)::text;

-- name: CountTOTPSecretsByKeyID :many
SELECT key_id, count(*) AS n
FROM totp_factors
GROUP BY key_id;

-- name: ListTOTPSecretsUnderKey :many
SELECT account_id, secret_encrypted
FROM totp_factors
WHERE key_id = sqlc.arg(key_id)
ORDER BY account_id
LIMIT sqlc.arg(batch);

-- name: ReplaceTOTPSecret :execrows
UPDATE totp_factors SET secret_encrypted = sqlc.arg(sealed), key_id = sqlc.arg(new_key_id)
WHERE account_id = sqlc.arg(account_id) AND key_id = sqlc.arg(old_key_id);

-- name: ListAbuseImageryEvidence :many
-- Every case's evidence file. Evidence records no key in the database: its file does.
SELECT id, media_id, evidence_key
FROM abuse_imagery_cases
ORDER BY created_at;

-- name: AbuseImageryCaseExists :one
SELECT EXISTS (SELECT 1 FROM abuse_imagery_cases WHERE id = $1);
