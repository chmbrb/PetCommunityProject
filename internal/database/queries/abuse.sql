-- name: CreateAbuseImageryCase :execrows
-- One case per upload: a retried job finds the case already open.
INSERT INTO abuse_imagery_cases (id, media_id, owner_actor_id, account_id, scanner, match_reference, sha256, evidence_key)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (media_id) DO NOTHING;

-- name: GetActorAccountID :one
SELECT account_id FROM actors WHERE id = $1;

-- name: SuspendAccount :execrows
UPDATE accounts SET status = 'suspended'
WHERE id = $1 AND status IN ('active', 'restricted');

-- name: RevokeAllAccountSessions :execrows
UPDATE sessions SET revoked_at = now()
WHERE account_id = $1 AND revoked_at IS NULL;

-- name: InsertAuditEvent :exec
INSERT INTO audit_log (id, account_id, action, target_type, target_id, details)
VALUES ($1, $2, $3, $4, $5, $6);
