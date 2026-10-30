-- name: ListFeatureFlags :many
SELECT flag, enabled
FROM instance_feature_flags
ORDER BY flag;
