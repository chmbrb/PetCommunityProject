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
| 9 | **Sign-in uses passkeys first, with email magic links, passwords, and Google/Apple SSO as alternatives. Meta sign-in is not offered.** SSO is a per-instance setting (on for the main instance), never links accounts automatically, and gives no trust by itself. Privileged roles still need a passkey or authenticator app on the platform. See [ADR 0019](adr/0019-sign-in-options-passkeys-sso.md). | Reduces signup friction, the main reason people avoid new accounts, without making the platform depend on any one provider. Apple's guideline 4.8 requires an equivalent privacy-focused option wherever Google sign-in appears in the iOS app; to start, the iOS app simply doesn't offer Google sign-in. Meta's tracking and review process conflict with the platform's privacy goals. |

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
| Unverified user | Signup by passkey, email, or Google/Apple sign-in | Browse, follow, comment, report |
| ID-verified individual | Identity check | Apply to adopt or foster, post in communities, rehome a pet (rehoming fee only) |
| Verified organization | Shelter/rescue documentation (e.g. EIN, 501(c)(3)), manual review | Create and manage adoption listings and a community, manage staff accounts |
| Verified sitter *(Phase 7, opt-in instances only)* | ID check, valid pet-sitter liability insurance and bonding certificate, manual review | Sitter profile in the directory. Verification is dated and states exactly what was checked; it expires with the insurance certificate. |
| Verified breeder *(Phase 6)* | Identity, applicable breeder license/registration, manual review | Breeder listings within listing caps, subject to reviews and revocation |

All verified tiers can be downgraded or revoked. Every tier change is written to the audit log.

## Stack

- **Proxy:** Caddy (automatic TLS)
- **Backend:** Go API, with a Postgres-backed job queue (e.g. `river`) for media processing, notifications, and later federation delivery
- **Frontend:** Vuetify SPA, plus server-rendered listing pages and OpenGraph metadata so listings rank in search and share well on social media
- **Mobile:** the same Vuetify app packaged as native Android and iOS apps with Capacitor; push notifications through FCM (Android), APNs (iOS), and UnifiedPush (Android without Google services, and the F-Droid build); installable web app with web push as the iOS stopgap until the App Store account exists
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
- Sign-in with passkeys, email magic links, and passwords; accounts support multiple sign-in methods
- Spike: how instances accept Google/Apple sign-in from the shared mobile app
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
- Google and Apple sign-in on the web and Android, per-instance setting, with explicit account linking. The iOS app starts without Google sign-in (so guideline 4.8 doesn't apply); Sign in with Apple is added once the App Store account exists.
- Mobile beta: Android (Play Store testing track) and iOS (TestFlight) covering browse, search, apply, messaging, and push. If the Apple account isn't ready yet, iOS users get the installable web app with web push (iOS 16.4+) instead.
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
- App publishing accounts: one organization account for the whole project, not one per instance (Apple $99/year, Google Play $25 once). Once the nonprofit entity exists (with a D-U-N-S number), apply for Apple's developer fee waiver for nonprofits; it requires a free app with no in-app purchases, so donations stay on the website. Check Apple's current waiver terms and eligible countries before applying.
- Android distribution beyond Google Play: an F-Droid build (free; requires a fully open-source build with UnifiedPush instead of Google's push service)
- AGPL and the Apple App Store: Apple's terms conflict with GPL-family licenses for code the publisher doesn't own. Options: a contributor agreement that lets the project publish contributions in app stores, or an explicit app-store exception for the mobile client. Get legal advice before accepting outside contributions to the client code.
- Whether to offer a sign-in option for Meta later, based on what interviews show
- Push notification privacy: payloads pass through Google and Apple, so they carry no personal data (see ADR 0018).
