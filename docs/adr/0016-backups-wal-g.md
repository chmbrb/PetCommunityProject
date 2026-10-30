# 0016. Backups with WAL-G

- **Status:** Accepted
- **Date:** 2026-10-29
- **Related:** PLAN.md (Stack: Backups; Open Questions: RPO/RTO), [0004](0004-postgresql-primary-datastore.md), [0009](0009-docker-compose-deployment.md), [0010](0010-s3-compatible-media-storage.md), THREAT_MODEL.md I3, O3

## Context

The project requires backup workflows that meet stated recovery targets (RPO: how much data can be lost; RTO: how long recovery can take). Backups contain every piece of private data, so they're also a breach risk.

## Decision

- **Follow the 3-2-1 rule:** three copies of the data, on two different storage media, with one copy off-site.
  1. **Live data:** the Postgres volume.
  2. **Local backup:** a dedicated backup volume, on a **different physical disk** from the database. A backup on the same disk doesn't count as a second medium. This copy gives the fastest restores.
  3. **Off-site copy:** the local backup repository is synced off-site on a schedule (e.g. with rclone). The destination is still to be decided (a hosted object storage provider, or a server run by a trusted operator).
- **Database:** WAL-G in a sidecar container, writing to the local backup volume (WAL-G's file storage):
  - Continuous WAL archiving, with a 60-second `archive_timeout`
  - Nightly full base backups
  - Retention of 7 daily + 4 weekly backups (proposed; must stay consistent with the data retention periods in PLAN.md)
  - Point-in-time recovery
- **Media is not backed up by this process.** In production, media lives in a hosted object storage bucket, which already stores data durably ([0010](0010-s3-compatible-media-storage.md)). MinIO is only used for local development and needs no backups. Operators who self-host object storage in production are responsible for backing it up themselves.
- **Encryption:** backups are encrypted client-side (libsodium key) before upload. The key is stored outside the server, and the operator is told to keep an offline copy.
- **Instance secret:** `PC_SECRET_KEY` (which encrypts actors' private keys, ADR 0012) isn't in the database, so it isn't in backups. Operators keep an offline copy alongside the backup encryption key; a restore needs both.
- **Credentials:** the off-site destination uses separate credentials that can write but not delete, and object lock or versioning where the provider supports it (protects against ransomware and a compromised server).
- **Monitoring:** the app's admin dashboard and health check report the time of the last successful WAL archive, base backup, and off-site sync. An alert email is sent if either is overdue.
- **Restore drills:** a documented `restore` procedure and a scripted drill that restores into a throwaway compose stack and runs checks. Operators are asked to run it monthly; CI runs it on every release.

**Recovery targets:** not decided yet (PLAN.md open questions). The design allows an RPO of about a minute for the local copy; the off-site RPO depends on the sync interval.

## Consequences

- Point-in-time recovery protects against mistakes (e.g. a bad migration) as well as hardware loss.
- Storage costs for WAL and base backups are small for instance-sized databases.
- Losing the encryption key means losing the backups. Onboarding must make the operator store it safely.

## Alternatives considered

- **pgBackRest:** very capable, but more configuration and heavier for single-node instances.
- **Nightly `pg_dump` only:** simple, but RPO of up to 24 hours.
- **Provider disk snapshots:** depend on the host and aren't application-consistent.
