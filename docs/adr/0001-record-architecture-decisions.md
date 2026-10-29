# 0001. Record architecture decisions

- **Status:** Accepted
- **Date:** 2026-10-29

## Context

The project is self-hostable and federated, and instance operators and future contributors will need to understand why the stack looks the way it does. Decisions made in conversation are easily lost.

## Decision

Record significant architecture decisions as numbered Markdown files in `docs/adr/`, using the format in [template.md](template.md). Product and scope decisions stay in [PLAN.md](../PLAN.md); ADRs cover technical choices.

## Consequences

- Each decision has a findable rationale, and changes happen through new ADRs that supersede old ones.
- A little writing overhead per decision.

## Alternatives considered

- **Wiki or issue tracker:** separated from the code and easy to lose when hosting changes.
