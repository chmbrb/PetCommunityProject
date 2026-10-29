# Personas

Status: **Draft (Phase 0)**. These are working hypotheses, not findings. Phase 0 outreach was limited (see below), so they'll be tested with a pilot shelter in Phase 2. See [PLAN.md](PLAN.md) for phases and trust tiers.

Each persona lists the phase in which it becomes relevant. Personas are written as roles rather than named individuals.

## Primary personas (MVP, Phases 1–3)

### 1. Shelter adoption coordinator
*Verified organization · Phase 2*

A staff member at a small-to-mid municipal or nonprofit shelter. They manage 20–150 animals at a time, often alongside other duties.

- **Goals:** get animals adopted faster, attract adopters who are a good fit, spend less time answering the same questions, keep the shelter's public reputation intact.
- **Frustrations:** re-entering the same animal on several sites, applications arriving by email/phone/paper in different formats, scammers re-posting their animals with fake contact details, harassment after a declined application.
- **Needs from the platform:**
  - Imported listings from their shelter software (Shelterluv or PetPoint) rather than manual entry
  - Structured applications with status tracking and a shared view for staff
  - Multiple staff accounts under one organization, with roles
  - A verification badge adopters recognize
  - Tools to report impersonation quickly
- **Success looks like:** listing an animal takes under 5 minutes, and every application arrives in one queue.

### 2. Small rescue volunteer
*Verified organization · Phase 2 (fostering in Phase 4)*

Runs or volunteers for a rescue with no physical facility; animals live in foster homes. Usually unpaid, working evenings on a phone.

- **Goals:** visibility comparable to bigger shelters, simple tools, coordination with fosters.
- **Frustrations:** paperwork for verification, software built for large shelters, cost.
- **Needs from the platform:**
  - A verification path that works for small or newly registered rescues
  - A workflow that works well on mobile
  - Free to use
- **Risk to design for:** this persona is the easiest for a scammer to impersonate ("we're a small rescue"), so the verification path must be accessible without being weak.

### 3. Prospective adopter
*Unverified → ID-verified individual · Phase 2*

Looking for a specific kind of pet (species, breed, size, age, temperament) within travel distance and a fee range they can afford.

- **Goals:** find a suitable animal nearby, trust that the listing and the organization are real, know where an application stands.
- **Frustrations:** stale listings for animals already adopted, scam listings that ask for deposits, filling in the same long application again for every shelter, getting no response.
- **Needs from the platform:**
  - Search by species, breed, location radius, and fee
  - Visible verification badges
  - A reusable adopter profile that pre-fills applications
  - Application status updates
  - Saved searches and alerts
- **Privacy expectation:** home address and household details are shared only with the shelter they apply to.

### 4. Rehoming owner
*ID-verified individual · Phase 2*

An individual who can no longer keep their pet (moving, allergies, a new baby, cost) and wants it to go to a good home.

- **Goals:** find a safe home quickly, screen applicants, avoid bad actors.
- **Frustrations:** fear of "free to good home" responders with bad intentions, time pressure, guilt.
- **Needs from the platform:**
  - A guided rehoming flow that suggests or requires a modest rehoming fee
  - Screening questions
  - The option to route the pet to a local shelter partner instead
- **Design note:** this is a common route to animal abuse (bait animals, flipping), so the flow should nudge toward shelters and encourage a rehoming fee.

### 5. Community owner and moderator
*ID-verified individual or org staff · Phase 3*

Runs a local community (e.g. "Eastside Dog Owners" or a shelter's supporter group).

- **Goals:** a healthy, active community; reasonable moderation workload; passing the role on cleanly when they step back.
- **Frustrations:** spam, brigading, being the only active moderator, communities taken over by bad actors.
- **Needs from the platform:**
  - A moderation queue and community-level rules
  - Delegated moderators
  - An ownership handoff process
  - Fair, abuse-resistant rules for removing inactive leaders

### 6. Community member and lost-pet searcher
*Unverified or ID-verified · Phase 3*

A local pet owner who uses the site socially and urgently when a pet goes missing.

- **Goals:** share pet photos and advice, and get a lost-pet alert out fast to nearby people.
- **Frustrations:** lost-pet scams (fake finders asking for "reward" money up front), alerts that reach no one nearby.
- **Needs from the platform:**
  - A lost/found post type with an approximate location and radius-based notifications
  - Scam warnings on reward requests

### 7. Instance operator
*Instance admin · Phase 1*

Runs an instance, often for a shelter network, county, or region. They may be technical or rely on a volunteer.

- **Goals:** low cost, low maintenance, safe instance, reliable backups, clear legal position.
- **Frustrations:** complex deployments, surprise bills, being personally liable, moderation burnout.
- **Needs from the platform:**
  - One-command docker compose deployment, automated backups, and tested restores
  - A dashboard of operating costs and funding
  - Feature flags (e.g. sitting off by default)
  - Admin tools that only cover safety and legal enforcement
  - Policy templates

## Later-phase personas

| Persona | Phase | Core need | Key risk |
|---------|-------|-----------|----------|
| Foster volunteer | 4 | Accept placements, report on the animal's progress, communicate with the shelter | Over-capacity, burnout |
| Verified breeder | 6 | Reach buyers with a credible reputation | Puppy mills, deposit fraud, regulatory exposure |
| Pet sitter | 7 | Get found by local owners, build a reputation | Liability, safety in strangers' homes |
| Pet owner seeking a sitter | 7 | A trustworthy, insured sitter nearby | Harm to the pet or property |

## Anti-personas (bad actors to design against)

| Anti-persona | Tactic | Primary countermeasures |
|--------------|--------|------------------------|
| Adoption scammer | Fake shelter or listing using stolen photos, asks for a deposit or shipping fee | Org verification, image hash matching against existing listings, "never pay off-platform" warnings, impersonation reports |
| Pet flipper / bait seeker | Takes free or cheap rehomed pets to resell or for abuse | Rehoming fee nudges, screening questions, shelter routing, adopter verification |
| Fake-verification forger | Forged EIN or 501(c)(3) documents | Manual review, checks against public registries, verification that can be revoked and audited |
| Account taker | Phishes shelter staff to hijack a trusted account | MFA required for verified orgs, session and device alerts |
| Community hijacker | Sockpuppets an inactive-leader removal vote | Voting eligibility (account age, tier, participation), quorum, appeal window, admin confirmation |
| Stalker / harasser | Uses photos, location, or applications to locate people | EXIF stripping, approximate locations, private application data, blocking |
| Spammer / malware distributor | Malicious links or files | Image-only uploads with re-encoding, link reputation checks, rate limits, new-account restrictions |

## Phase 0 findings

Outreach to local shelters (2026-10) produced one finding: **they use a mix of Shelterluv and PetPoint**. Even that much was a noticeable imposition on staff time, so further interviews are unlikely.

What this means:

- It supports the coordinator persona's core need: shelters won't re-enter data, so imports are essential ([ADR 0020](adr/0020-shelter-software-imports.md)).
- It also suggests shelter staff have very little spare time. Onboarding has to be close to effortless: paste an API key, review the imported animals, publish.
- All other assumptions in this document remain untested. The interview guide below is kept for the Phase 2 pilot shelter and for adopters, who are easier to reach.

## Interview guide (for the Phase 2 pilot)

**Shelters and rescues (target: 2–3 interviews)**
1. Walk me through what happens from intake to adoption. Where does the time go?
2. Which software and sites do you use today (shelter software, Petfinder, social media)? What do you pay for them?
3. How do applications arrive, and how do you track them?
4. Have scammers impersonated you? What happened?
5. What would make you stop using a new platform after a month?
6. Would you join or run a local community space? Who would moderate it?
7. What proof of status could you provide easily (EIN, 501(c)(3) letter, municipal ID)?

**Adopters and rehoming owners (target: 3–5 conversations)**
1. How did you search last time? What went wrong?
2. Have you encountered a listing you believed was a scam?
3. What information are you comfortable sharing, and with whom?
4. (Rehoming) What worried you most about the people who responded?

**Findings to capture:** confirmed or rejected assumptions per persona, must-have vs. nice-to-have features for the Phase 2 MVP, and shelter-software import priorities.
