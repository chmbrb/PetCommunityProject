-- name: FindAccounts :many
-- For operators (petcommunity accounts find): an exact handle or email address.
SELECT acc.id, acc.email, acc.status, a.handle, a.display_name, acc.created_at, acc.last_active_at
FROM accounts acc
JOIN actors a ON a.account_id = acc.id
WHERE a.handle = sqlc.arg(query)::text OR acc.email = sqlc.arg(query)::text
ORDER BY acc.created_at;

-- name: GetAccountForOperator :one
-- One account, with how it signs in and whether an abuse imagery case holds it.
SELECT acc.id, acc.email, acc.email_verified_at, acc.status, acc.created_at, acc.last_active_at,
       a.id AS actor_id, a.handle, a.display_name,
       EXISTS (SELECT 1 FROM auth_identities i WHERE i.account_id = acc.id AND i.kind = 'password') AS has_password,
       EXISTS (SELECT 1 FROM totp_factors t WHERE t.account_id = acc.id AND t.confirmed_at IS NOT NULL) AS has_totp,
       (SELECT count(*) FROM abuse_imagery_cases c WHERE c.account_id = acc.id AND c.status IN ('open', 'reported')) AS held_by_cases
FROM accounts acc
JOIN actors a ON a.account_id = acc.id
WHERE acc.id = $1;

-- name: ReinstateAccount :execrows
-- Lifts a suspension, but never while an abuse imagery case is open or reported for
-- the account: that suspension lasts until the case is closed (ADR 0022).
UPDATE accounts SET status = 'active'
WHERE accounts.id = $1 AND accounts.status = 'suspended'
  AND NOT EXISTS (
      SELECT 1 FROM abuse_imagery_cases c
      WHERE c.account_id = accounts.id AND c.status IN ('open', 'reported')
  );
