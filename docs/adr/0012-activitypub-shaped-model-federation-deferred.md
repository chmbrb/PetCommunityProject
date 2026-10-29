# 0012. ActivityPub-shaped data model, federation deferred

- **Status:** Accepted
- **Date:** 2026-10-29
- **Related:** PLAN.md decision #2 and Phase 5, DOMAIN_MODEL.md, [0013](0013-identifiers-uuidv7-stable-uris.md)

## Context

The project's vision is federated, but federation brings its own moderation, security, and privacy problems (THREAT_MODEL.md F1–F5). Moderation and trust need to be proven on a single instance first. Adding federation later to a model that wasn't designed for it usually means a painful rewrite.

## Decision

From Phase 1, the data model follows ActivityPub's structure, but no federation endpoints are exposed until Phase 5:

- Accounts, organizations, and communities are **Actors** (`Person`, `Organization`, `Group`), each with a stable URI, inbox/outbox URIs, and a key pair generated at creation (private key encrypted at rest).
- State changes are recorded in an **activity log** in AP-compatible JSON (Create, Update, Delete, Follow, Flag, …).
- Deleted objects become **tombstones**.
- Communities follow the FEP-1b12 Group model (the group `Announce`s member posts).
- Pet-specific listing fields live under a project JSON-LD namespace, to be defined in Phase 5.
- Private data (applications, adopter profiles, verification evidence, moderation internals) is modeled so it can't be federated by accident: those entities are never Activities.

## Consequences

- Phase 5 adds endpoints (WebFinger, inbox, outbox, HTTP signatures) and delivery jobs, rather than reworking the schema.
- Small upfront cost: keys and URIs for every actor, and an activity log that isn't consumed externally yet.
- Domain names matter from day one: actor URIs include the instance domain and can't change once federated. Operators must choose their final domain before launch.

## Alternatives considered

- **Federate from day one:** maximum reach, but immature moderation would be exposed to other instances' problems.
- **Ignore federation until later:** faster start, but a likely rewrite of identity and content models.
- **Build on existing fediverse software (fork GoToSocial or Lemmy):** inherits a lot, but brings a domain model not designed for listings, applications, and verification.
