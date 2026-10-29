# 0004. PostgreSQL as the primary datastore

- **Status:** Accepted
- **Date:** 2026-10-29
- **Related:** [0005](0005-data-access-sqlc-pgx-goose.md), [0006](0006-postgres-job-queue-river.md), [0016](0016-backups-wal-g.md), DOMAIN_MODEL.md

## Context

The platform needs relational data with strong integrity (applications, memberships, moderation), location search ("pets within 25 km"), text search (breed, description), and background jobs. Every extra service adds hosting cost, containers to operate, and things to back up.

## Decision

Use PostgreSQL (latest stable major version at Phase 1 start) for as much as possible:

- **Relational data**, with foreign keys and check constraints enforcing the domain model's rules.
- **PostGIS** for location: `geography(Point)` columns and GiST indexes for radius search.
- **Built-in full-text search** (`tsvector` + GIN) for listings and posts, plus `pg_trgm` for breed name matching and handle similarity checks (impersonation, threat A3).
- **JSONB** for loosely structured data (custom application questions, AP activity payloads).
- **The job queue** (see [0006](0006-postgres-job-queue-river.md)).
- **Sessions** (see [0014](0014-authentication-sessions-mfa.md)).

No Redis, Elasticsearch, or separate queue broker in the MVP.

## Consequences

- One stateful service to run, monitor, and back up. Transactions cover both data and jobs.
- Postgres search is good enough for instance-sized data, but has no advanced relevance tuning. Revisit only if real usage demands it.
- The Postgres image must include PostGIS (e.g. the `postgis/postgis` image).

## Alternatives considered

- **Postgres + Elasticsearch/Meilisearch:** better search relevance, but another service and another sync path.
- **SQLite:** even leaner, but weaker for concurrent writes and no PostGIS. A single-file option could be revisited for very small instances.
- **Redis for sessions and queues:** faster, but another stateful service to back up.
