-- name: ListFeatureFlags :many
SELECT flag, enabled
FROM instance_feature_flags
ORDER BY flag;

-- name: SetFeatureFlag :exec
UPDATE instance_feature_flags SET enabled = $2, updated_at = now() WHERE flag = $1;
