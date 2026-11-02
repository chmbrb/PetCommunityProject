-- name: GetAccountForEmail :one
SELECT a.id, a.email, a.email_verified_at, a.status, ac.display_name
FROM accounts a
JOIN actors ac ON ac.account_id = a.id
WHERE a.id = $1;

-- name: GetAccountByEmail :one
SELECT id, email, email_verified_at, status
FROM accounts
WHERE email = $1;

-- name: CreateEmailToken :exec
INSERT INTO email_tokens (id, token_hash, code_hash, account_id, purpose, email, signup, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ConsumeEmailToken :one
-- Marks an unused, unexpired token as used and returns it, so each works only once.
UPDATE email_tokens
SET used_at = now()
WHERE token_hash = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > now()
RETURNING account_id, email, signup;

-- name: ConsumeEmailCode :one
-- Like ConsumeEmailToken, for the code in the email. Only the newest token sent to
-- the address accepts codes, and only until it has had too many wrong attempts, so
-- older emails' codes can't be guessed once a newer one is used up.
WITH latest AS (
    SELECT e.id FROM email_tokens e
    WHERE e.email = sqlc.arg(email) AND e.purpose = sqlc.arg(purpose) AND e.code_hash IS NOT NULL
    ORDER BY e.id DESC
    LIMIT 1
)
UPDATE email_tokens t
SET used_at = now()
FROM latest
WHERE t.id = latest.id
  AND t.code_hash = sqlc.arg(code_hash)
  AND t.used_at IS NULL AND t.expires_at > now()
  AND t.attempts < sqlc.arg(max_attempts)::integer
RETURNING t.account_id, t.email, t.signup;

-- name: RecordEmailCodeFailure :exec
-- Counts a wrong code against the newest token sent to the address, and uses the
-- token up (link included) when it reaches the limit.
WITH latest AS (
    SELECT e.id FROM email_tokens e
    WHERE e.email = sqlc.arg(email) AND e.purpose = sqlc.arg(purpose) AND e.code_hash IS NOT NULL
    ORDER BY e.id DESC
    LIMIT 1
)
UPDATE email_tokens t
SET attempts = t.attempts + 1,
    used_at = CASE WHEN t.attempts + 1 >= sqlc.arg(max_attempts)::integer THEN now() ELSE t.used_at END
FROM latest
WHERE t.id = latest.id AND t.used_at IS NULL;

-- name: MarkEmailVerified :execrows
-- Only verifies the address the token was sent to, if it's still the account's email.
UPDATE accounts
SET email_verified_at = now()
WHERE id = $1 AND email = $2 AND email_verified_at IS NULL;

-- name: DeleteExpiredEmailTokens :execrows
-- Keeps used and expired tokens for a week for troubleshooting, then removes them.
DELETE FROM email_tokens WHERE expires_at < now() - interval '7 days';
