-- SPDX-License-Identifier: AGPL-3.0-or-later

-- +goose Up
-- An upload that matched known abuse imagery (ADR 0022). The original is kept,
-- encrypted, in the evidence bucket until the legally required report is filed and
-- the preservation period has passed (18 U.S.C. § 2258A: at least one year after
-- the report). The RESTRICT references keep the upload, its owner, and their
-- account from being deleted while the case exists.
CREATE TABLE abuse_imagery_cases (
    id               uuid PRIMARY KEY,
    media_id         uuid NOT NULL UNIQUE REFERENCES media_assets (id) ON DELETE RESTRICT,
    owner_actor_id   uuid NOT NULL REFERENCES actors (id) ON DELETE RESTRICT,
    account_id       uuid REFERENCES accounts (id) ON DELETE RESTRICT,
    -- Which scanner matched, and its reference for the match (never image content).
    scanner          text NOT NULL,
    match_reference  text,
    -- SHA-256 of the original upload, and where its encrypted copy is stored.
    sha256           bytea NOT NULL CHECK (length(sha256) = 32),
    evidence_key     text NOT NULL,
    status           text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'reported', 'closed')),
    -- The report's reference (e.g. the NCMEC CyberTipline report ID).
    report_reference text,
    reported_at      timestamptz,
    preserve_until   timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    CHECK (status <> 'reported' OR (reported_at IS NOT NULL AND preserve_until >= reported_at + interval '1 year'))
);
CREATE INDEX abuse_imagery_cases_open_idx ON abuse_imagery_cases (created_at) WHERE status = 'open';

-- Who did what, when (PLAN.md Data Retention). Append-only: the app's code never
-- updates or deletes rows. A null account_id means the system did it.
CREATE TABLE audit_log (
    id          uuid PRIMARY KEY,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    account_id  uuid REFERENCES accounts (id) ON DELETE SET NULL,
    action      text NOT NULL,
    target_type text NOT NULL,
    target_id   uuid NOT NULL,
    details     jsonb NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX audit_log_target_idx ON audit_log (target_type, target_id);

-- +goose Down
DROP TABLE audit_log;
DROP TABLE abuse_imagery_cases;
