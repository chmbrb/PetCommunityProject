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

### As built (Phase 1)

The off-site copy is deferred; Phase 1 builds copies 1 and 2.

- **A separate backup container with no network** (`network_mode: none`). It reaches Postgres only through Postgres's Unix socket, shared as a volume, and reads the data files through a read-only mount. It alone holds the backup key (a Compose secret file, which WAL-G reads directly, so the key is never in any environment) and writes to the backup store.
- **The database container never runs WAL-G** and never sees the key or the store. Its `archive_command` copies each finished WAL file (via a temporary name) into a spool folder on the backup disk; the backup container encrypts and stores it within about 10 seconds. RPO is about a minute (`archive_timeout` is 60 seconds).
- **Backup folder:** `PC_BACKUP_DIR`, a host folder holding the encrypted WAL-G store (`walg/`), the WAL spool (`wal-spool/`), and a `status` file. For now it's on the VPS's own disk: that covers mistakes and bad data through point-in-time restore, but not losing the disk or the VPS. The planned second copy is a server the operator runs locally, which **pulls** the encrypted store from the VPS on a schedule (so a compromised VPS can't delete it); see DEVELOPMENT.md.
- **Schedule and retention:** a base backup when none exists, then every 24 hours; the last 7 base backups are kept, with the WAL needed to restore any point since the oldest. (Weekly retention comes with the off-site copy.)
- **Restore drills:** the backup container restores the latest backup into a scratch copy weekly (and an hour after a failed drill), checks the app's tables, and deletes the copy. `pc-backup verify` runs one on demand.
- **Monitoring:** the container's health check fails if the service loop stops, the last base backup is over 26 hours old, WAL is piling up in the spool, Postgres can't write to the spool, the last drill failed, or the backup disk is under 10% free. Alert emails and an admin dashboard come later.
- **Restore:** a separate one-off `backup-restore` service (also no network) restores the latest backup, optionally to a point in time, while the database is stopped. It moves the current data aside rather than deleting it.
- **Not yet built:** off-site sync, weekly retention, alert emails, and a CI restore drill.

## Consequences

- Point-in-time recovery protects against mistakes (e.g. a bad migration) as well as hardware loss.
- Storage costs for WAL and base backups are small for instance-sized databases.
- Losing the encryption key means losing the backups. Onboarding must make the operator store it safely.

## Alternatives considered

- **pgBackRest:** very capable, but more configuration and heavier for single-node instances.
- **Nightly `pg_dump` only:** simple, but RPO of up to 24 hours.
- **Provider disk snapshots:** depend on the host and aren't application-consistent.
