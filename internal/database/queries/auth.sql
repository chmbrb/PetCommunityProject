-- name: FeatureFlagEnabled :one
SELECT enabled FROM instance_feature_flags WHERE flag = $1;

-- name: EmailExists :one
SELECT EXISTS (SELECT 1 FROM accounts WHERE email = $1);

-- name: HandleExists :one
SELECT EXISTS (SELECT 1 FROM actors WHERE handle = $1);

-- name: CreateAccount :exec
INSERT INTO accounts (id, email, webauthn_user_handle)
VALUES ($1, $2, $3);

-- name: CreatePersonActor :exec
INSERT INTO actors (id, type, handle, display_name, account_id)
VALUES ($1, 'person', $2, $3, $4);

-- name: CreateAuthIdentity :exec
INSERT INTO auth_identities (id, account_id, kind, last_used_at)
VALUES ($1, $2, $3, now());

-- name: CreatePasskeyCredential :exec
INSERT INTO passkey_credentials (identity_id, credential_id, credential, name)
VALUES ($1, $2, $3, $4);

-- name: GetAccountByWebAuthnHandle :one
SELECT id, email, webauthn_user_handle, status
FROM accounts
WHERE webauthn_user_handle = $1;

-- name: ListPasskeysForAccount :many
SELECT pc.identity_id, pc.credential_id, pc.credential
FROM passkey_credentials pc
JOIN auth_identities ai ON ai.id = pc.identity_id
WHERE ai.account_id = $1;

-- name: UpdatePasskeyAfterLogin :exec
UPDATE passkey_credentials SET credential = $2 WHERE identity_id = $1;

-- name: TouchAuthIdentity :exec
UPDATE auth_identities SET last_used_at = now() WHERE id = $1;

-- name: CreateWebAuthnChallenge :exec
INSERT INTO webauthn_challenges (id, purpose, session, signup, account_id, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: TakeWebAuthnChallenge :one
-- Deletes and returns an unexpired challenge, so each one can be used only once.
DELETE FROM webauthn_challenges
WHERE id = $1 AND purpose = $2 AND expires_at > now()
RETURNING session, signup, account_id;

-- name: DeleteExpiredWebAuthnChallenges :execrows
DELETE FROM webauthn_challenges WHERE expires_at <= now();

-- name: CreateSession :exec
INSERT INTO sessions (id, token_hash, account_id, client, user_agent, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetActiveSession :one
-- Returns the session and its account if the session is valid and the account may sign in.
SELECT s.id, s.account_id, s.client, s.created_at, s.last_seen_at, a.status AS account_status
FROM sessions s
JOIN accounts a ON a.id = s.account_id
WHERE s.token_hash = $1
  AND s.revoked_at IS NULL
  AND s.expires_at > now()
  AND a.status IN ('active', 'restricted');

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = now() WHERE id = $1;

-- name: TouchAccount :exec
UPDATE accounts SET last_active_at = now() WHERE id = $1;

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL;

-- name: GetMe :one
SELECT a.id AS account_id, a.email, a.email_verified_at, a.status,
       ac.id AS actor_id, ac.handle, ac.display_name
FROM accounts a
JOIN actors ac ON ac.account_id = a.id
WHERE a.id = $1;
