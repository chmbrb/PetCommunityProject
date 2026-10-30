-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- PostGIS for location search; pg_trgm for breed matching and handle similarity (ADR 0004).
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- +goose Down
DROP EXTENSION IF EXISTS pg_trgm;
DROP EXTENSION IF EXISTS postgis;
