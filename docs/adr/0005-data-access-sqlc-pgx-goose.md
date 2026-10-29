# 0005. Data access with sqlc, pgx, and goose

- **Status:** Proposed
- **Date:** 2026-10-29
- **Related:** [0004](0004-postgresql-primary-datastore.md), THREAT_MODEL.md T2

## Context

The backend needs a safe, readable way to query Postgres (including PostGIS and full-text search, which ORMs handle poorly) and a way to evolve the schema over time.

## Decision

- **Driver:** `pgx` (v5) with its connection pool.
- **Queries:** write SQL in `.sql` files and generate type-safe Go code with `sqlc`. No string-built SQL anywhere (threat T2).
- **Migrations:** `goose`, with numbered SQL migration files, run by the binary's `migrate` mode at deploy time.

## Consequences

- Queries are plain SQL, so PostGIS and full-text search work without fighting an ORM.
- Compile-time checking of query parameters and result types.
- Dynamic queries (e.g. search with optional filters) need care: use `sqlc` with nullable parameters (`sqlc.narg`) or a small, reviewed query builder for that one case.
- Migrations must be forward-compatible with the running version during upgrades, and reviewed.

## Alternatives considered

- **GORM / ent:** faster CRUD, but awkward for spatial and search queries and hides the SQL.
- **database/sql + hand-written scanning:** no generator, but lots of boilerplate and no type checking.
- **Atlas / golang-migrate:** both fine; goose is simple and supports Go-coded migrations if ever needed.
