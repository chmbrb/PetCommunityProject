# 0010. S3-compatible object storage for media

- **Status:** Accepted (MinIO for local development) / Proposed (production self-hosted option)
- **Date:** 2026-10-29
- **Related:** [0009](0009-docker-compose-deployment.md), [0011](0011-media-processing-pipeline.md), [0016](0016-backups-wal-g.md)

## Context

Pet photos are the largest data on an instance and grow steadily. Storing them in Postgres bloats backups; storing them on the local filesystem ties the instance to one disk and complicates backups and migration.

## Decision

Store media in S3-compatible object storage, accessed through a small storage interface in the Go code:

- **Local development:** **MinIO** in the development compose file (`compose.dev.yaml`), so development uses the same S3 API as production.
- **Production:** a **hosted provider** (e.g. Backblaze B2, Cloudflare R2, Wasabi), recommended for low cost and durability, or a **maintained self-hosted S3-compatible server** for fully self-contained instances. Candidates to evaluate: Garage, SeaweedFS.

MinIO isn't recommended for production: MinIO stopped publishing community Docker images in October 2025 and archived the community repository in early 2026, so it no longer receives regular security fixes. That's acceptable for local development only. Pin the development image (or use a maintained build, such as Chainguard's) and revisit if a well-supported fork emerges.

Buckets:

- `quarantine`: raw uploads, private, deleted automatically after processing or within 24 hours
- `media`: processed images only, served publicly through the media hostname

Postgres stores metadata only (`MediaAsset`). Integration tests run against MinIO; unit tests use an in-memory implementation of the storage interface.

## Consequences

- Cheap, durable storage, separate from database backups. Media isn't part of the backup process ([0016](0016-backups-wal-g.md)); the hosted provider's durability covers hardware loss.
- Durability doesn't protect against deletion. Enable the provider's object versioning or soft-delete (with a short expiry, e.g. 30 days) on the `media` bucket, so files deleted by mistake, by a bug, or with stolen app credentials can be recovered. This is a bucket setting, not a separate backup.
- The app's storage credentials can't change bucket versioning or lifecycle settings.
- Raw uploads are never publicly reachable.
- Another credential to manage, and egress costs depend on the provider. Recommend providers with free or cheap egress.

## Alternatives considered

- **MinIO in production:** the community edition is no longer maintained (see above).
- **Local filesystem only:** simplest, but ties the instance to one host and needs separate backup handling.
- **Postgres large objects / bytea:** bloats the database and backups.
