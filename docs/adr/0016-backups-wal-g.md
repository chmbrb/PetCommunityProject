# 0016. Backups with WAL-G

- **Status:** Proposed
- **Date:** 2026-10-29
- **Related:** PLAN.md (Stack: Backups; Open Questions: RPO/RTO), [0004](0004-postgresql-primary-datastore.md), [0009](0009-docker-compose-deployment.md), [0010](0010-s3-compatible-media-storage.md), THREAT_MODEL.md I3, O3

## Context

The project requires backup workflows that meet stated recovery targets (RPO: how much data can be lost; RTO: how long recovery can take). Backups contain every piece of private data, so they're also a breach risk.

## Decision

- **Database:** WAL-G in a sidecar container:
  - Continuous WAL archiving to off-site S3-compatible storage, with a 60-second `archive_timeout`
  - Nightly full base backups
  - Retention of 7 daily + 4 weekly backups (proposed)
  - Point-in-time recovery
- **Encryption:** backups are encrypted client-side (libsodium key) before upload. The key is stored outside the server, and the operator is told to keep an offline copy.
- **Credentials:** the backup bucket uses separate credentials that can write but not delete, and object lock or versioning where the provider supports it (protects against ransomware and a compromised server).
- **Media:** provider replication or versioning on the `media` bucket ([0010](0010-s3-compatible-media-storage.md)).
- **Monitoring:** the app's admin dashboard and health check report the time of the last successful WAL archive and base backup. An alert email is sent if either is overdue.
- **Restore drills:** a documented `restore` procedure and a scripted drill that restores into a throwaway compose stack and runs checks. Operators are asked to run it monthly; CI runs it on every release.

**Proposed targets** (to confirm, see PLAN.md open questions): **RPO ≤ 5 minutes**, **RTO ≤ 4 hours**.

## Consequences

- Point-in-time recovery protects against mistakes (e.g. a bad migration) as well as hardware loss.
- Storage costs for WAL and base backups are small for instance-sized databases.
- Losing the encryption key means losing the backups. Onboarding must make the operator store it safely.

## Alternatives considered

- **pgBackRest:** very capable, but more configuration and heavier for single-node instances.
- **Nightly `pg_dump` only:** simple, but RPO of up to 24 hours.
- **Provider disk snapshots:** depend on the host and aren't application-consistent.
