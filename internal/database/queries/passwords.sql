-- name: GetPasswordLogin :one
-- The account's password state, locked so concurrent attempts count correctly.
SELECT id, email, status, password_hash, failed_password_attempts, password_locked_until
FROM accounts
WHERE email = $1
FOR UPDATE;

-- name: RecordPasswordFailure :one
-- Counts a wrong password, and starts a lockout when the limit is reached.
UPDATE accounts
SET failed_password_attempts = CASE
        WHEN failed_password_attempts + 1 >= sqlc.arg(max_attempts)::integer THEN 0
        ELSE failed_password_attempts + 1
    END,
    password_locked_until = CASE
        WHEN failed_password_attempts + 1 >= sqlc.arg(max_attempts)::integer THEN now() + make_interval(secs => sqlc.arg(lockout_seconds)::integer)
        ELSE password_locked_until
    END
WHERE id = sqlc.arg(id)
RETURNING password_locked_until;

-- name: ResetPasswordFailures :exec
UPDATE accounts SET failed_password_attempts = 0, password_locked_until = NULL WHERE id = $1;

-- name: SetPasswordHash :exec
-- Sets, changes, or (with NULL) removes the password. Clears any lockout.
UPDATE accounts
SET password_hash = $2,
    password_changed_at = CASE WHEN $2::text IS NULL THEN NULL ELSE now() END,
    failed_password_attempts = 0,
    password_locked_until = NULL
WHERE id = $1;

-- name: RehashPassword :exec
-- Replaces the hash with one using current parameters; the password is unchanged.
UPDATE accounts SET password_hash = $2 WHERE id = $1;

-- name: GetPasswordState :one
SELECT (a.password_hash IS NOT NULL)::boolean AS has_password, a.password_changed_at, a.email, ac.handle
FROM accounts a
JOIN actors ac ON ac.account_id = a.id
WHERE a.id = $1;

-- name: KnownDeviceExists :one
SELECT EXISTS (
    SELECT 1 FROM known_devices
    WHERE account_id = $1 AND token_hash = $2 AND expires_at > now()
);

-- name: RememberDevice :exec
-- Records that a device signed in to the account, or extends it if already known.
INSERT INTO known_devices (id, account_id, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (account_id, token_hash)
DO UPDATE SET last_used_at = now(), expires_at = EXCLUDED.expires_at;

-- name: DeleteExpiredKnownDevices :execrows
DELETE FROM known_devices WHERE expires_at <= now();
