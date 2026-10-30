-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- Instance-level feature flags (ADR 0017). Operator policy, not per-user experiments.
CREATE TABLE instance_feature_flags (
    flag       text PRIMARY KEY CHECK (flag ~ '^[a-z][a-z0-9_]*$'),
    enabled    boolean NOT NULL,
    config     jsonb NOT NULL DEFAULT '{}'::jsonb,
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- Defaults from ADR 0017. High-liability features start disabled.
INSERT INTO instance_feature_flags (flag, enabled, config) VALUES
    ('open_registration', true,  '{}'),
    ('rehoming',          true,  '{}'),
    ('communities',       true,  '{}'),
    ('fostering',         false, '{}'),
    ('federation',        false, '{"mode": "allow_list"}'),
    ('breeder_listings',  false, '{}'),
    ('pet_sitting',       false, '{}'),
    ('sso_providers',     false, '{"providers": []}');

-- +goose Down
DROP TABLE instance_feature_flags;
