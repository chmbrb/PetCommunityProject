# 0006. Postgres-backed job queue (River)

- **Status:** Accepted
- **Date:** 2026-10-29
- **Related:** [0004](0004-postgresql-primary-datastore.md), [0011](0011-media-processing-pipeline.md), THREAT_MODEL.md D1

## Context

Several tasks must run outside the request path: image processing, emails and notifications, listing expiry, imports from shelter software, data retention cleanup, and later federation delivery with retries.

## Decision

Use [River](https://riverqueue.com) (Go, Postgres-backed) for background jobs. Jobs are inserted in the same transaction as the data change that triggers them. The `worker` mode of the binary runs job consumers, with per-queue concurrency limits (e.g. `media`, `email`, `federation`, `maintenance`).

## Consequences

- No extra broker: jobs are backed up with the database and can't be lost between a commit and an enqueue.
- Retries, scheduling, and unique jobs are built in.
- Throughput is limited by Postgres, which is fine at instance scale. Federation fan-out in Phase 5 needs monitoring.
- Pin the River version and follow its migration process when upgrading.

## Alternatives considered

- **Redis-based queues (asynq):** need Redis.
- **Hand-rolled `SELECT ... FOR UPDATE SKIP LOCKED`:** simple to start, but we'd end up rebuilding retries and scheduling.
- **Separate broker (NATS, RabbitMQ):** unnecessary at this scale.
