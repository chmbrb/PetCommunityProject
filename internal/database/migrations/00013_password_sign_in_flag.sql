-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Lets operators pause password sign-ins during a wave of account takeovers
-- (INCIDENT_RESPONSE.md). Email codes and passkeys keep working while it's off.
INSERT INTO instance_feature_flags (flag, enabled) VALUES ('password_sign_in', true)
ON CONFLICT (flag) DO NOTHING;

-- +goose Down
DELETE FROM instance_feature_flags WHERE flag = 'password_sign_in';
