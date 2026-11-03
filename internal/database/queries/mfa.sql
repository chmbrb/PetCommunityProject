-- name: GetTOTPFactor :one
SELECT account_id, secret_encrypted, key_id, last_used_step, confirmed_at
FROM totp_factors
WHERE account_id = $1
FOR UPDATE;

-- name: HasConfirmedTOTP :one
SELECT EXISTS (SELECT 1 FROM totp_factors WHERE account_id = $1 AND confirmed_at IS NOT NULL);

-- name: StartTOTPSetup :execrows
-- Stores a new, unconfirmed secret, replacing an earlier unfinished setup. Does
-- nothing if the account already has a confirmed authenticator.
INSERT INTO totp_factors (account_id, secret_encrypted, key_id)
VALUES ($1, $2, $3)
ON CONFLICT (account_id) DO UPDATE
SET secret_encrypted = EXCLUDED.secret_encrypted, key_id = EXCLUDED.key_id,
    last_used_step = 0, created_at = now()
WHERE totp_factors.confirmed_at IS NULL;

-- name: ConfirmTOTP :exec
UPDATE totp_factors
SET confirmed_at = now(), last_used_step = $2, last_used_at = now()
WHERE account_id = $1;

-- name: UseTOTPStep :exec
UPDATE totp_factors SET last_used_step = $2, last_used_at = now() WHERE account_id = $1;

-- name: DeleteTOTPFactor :execrows
DELETE FROM totp_factors WHERE account_id = $1;

-- name: DeleteRecoveryCodes :exec
DELETE FROM recovery_codes WHERE account_id = $1;

-- name: CreateRecoveryCode :exec
INSERT INTO recovery_codes (id, account_id, code_hash) VALUES ($1, $2, $3);

-- name: UseRecoveryCode :execrows
UPDATE recovery_codes SET used_at = now()
WHERE account_id = $1 AND code_hash = $2 AND used_at IS NULL;

-- name: CountRecoveryCodesLeft :one
SELECT count(*) FROM recovery_codes WHERE account_id = $1 AND used_at IS NULL;

-- name: CreateMFAChallenge :exec
INSERT INTO mfa_challenges (id, token_hash, account_id, client, expires_at)
VALUES ($1, $2, $3, $4, $5);

-- name: GetMFAChallenge :one
-- Locked, so concurrent attempts are counted one at a time.
SELECT id, account_id, client, attempts
FROM mfa_challenges
WHERE token_hash = $1 AND expires_at > now()
FOR UPDATE;

-- name: RecordMFAFailure :one
-- Counts a wrong code and returns the total.
UPDATE mfa_challenges SET attempts = attempts + 1 WHERE id = $1 RETURNING attempts;

-- name: DeleteMFAChallenge :exec
DELETE FROM mfa_challenges WHERE id = $1;

-- name: DeleteExpiredMFAChallenges :execrows
DELETE FROM mfa_challenges WHERE expires_at <= now();
