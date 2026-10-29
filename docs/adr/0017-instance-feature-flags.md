# 0017. Instance-level feature flags

- **Status:** Accepted
- **Date:** 2026-10-29
- **Related:** PLAN.md decision #6, Phases 4–7; [0015](0015-central-authorization-policy.md)

## Context

Instances differ in legal situation, insurance, and capacity. Some features carry real liability (pet sitting, breeder listings) or need moderation capacity an operator may not have (federation, open signups). Operators must be able to turn these on deliberately.

## Decision

Features are controlled by instance-level flags stored in the database and editable by instance admins:

| Flag | Default | Notes |
|------|---------|-------|
| `open_registration` | on | Off = invite or approval only |
| `rehoming` | on | Individuals rehoming their own pets |
| `communities` | on (from Phase 3) | |
| `fostering` | off (Phase 4) | |
| `federation` | off (Phase 5) | Plus allow-list-only mode when first enabled |
| `breeder_listings` | **off** (Phase 6) | Enabling requires confirming a legal review |
| `pet_sitting` | **off** (Phase 7) | Enabling requires confirming legal review and insurance |

- Flags are checked in the authorization layer ([0015](0015-central-authorization-policy.md)) and hide the corresponding UI.
- Enabling a flag marked as high-liability shows the operator a checklist and records the confirmation in the audit log.
- Flags are not per-user experiments or A/B tests; they're operator policy.

## Consequences

- Each instance can match the platform to its legal and operational situation.
- Every flag adds code paths to test; keep the list short and remove flags for features that become universal.
- Data from a disabled feature is kept but hidden, so turning a flag off doesn't delete anything.

## Alternatives considered

- **Config file / environment variables:** requires a restart and isn't visible in the admin UI or audit log. Could still set the initial defaults.
- **Separate builds per feature set:** much harder to maintain.
