# 0002. License under AGPLv3

- **Status:** Accepted
- **Date:** 2026-10-29
- **Related:** PLAN.md decision #7

## Context

The software is meant to be run by many independent operators (shelters, counties, regional groups). Under GPLv3, someone can modify the software and run it as a website without sharing their changes, because serving a website isn't "distribution".

## Decision

License the project under the GNU Affero General Public License v3.0 (`LICENSE`). Operators who run a modified version must offer its source to the instance's users.

## Consequences

- Improvements made by any instance stay available to the whole community. This matches fediverse norms (Mastodon, Lemmy, GoToSocial are AGPL).
- Some businesses avoid AGPL dependencies or contributions. This is acceptable for a nonprofit, community project.
- Follow-up:
  - Add a "Source code" link in the app footer that points to the exact running version, which satisfies AGPL section 13.
  - Add SPDX headers (`// SPDX-License-Identifier: AGPL-3.0-or-later`) to source files.
  - Check that dependency licenses are compatible (MIT/BSD/Apache-2.0/MPL are fine).
  - Decide whether to use "-only" or "-or-later". Proposed: `AGPL-3.0-or-later`.

## Alternatives considered

- **GPLv3:** doesn't cover network use.
- **MIT/Apache-2.0:** allows closed commercial forks, which conflicts with the cost-recovery, community-owned model.
