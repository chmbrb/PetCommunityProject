-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- When a case was closed (ADR 0022). The purge job deletes a closed case's evidence
-- only some time after it was closed, and never before preserve_until, so a case
-- closed by mistake isn't purged within minutes. Cases closed before this column
-- existed count as closed now.
ALTER TABLE abuse_imagery_cases ADD COLUMN closed_at timestamptz;
UPDATE abuse_imagery_cases SET closed_at = now() WHERE status = 'closed';
ALTER TABLE abuse_imagery_cases
    ADD CONSTRAINT abuse_imagery_cases_closed_at_check CHECK ((status = 'closed') = (closed_at IS NOT NULL));
CREATE INDEX abuse_imagery_cases_closed_idx ON abuse_imagery_cases (closed_at) WHERE status = 'closed';

-- +goose Down
ALTER TABLE abuse_imagery_cases DROP COLUMN closed_at;
