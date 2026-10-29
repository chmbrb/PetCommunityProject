# 0014. Authentication: server-side sessions and MFA

- **Status:** Proposed
- **Date:** 2026-10-29
- **Related:** THREAT_MODEL.md S1, S2, T4, E1; PERSONAS.md (Account taker)

## Context

Takeover of a verified shelter account is one of the top MVP risks: it turns a trusted identity into a scam tool. The SPA and the API are served from the same domain.

## Decision

- **Passwords:** argon2id hashing; minimum length 12; rejected if found in breached-password lists (k-anonymity range check or a bundled list); login rate limits and temporary lockouts.
- **Sessions:** opaque random tokens stored hashed in Postgres; sent as `HttpOnly; Secure; SameSite=Lax` cookies; rotated on login and privilege change; all sessions revoked on password change. No JWTs for browser sessions.
- **CSRF:** SameSite cookies plus a CSRF token on every state-changing request.
- **MFA:** passkeys/WebAuthn and TOTP, with one-time recovery codes.
  - **Required** for members of verified organizations, community moderators and owners, and instance admins.
  - Optional for everyone else.
- **Alerts:** email on new-device login, MFA changes, and email changes.
- **Email verification** before posting or applying.
- **Step-up:** sensitive actions (role grants, verification decisions, data exports) require re-entering MFA within the last few minutes.

## Consequences

- Sessions can be revoked instantly (moderation bans take effect immediately).
- One database lookup per request. Fine at this scale; cache in memory briefly if needed.
- MFA requirements add friction for shelter staff. Passkeys keep it low; the onboarding flow must explain why.
- Social logins (Google etc.) are not included. They could be added later as an option, but don't replace MFA requirements.

## Alternatives considered

- **JWT access tokens:** hard to revoke, easy to mishandle in a browser.
- **External identity provider (Keycloak, Authentik):** powerful, but another heavy service to run.
- **SMS codes:** vulnerable to SIM swapping, and they cost money.
