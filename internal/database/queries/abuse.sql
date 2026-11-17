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

-- name: ListAbuseImageryCases :many
-- For operators (petcommunity cases list); all statuses when status is null.
SELECT c.id, c.status, c.scanner, c.created_at, c.reported_at, c.preserve_until, a.handle
FROM abuse_imagery_cases c
JOIN actors a ON a.id = c.owner_actor_id
WHERE sqlc.narg(status)::text IS NULL OR c.status = sqlc.narg(status)::text
ORDER BY c.created_at;

-- name: GetAbuseImageryCase :one
-- Everything a report asks about the case, the uploader, and the upload.
SELECT c.id, c.media_id, c.owner_actor_id, c.account_id, c.scanner, c.match_reference,
       c.sha256, c.evidence_key, c.status, c.report_reference, c.reported_at, c.preserve_until,
       c.created_at,
       a.handle, a.display_name,
       acc.email AS account_email, acc.status AS account_status, acc.created_at AS account_created_at,
       m.created_at AS uploaded_at, m.purpose AS upload_purpose, m.upload_bytes
FROM abuse_imagery_cases c
JOIN actors a ON a.id = c.owner_actor_id
JOIN media_assets m ON m.id = c.media_id
LEFT JOIN accounts acc ON acc.id = c.account_id
WHERE c.id = $1;

-- name: MarkAbuseImageryCaseReported :execrows
-- Only an open case can be reported; the table checks preserve_until.
UPDATE abuse_imagery_cases
SET status = 'reported', report_reference = $2, reported_at = $3, preserve_until = $4
WHERE id = $1 AND status = 'open';

-- name: CloseAbuseImageryCase :execrows
-- Closing keeps a reported case's preserve_until; evidence is only purged after it.
UPDATE abuse_imagery_cases SET status = 'closed'
WHERE id = $1 AND status IN ('open', 'reported');
