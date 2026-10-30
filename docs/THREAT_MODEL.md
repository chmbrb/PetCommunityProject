# Threat Model

Status: **Draft (Phase 0)**. Covers a single instance through Phase 3, and notes federation (Phase 5) and later phases. Revisit at the start of each phase. See [PLAN.md](PLAN.md), [PERSONAS.md](PERSONAS.md), and [DOMAIN_MODEL.md](DOMAIN_MODEL.md).

## Scope and architecture

```
Internet ──▶ Caddy (TLS) ──▶ Go API (rate limits) ──▶ Postgres
                     │                      │  └──▶ river jobs (media, email, later federation)
                     └──▶ Vuetify SPA       └──▶ Object storage (media)
                                                     │
                                Off-site backups ◀───┘ (WAL-G/pgBackRest, media replication)
```

### Trust boundaries

1. Browser ↔ Caddy (public internet)
2. Caddy ↔ API (internal network)
3. API ↔ Postgres and object storage (internal network, separate credentials)
4. API → outbound internet: email, link previews, later federation fetches (**SSRF risk**)
5. Instance → off-site backup storage
6. Admin and moderator UI ↔ privileged API routes
7. Mobile app ↔ API (bearer tokens, any instance chosen by the user)
8. API → push providers (FCM, APNs), which see notification metadata
9. *(Phase 5)* Instance ↔ remote instances

## Assets

| Asset | Sensitivity | Why it matters |
|-------|-------------|----------------|
| Adopter profiles and applications | **High** (PII: household, housing, contact details) | Stalking, discrimination, data-breach liability |
| Precise locations | **High** | Physical safety of individuals, fosters, and later sitters |
| Verification evidence | **High** | Identity documents, org registration |
| Verified org accounts | **High** (trust) | A hijacked shelter account is the most effective scam tool |
| Account credentials and sessions | High | Account takeover |
| Actor private keys | High (from Phase 5) | Impersonation across the fediverse |
| Moderation records and reports | Medium–High | Reporter safety, appeals |
| Media | Medium | CSAM risk, stolen photos, EXIF leaks |
| Public listings and posts | Low (public), integrity matters | Scams, defacement |
| Backups | **High** (contain everything) | Theft of a backup = full breach |
| Instance reputation | High | Trust is the product |

## Adversaries

| Adversary | Motivation | Capability |
|-----------|------------|------------|
| Adoption and deposit scammers | Money | Low–medium technical skill, high persistence, many accounts |
| Pet flippers and abusers | Free animals to resell or harm | Social engineering, plausible applications |
| Stalkers and harassers | Locate or intimidate a person | Persistent, targeted |
| Spammers and malware distributors | Traffic, infections | Automated, bulk accounts |
| Opportunistic attackers | Data, compute, defacement | Automated scanning for known CVEs |
| Malicious insiders | Grudges, curiosity | Moderator or admin privileges |
| Community hijackers | Control of a popular group | Sockpuppets, social engineering |
| Abusive content uploaders | Distribute illegal material | Uploads |
| *(Phase 5)* Malicious or compromised instances | Spam, abuse, impersonation | Full control of their own server |

## Threats and mitigations

Ratings: **L**ikelihood and **I**mpact, each H/M/L. **Phase** = when the mitigation must be in place.

### Platform abuse (domain-specific)

| # | Threat | L | I | Mitigations | Phase |
|---|--------|---|---|-------------|-------|
| A1 | Fake shelter or rescue gets verified using forged documents | M | H | Manual review; check EIN/501(c)(3) against public registries; contact the org via independently sourced details; verification can be revoked and every decision is audited; badge says exactly what was checked | 2 |
| A2 | Scam listing using stolen photos, demanding a deposit or shipping fee | H | H | Perceptual-hash match against existing listings and flag duplicates; only verified orgs and ID-verified individuals can list; "never pay outside the platform" warnings; scam report category with priority | 2 |
| A3 | Impersonation of a real shelter (lookalike handle or name) | H | H | Handle similarity checks against verified orgs; reserved names; fast impersonation report → quarantine | 2 |
| A4 | Pet flipper or abuser adopts or acquires rehomed pets | M | H | Rehoming fee nudges; screening questions; ID-verified adopters; shared org-to-org reports on bad applicants (careful: due process, defamation risk); route rehoming to shelter partners | 2 |
| A5 | Lost-pet reward scams | H | M | Warning on lost/found posts; no in-app payment for rewards; report category | 3 |
| A6 | Sockpuppets hijack a community via a leader-removal vote | M | M | Voter eligibility (account age, tier, prior participation); quorum; appeal window; admin confirmation when all leaders are absent; audit | 3 |
| A7 | Brigading or harassment of a shelter after a declined application | M | M | Rate limits on new accounts; per-org block lists; moderators can lock comments; reviews of orgs deferred or moderated | 2–3 |
| A8 | Spam and bot accounts | H | M | Email verification; new-account limits (no links or listings at first); rate limits; optional proof-of-work or CAPTCHA on signup | 1 |
| A9 | Stale listings mislead adopters | H | L | Listings expire unless refreshed; imports sync status | 2 |
| A10 | Shelter software imports bring in PII (finder, surrendering owner, adopter, or staff details; contact info in descriptions) | M | H | Read-only imports with allow-list field mapping; person records and internal notes discarded before storage or logging; free text scanned for contact details and held for shelter review ([ADR 0020](adr/0020-shelter-software-imports.md)) | 2 |

### Privacy

| # | Threat | L | I | Mitigations | Phase |
|---|--------|---|---|-------------|-------|
| P1 | EXIF GPS in photos reveals a home location | H | H | Re-encode every image and strip all metadata server-side; never serve originals. Built: variants are encoded from decoded pixels only, and tests check that EXIF and a GPS marker never reach published files | 1 |
| P2 | Precise location revealed by search queries (e.g. narrowing distances from several points to triangulate) | M | H | Search and display use only the snapped `public_point`; round distances; rate-limit search | 2 |
| P3 | Application data seen by the wrong parties | M | H | Strict authorization (applicant + the listing org's members only); profile snapshot per application; access by admins is audited | 2 |
| P4 | Excessive data retention | M | M | Retention periods per data type (see Data Retention in PLAN.md, still to be decided); account deletion removes PII and leaves tombstones; backup retention kept in line with them | 2 |
| P5 | Federated content can't be recalled | — | M | (Phase 5) Clear user notice; private data never federates; send `Delete` on removal | 5 |
| P6 | Reporter identity exposed to the person reported | M | H | Reports are anonymous to the subject by default | 2 |
| P7 | Google or Apple learn when users sign in to the platform | H | L | Disclosed in the privacy policy; minimal scopes (`openid email`); no provider SDK tracking; passkeys offered as the provider-independent default | 2 |

### Application security (STRIDE)

| # | Threat | L | I | Mitigations | Phase |
|---|--------|---|---|-------------|-------|
| S1 | Credential stuffing or phishing → account takeover | H | H | argon2id; rate limits and lockouts; check passwords against breached-password lists; **MFA required for verified orgs, moderators, and admins**; new-device login alerts | 1 |
| S2 | Session theft | M | H | HttpOnly, Secure, SameSite cookies; short-lived sessions with rotation (signing in again replaces the old session); revoke on password change; people can see their signed-in devices and sign out any of them; adding or removing a passkey needs a sign-in within the last 15 minutes, so a stolen session can't quietly add its own passkey; email notice on every passkey change | 1 |
| S3 | Account takeover through SSO account linking (attacker signs in with a provider account using the victim's email) | M | H | Never link by email automatically; linking requires signing in to the existing account and confirming; email alert and audit entry on link/unlink; match identities by provider + subject | 2 |
| S4 | User's Google or Apple account is compromised | M | H | Platform passkey or TOTP still required for privileged roles; new-device alerts; users can unlink a provider and revoke sessions | 2 |
| S5 | Flawed OIDC handling (missing state/nonce/audience checks, trusting unverified emails) | M | H | Maintained OIDC library; PKCE, state, nonce, issuer, and audience validation; only provider-verified emails; tests for each failure case | 2 |
| S6 | Magic link intercepted or reused | L | M | Single use, 15-minute expiry; only token hashes stored, and tokens are created by the worker at send time, so they never sit in the job queue; token in the URL fragment so it stays out of server logs; signing in takes a click, so email link scanners can't use it up; per-IP and per-recipient rate limits. Not bound to the requesting browser, so links work across devices | 1 |
| T1 | Tampering via broken authorization (IDOR: accessing another user's records by changing an ID) | H | H | Authorization check on every resource in one central policy layer; tests for each role × resource | 1 |
| T2 | SQL injection | L | H | Parameterized queries only (e.g. sqlc or pgx); no string-built SQL | 1 |
| T3 | XSS via user content | M | H | Render Markdown to sanitized HTML on the server (allow-list); strict CSP; no `v-html` on unsanitized data | 1 |
| T4 | CSRF | M | M | SameSite cookies + CSRF tokens on state-changing requests | 1 |
| R1 | A moderator or admin denies taking an action | M | M | Append-only audit log; admins can't edit it | 1 |
| I1 | SSRF through link previews or (later) federation fetches | M | H | Outbound fetches go through one hardened client: block private/internal IP ranges, re-check after DNS resolution, size and time limits, no redirects to internal addresses | 1 (5) |
| I2 | Secrets leaked in a repo, image, or logs | M | H | Production secrets are files (Compose secrets) mounted only into the containers that need them, never environment variables, so `docker inspect`, crash reports and child processes don't see them; `secrets/` is gitignored; gitleaks scans the full history in CI on every change; PII redacted in logs | 1 |
| I3 | Backup theft | L | H | Backups encrypted before upload; separate credentials with write-only/append-only permission where possible; restricted access. Built: the backup container has no network at all and alone holds the backup key; the database, app, and worker containers can't read the backup store | 1 |
| I4 | Theft of shelters' import API keys | L | H | Encrypted at rest, used only by the import worker, never shown again after entry; shelters can revoke them; read-only keys where the vendor offers them | 2 |
| I5 | Theft of actors' private signing keys (impersonation across the fediverse, Phase 5) | L | H | Encrypted at rest with AES-256-GCM under a key derived from `PC_SECRET_KEY`, bound to each actor's ID; only the worker holds the secret, so an API compromise or database dump alone doesn't expose them; the secret isn't in database backups | 1 |
| D1 | DoS or resource exhaustion | M | M | API rate limits per client IP, applied before authentication so floods don't reach the database (IPv6 grouped by /64); request size limits; image decompression-bomb limits (max pixels); job queue concurrency caps | 1 |
| E1 | Privilege escalation through role management | L | H | Only owners/admins grant roles; role changes are audited; elevated roles require MFA | 1 |

### Content and malware

| # | Threat | L | I | Mitigations | Phase |
|---|--------|---|---|-------------|-------|
| C1 | CSAM uploads | L | **Critical** | Hash matching (PDQ / PhotoDNA, via an industry hash-sharing program); quarantine without anyone viewing it; legally required reporting (e.g. NCMEC in the US); preserve evidence per legal requirements; operator runbook. Built: the pipeline has a pluggable scanner step that quarantines matches unpublished and keeps the original; **no provider is connected yet**, so uploads are limited to accounts with a confirmed email until one is | 1 |
| C2 | Malware via uploads (polyglot files, malicious SVG) | M | H | Allow only JPEG/PNG/WebP/HEIC/AVIF, checked by content rather than extension; no SVG; pixel limit checked from the header before decoding; decode with memory-safe code only (ADR 0021) in the memory-limited worker; raw uploads stay in a private bucket and are deleted after processing; only re-encoded files are served, with `nosniff`, ideally from a separate domain | 1 |
| C3 | Malicious links in posts | H | M | Link reputation checks (e.g. Safe Browsing); `rel="nofollow ugc noopener"`; new accounts can't post links | 1 |
| C4 | Graphic animal-cruelty content | M | M | Report category; quarantine; content warnings | 3 |

### Operations and supply chain

| # | Threat | L | I | Mitigations | Phase |
|---|--------|---|---|-------------|-------|
| O1 | Vulnerable dependency or container image | M | H | Pinned versions and image digests; `govulncheck`, `pnpm audit`, Trivy (lockfiles and built images; fixable high/critical OS-package vulnerabilities fail CI) and dependency review on pull requests; CodeQL; weekly scheduled scans; Dependabot; minimal base images | 1 |
| O2 | Compromised CI or release pipeline | L | H | Protected branches, signed tags, least-privilege CI tokens (read-only by default, no persisted checkout credentials), actions and scanner images pinned by SHA/digest, actionlint and zizmor on workflow changes, reproducible builds where practical | 1 |
| O3 | Backup fails silently → data loss | M | H | Monitoring and alerts on backups; scheduled restore drills; RPO/RTO targets set and tested. Built: the backup container's health check fails on stale backups, WAL backlog, archive errors, a failed drill, or a full disk; weekly automatic restore drills; alert emails still to come | 1 |
| O4 | Instance operator misconfiguration | H | M | Secure defaults; startup self-checks (TLS, secrets set, backups configured); hardening guide | 1 |
| O5 | Insider misuse of admin access to private data | L | H | Least privilege; break-glass access with a reason field; audit log reviewed by a second person | 1 |

### Mobile apps, messaging, and push (Phases 1–3)

| # | Threat | L | I | Mitigations | Phase |
|---|--------|---|---|-------------|-------|
| M1 | Stolen or leaked mobile bearer token | M | H | Tokens in Keychain / Android Keystore; revocable per device; "signed-in devices" page; rotation on sensitive actions; MFA step-up still required for privileged actions | 1 |
| M2 | Personal data exposed through push notifications (lock screen, Google/Apple servers) | M | M | Payloads carry only a type and ID; generic lock-screen text by default; per-type opt-in | 2 |
| M3 | Scams and harassment through messages (fake "shelter" asking for deposits, abuse after a declined application) | H | H | MVP threads exist only on applications, between the applicant and the listing's verified org; report and block on every message; scam-phrase and payment-link detection with warnings; moderators can lock threads | 2 |
| M4 | Fake instance or phishing via the app's instance picker | M | M | Default instance pre-selected; show instance name, domain, and rules before sign-in; warn on instances not on a curated list | 1 |
| M5 | Malicious or impersonating app in the stores using the project's name | L | M | Publish only under the project's organization developer account; link official store pages from every instance; report impersonating apps | 3 |
| M6 | Lost-pet push alerts used to track someone's location or spam an area | M | M | Alerts use approximate locations only; rate limits per account; only ID-verified users can trigger area alerts | 3 |

### Federation (Phase 5)

| # | Threat | Mitigations |
|---|--------|-------------|
| F1 | Forged activities | Verify HTTP Signatures; check the key belongs to the claimed actor; fetch objects from their origin |
| F2 | Malicious or spammy instance | Instance policy (allow / limit / block); shared block lists; start allow-list-only |
| F3 | Remote content bypasses local moderation | Run remote media through the same pipeline; quarantine remote content when reported |
| F4 | Remote instance claims a local org is "verified" | Badges are never trusted from remote instances unless that instance is allow-listed |
| F5 | Remote fetches used for SSRF or DoS | Use the hardened outbound client (I1); cap delivery retries and queue size |

### Later phases (summary)

- **Breeders (Phase 6):** forged licenses, puppy-mill fronts, deposit fraud → license checks, listing caps, reviews, fast revocation, legal review first.
- **Sitting (Phase 7):** physical safety, theft, harm to pets, liability → insurance requirement, meet-and-greet, incident process, directory model, opt-in per instance (see PLAN decision #6).

## Highest-priority risks for the MVP

1. **A2/A3: scam and impersonation listings.** These are the most likely threats and the most damaging to trust.
2. **S1: takeover of a verified org account.** MFA is required from day one.
3. **C1/C2: uploaded content.** The media pipeline must be finished before any upload feature ships.
4. **P1/P2/P3: location and application privacy.**
5. **T1: broken authorization.** Use one central policy layer, with tests.

## Phase 0 follow-ups

- Confirm legal obligations for CSAM reporting and data protection (GDPR/CCPA etc.) in the launch jurisdiction.
- Choose a hash-matching provider or program, and check eligibility for a small nonprofit.
- Decide the retention periods per data type (PLAN.md, Data Retention).
- Ask the Phase 2 pilot shelter how they have been impersonated, so A2/A3 mitigations are based on real cases.
- Write an incident response runbook (account compromise, data breach, CSAM, scam wave).
