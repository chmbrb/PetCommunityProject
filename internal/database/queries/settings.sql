-- name: GetAccountWebAuthn :one
SELECT a.webauthn_user_handle, ac.handle, ac.display_name
FROM accounts a
JOIN actors ac ON ac.account_id = a.id
WHERE a.id = $1;

-- name: ListAccountPasskeys :many
SELECT ai.id, pc.name, pc.credential, ai.created_at, ai.last_used_at
FROM auth_identities ai
JOIN passkey_credentials pc ON pc.identity_id = ai.id
WHERE ai.account_id = $1
ORDER BY ai.created_at;

-- name: CountAccountPasskeys :one
-- Locks the account's passkeys, so two concurrent removals can't both leave it with none.
SELECT count(*)
FROM (
    SELECT 1 FROM auth_identities
    WHERE account_id = $1 AND kind = 'passkey'
    FOR UPDATE
) AS passkeys;

-- name: RenamePasskey :execrows
UPDATE passkey_credentials pc
SET name = $3
FROM auth_identities ai
WHERE pc.identity_id = ai.id AND ai.id = $1 AND ai.account_id = $2;

-- name: GetAccountPasskeyName :one
SELECT pc.name
FROM auth_identities ai
JOIN passkey_credentials pc ON pc.identity_id = ai.id
WHERE ai.id = $1 AND ai.account_id = $2;

-- name: DeletePasskey :execrows
-- Deletes one of the account's passkeys; its credential row cascades.
DELETE FROM auth_identities
WHERE id = $1 AND account_id = $2 AND kind = 'passkey';

-- name: ListAccountSessions :many
SELECT id, client, user_agent, created_at, last_seen_at
FROM sessions
WHERE account_id = $1 AND revoked_at IS NULL AND expires_at > now()
ORDER BY last_seen_at DESC;

-- name: RevokeAccountSession :execrows
UPDATE sessions SET revoked_at = now()
WHERE id = $1 AND account_id = $2 AND revoked_at IS NULL;

-- name: RevokeOtherAccountSessions :execrows
UPDATE sessions SET revoked_at = now()
WHERE account_id = $1 AND id <> $2 AND revoked_at IS NULL;
