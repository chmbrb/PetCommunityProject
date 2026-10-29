# 0015. Central authorization policy layer

- **Status:** Accepted
- **Date:** 2026-10-29
- **Related:** THREAT_MODEL.md T1, E1, P3; PLAN.md (Trust tiers, Governance)

## Context

Permissions depend on several things at once: trust tier, organization role, community role, resource ownership, account status (restricted/suspended), and instance feature flags. Scattering these checks across handlers is the most common way applications leak data (threat T1).

## Decision

All authorization goes through one Go package (`authz`) with a single entry point, roughly:

```go
authz.Can(ctx, principal, action, resource) error
```

- **Principal:** account, its actor, trust tier, MFA status, and memberships (loaded once per request).
- **Action:** a named constant (e.g. `ListingCreate`, `ApplicationView`, `CommunityModerate`).
- **Resource:** the loaded object, or a type plus parent for create actions.
- Rules are plain Go functions, one file per resource type, and deny by default.
- Every handler calls `authz.Can` before doing work. A test lists all routes and fails if a handler doesn't.
- Queries for lists also filter by permission in SQL (e.g. applications visible to an org's members), with tests that compare the SQL filter and the policy.
- Admin access to private data (applications, verification evidence) requires a reason and is written to the audit log.

A table-driven test suite covers every role × action × resource combination.

## Consequences

- One place to review and test permissions; security review is practical.
- Rules are code, so changes go through code review.
- Some duplication between the policy and SQL list filters, managed by tests.

## Alternatives considered

- **Policy engine (OPA/Rego, Casbin, Cedar):** powerful, but another language and runtime to learn for a small team.
- **Database row-level security only:** strong guarantee, but hard to express tiers, flags, and MFA requirements, and hard to test. Could be added later as an extra layer for the most sensitive tables.
