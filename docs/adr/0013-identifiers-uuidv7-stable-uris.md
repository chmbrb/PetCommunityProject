# 0013. UUIDv7 identifiers and stable URIs

- **Status:** Accepted
- **Date:** 2026-10-29
- **Related:** [0012](0012-activitypub-shaped-model-federation-deferred.md), DOMAIN_MODEL.md, THREAT_MODEL.md T1

## Context

Every entity needs an ID that can appear in URLs and, later, in federated URIs that other servers store permanently. Sequential integer IDs reveal how many records exist and make guessing other records' URLs trivial (which makes broken authorization easier to exploit).

## Decision

- Primary keys are **UUIDv7** (time-ordered, so index-friendly), generated in Go.
- Public URIs use the ID and never change: `https://{domain}/actors/{id}`, `/listings/{id}`, `/posts/{id}`, `/communities/{id}`. Human-friendly paths (e.g. `/@handle`, `/listings/{id}/{slug}`) redirect to or alias the canonical URI.
- Handles can change; actor URIs can't.

## Consequences

- IDs reveal creation time, which is acceptable for public objects. For private records (applications), time-ordering is harmless since they're never publicly addressable.
- Unguessable IDs reduce, but don't replace, authorization checks ([0015](0015-central-authorization-policy.md)).
- 16-byte keys are slightly larger than integers. Negligible at this scale.

## Alternatives considered

- **Bigint sequences:** compact, but enumerable.
- **UUIDv4:** random insert order hurts index locality.
- **ULID / Snowflake:** similar benefits, but UUIDv7 is a standard with native Postgres support.
