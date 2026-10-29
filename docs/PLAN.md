# Plan

This document records project decisions and the phased roadmap. See [DESCRIPTION.md](DESCRIPTION.md) for the original vision.

## Decisions

| # | Decision | Rationale |
|---|----------|-----------|
| 1 | **MVP focuses on adoption through verified shelters.** | Shelters bring the listings and the users, they're easy to verify, and they carry the least liability. Fostering and sitting come later. |
| 2 | **Data model is ActivityPub-shaped from day one; federation is turned on in a later phase.** | Get moderation and trust working on a single instance first. Modeling users, orgs, and groups as AP actors makes federating later an addition rather than a rewrite. |
| 3 | **Verified breeders are allowed, but deferred to a late phase (Phase 6, right before pet sitting and monetization).** Until then, only shelter adoption and individual rehoming fees are allowed. | Breeders meet real demand, but they carry the highest fraud and regulatory risk. Deferring them lets moderation, trust tiers, and federation mature first. When breeders arrive, they need identity and licensing checks, listing caps, and verification that can be revoked. |
| 4 | **Planning lives in `docs/PLAN.md`; `docs/DESCRIPTION.md` stays the original vision.** | Keeps the vision and the decisions separate. |
| 5 | **Monetization is cost recovery, not profit.** Revenue should only cover what an instance needs to run and keep running: hosting, backups, moderation, maintenance, insurance, and a reserve. | Profit incentives work against trust and reward listing volume. Instances should be self-sufficient without becoming commercial operations. |
| 6 | **Pet sitting uses a directory model, is opt-in per instance (off by default), and charges no booking fees.** Sitters are independent operators who carry their own insurance. | Nonprofit status protects directors and volunteers but not the organization or its reserve. Not controlling the work, not taking a cut of bookings, and requiring sitters to be insured keep the platform out of the transaction. Opt-in lets each operator enable sitting only after securing its own insurance and legal sign-off. |
| 7 | **The project is licensed under AGPLv3.** | Anyone who runs a modified version as a network service must share its source with users. That keeps every instance's improvements available to the whole community, which is the norm for fediverse software. |
| 8 | **Native Android and iOS apps are an end goal, built early: an API-first backend from Phase 1 and app beta releases alongside the Adoption MVP.** Recommended approach: package the Vue/Vuetify app with Capacitor (see [ADR 0018](adr/0018-mobile-apps-capacitor.md)). | Most users, including shelter staff and rescue volunteers, are on phones. Native apps add push notifications (application updates, messages, lost-pet alerts), camera and location access, and an app store presence that builds trust. Building on the same web codebase keeps one UI to maintain. |

## Principles

- **Lean:** few containers, Postgres for as much as possible, cheap to self-host.
- **Safety first:** moderation tooling ships before or alongside every feature that creates content.
- **Trust is earned and visible:** a user's verification tier determines what they can do, and it's shown on their profile and listings.
- **Sustainable, not profitable:** funding targets an instance's operating costs plus a reserve, and both are published openly.
- **Mobile-first:** every feature is designed for phones first, and everything the web app does goes through the same public API the mobile apps use.
- **Ready for federation:** stable actor URIs, inbox/outbox-compatible activity records, Group actors for communities.

## Trust Tiers

| Tier | How it's earned | What it allows |
|------|-----------------|----------------|
| Unverified user | Email signup | Browse, follow, comment, report |
| ID-verified individual | Identity check | Apply to adopt or foster, post in communities, rehome a pet (rehoming fee only) |
| Verified organization | Shelter/rescue documentation (e.g. EIN, 501(c)(3)), manual review | Create and manage adoption listings and a community, manage staff accounts |
| Verified sitter *(Phase 7, opt-in instances only)* | ID check, valid pet-sitter liability insurance and bonding certificate, manual review | Sitter profile in the directory. Verification is dated and states exactly what was checked; it expires with the insurance certificate. |
| Verified breeder *(Phase 6)* | Identity, applicable breeder license/registration, manual review | Breeder listings within listing caps, subject to reviews and revocation |

All verified tiers can be downgraded or revoked. Every tier change is written to the audit log.

## Stack

- **Proxy:** Caddy (automatic TLS)
- **Backend:** Go API, with a Postgres-backed job queue (e.g. `river`) for media processing, notifications, and later federation delivery
- **Frontend:** Vuetify SPA, plus server-rendered listing pages and OpenGraph metadata so listings rank in search and share well on social media
- **Mobile:** the same Vuetify app packaged as native Android and iOS apps with Capacitor; push notifications through FCM (Android) and APNs (iOS)
- **Database:** PostgreSQL with PostGIS (location search) and built-in full-text search (no separate search cluster)
- **Media:** S3-compatible object storage (MinIO, or a hosted provider)
- **Orchestration:** docker compose
- **Backups:** WAL-G or pgBackRest to off-site storage, plus media bucket replication. Set explicit RPO/RTO targets (see Open Questions) and run scheduled restore drills.

## Safety Baseline

- **Uploads:** image types only, size limits, re-encode all images, strip EXIF metadata (including GPS)
- **CSAM:** match uploads against known-image hashes (e.g. PDQ / PhotoDNA) and follow the legal reporting process
- **Abuse controls:** rate limits, a user report queue, keyword filters
- **Corrective actions:** warn, restrict, quarantine (content or account), suspend, ban
- **Accountability:** an audit log of all moderation and admin actions
- **Privacy:** only approximate locations are public for individuals; precise addresses are shared only within an approved application
- **Breeder anti-scam measures (Phase 6):** verification badges, listing caps, warnings against off-platform deposits, buyer reviews, fast revocation

## Governance

- Each community has one **owner** and a set of approved **moderators**.
- **Ownership handoff:** the owner can transfer ownership to a moderator, and the moderator must accept.
- **Removing inactive leaders** (at the community level only):
  - A leader counts as inactive after N days with no activity (proposed: 90).
  - Moderators or established members can start a removal vote that needs a defined quorum.
  - The inactive leader is notified and has an appeal window.
  - If every leader is inactive, members may nominate a new owner, subject to instance-admin confirmation.
- **Instance admins** don't run communities. They step in only for safety, legal, or ToS violations.

## Phased Roadmap

### Phase 0: Discovery
- Interview 2–3 shelters about their current software, workflows, and pain points
- Personas ([draft](PERSONAS.md)), domain model ([draft](DOMAIN_MODEL.md)), threat model ([draft](THREAT_MODEL.md)), first ADRs (architecture decision records, [docs/adr](adr/README.md))
- **Exit:** a validated MVP scope and an agreed domain model

### Phase 1: Foundation
- Compose stack (Caddy, Go API, Postgres, object storage), CI
- Auth, accounts, and profiles modeled as AP actors
- Versioned public API (`/api/v1`) used by both web and mobile; token auth for mobile clients
- Mobile shell: Capacitor project for Android and iOS, building in CI, with sign-in and an instance picker
- Media pipeline (re-encode, EXIF strip, hash matching)
- Backups configured, with a successful restore drill
- **Exit:** a user can sign up and upload a safe image (on web and from a phone camera in the app), and a backup restores cleanly

### Phase 2: Adoption MVP
- Organization verification workflow
- Pet listings (species, breed, age, fee, location, photos)
- Search by breed, location radius, and fee range
- Adoption applications and status tracking
- Messaging between applicants and the organization, attached to each application
- Notifications: in-app, email, and push (application updates, new messages, saved-search matches)
- Moderation tools: report queue, actions, audit log
- Server-rendered listing pages with OpenGraph metadata
- Mobile beta: Android (Play Store testing track) and iOS (TestFlight) covering browse, search, apply, messaging, and push
- App store requirements: in-app account deletion, report and block on all user content, privacy disclosures
- **Exit:** a pilot shelter lists pets and processes real applications, including from the mobile beta

### Phase 3: Communities
- Group actors, posts, follows/subscriptions
- Owner/moderator roles, handoff, and inactive-leader removal rules
- Lost-and-found pet posts, with push alerts to nearby users who opt in
- Public release of the Android and iOS apps in both stores
- **Exit:** a pilot shelter runs an active community, and the apps are publicly available

### Phase 4: Fostering
- Foster applications, placements, and shelter–foster communication
- **Exit:** a shelter manages foster placements on the platform

### Phase 5: Federation
- HTTP signatures, inbox/outbox delivery, WebFinger
- FEP-1b12 Group federation for communities
- Instance allow/block lists and federated moderation reports
- **Exit:** two instances federate listings and communities with moderation working across both

### Phase 6: Verified Breeders
- Prerequisite: legal review of animal sale and breeder rules in the launch jurisdiction(s)
- Breeder verification flow, listing caps, reviews, revocation
- Anti-scam warnings and reporting
- Decide how breeder listings federate (e.g. local-only, or only to allow-listed instances)
- **Exit:** breeder listings live with no unresolved fraud reports during the pilot

### Phase 7: Pet Sitting and Monetization
- Prerequisite: legal review of pet-sitting liability in the launch jurisdiction(s), and platform insurance in place (general liability, directors and officers, cyber)
- Pet sitting as an instance-level feature flag, off by default
- Directory model: sitters set their own rates, schedules, and terms. The platform connects people but doesn't assign, price, or direct the work.
- Sitter verification: ID check and valid liability insurance and bonding certificate, re-checked on expiry. Badge wording says exactly what was verified and when.
- Payments go directly between owner and sitter, or through Stripe Connect. The platform never holds funds and takes no booking fee.
- Safety features: meet-and-greet before the first booking, emergency vet authorization and contact forms, reviews, incident reporting with a documented response process
- Sitter listings stay on the home instance (not federated) by default
- Project-provided templates: sample terms of service (liability limits, sitter indemnification, disclaimers), verification checklist, insurance guide. Each operator still gets its own legal review.
- Cost-recovery funding only, sized to each instance's operating budget (including insurance premiums):
  - Donations and recurring supporter contributions (e.g. Open Collective)
  - Sponsored placement for vetted local businesses (clearly labeled, never ranked above organic listings)
  - Hosted instances for shelters, priced at cost
- Transparency: publish each instance's costs, income, and reserve. Revenue beyond the budget and reserve goes to lower fees or to supporting shelters, not to profit.
- **Exit:** at least one instance covers its operating costs and reserve from its own funding, without rewarding listing volume

## Open Questions

- RPO/RTO targets (e.g. RPO ≤ 15 min, RTO ≤ 4 h?)
- Legal review of animal sale and breeder rules in the launch jurisdiction(s)
- Importing listings from shelter software (Petfinder, RescueGroups, Shelterluv, PetPoint)
- Identity verification provider and its cost
- Who operates the first instance, and under which legal entity (a nonprofit structure fits the cost-recovery model)
- How instances are funded before Phase 7. Donations could be turned on as early as Phase 2.
- Target reserve size (e.g. 6–12 months of operating costs)
- Platform insurance cost per instance, and whether it's affordable for small operators
- Minimum sitter insurance coverage required, and how certificates are checked
- Who publishes the apps in the stores (needs an organization developer account: Apple $99/year, Google $25 once), and app store costs in the operating budget
- AGPL and the Apple App Store: Apple's terms conflict with GPL-family licenses for code the publisher doesn't own. Options: a contributor agreement that lets the project publish contributions in app stores, or an explicit app-store exception for the mobile client. Get legal advice before accepting outside contributions to the client code.
- Push notification privacy: payloads pass through Google and Apple, so they should carry no personal data. Should the Android app also support UnifiedPush for devices without Google services?
