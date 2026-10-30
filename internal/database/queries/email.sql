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
INSERT INTO email_tokens (id, token_hash, account_id, purpose, email, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ConsumeEmailToken :one
-- Marks an unused, unexpired token as used and returns it, so each works only once.
UPDATE email_tokens
SET used_at = now()
WHERE token_hash = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > now()
RETURNING account_id, email;

-- name: MarkEmailVerified :execrows
-- Only verifies the address the token was sent to, if it's still the account's email.
UPDATE accounts
SET email_verified_at = now()
WHERE id = $1 AND email = $2 AND email_verified_at IS NULL;

-- name: DeleteExpiredEmailTokens :execrows
-- Keeps used and expired tokens for a week for troubleshooting, then removes them.
DELETE FROM email_tokens WHERE expires_at < now() - interval '7 days';
