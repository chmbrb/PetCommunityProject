# 0010. S3-compatible object storage for media

- **Status:** Proposed
- **Date:** 2026-10-29
- **Related:** [0009](0009-docker-compose-deployment.md), [0011](0011-media-processing-pipeline.md), [0016](0016-backups-wal-g.md)

## Context

Pet photos are the largest data on an instance and grow steadily. Storing them in Postgres bloats backups; storing them on the local filesystem ties the instance to one disk and complicates backups and migration.

## Decision

Store media in S3-compatible object storage, accessed through a small storage interface in the Go code:

- **Hosted provider** (e.g. Backblaze B2, Cloudflare R2, Wasabi) recommended for low cost and durability, or
- **MinIO** in the compose stack for fully self-contained instances.

Buckets:

- `quarantine`: raw uploads, private, deleted automatically after processing or within 24 hours
- `media`: processed images only, served publicly through the media hostname

Postgres stores metadata only (`MediaAsset`). A local-filesystem implementation of the storage interface exists for development and tests.

## Consequences

- Cheap, durable storage, separate from database backups. Media backup is handled by provider replication or versioning.
- Raw uploads are never publicly reachable.
- Another credential to manage, and egress costs depend on the provider. Recommend providers with free or cheap egress.

## Alternatives considered

- **Local filesystem only:** simplest, but ties the instance to one host and needs separate backup handling.
- **Postgres large objects / bytea:** bloats the database and backups.
