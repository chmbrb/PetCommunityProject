# 0014. Authentication: server-side sessions and MFA

- **Status:** Accepted
- **Date:** 2026-10-29
- **Related:** THREAT_MODEL.md S1, S2, T4, E1, M1; PERSONAS.md (Account taker); [0018](0018-mobile-apps-capacitor.md), [0019](0019-sign-in-options-passkeys-sso.md)

## Context

Takeover of a verified shelter account is one of the top MVP risks: it turns a trusted identity into a scam tool. The web SPA and the API are served from the same domain. The native mobile apps ([0018](0018-mobile-apps-capacitor.md)) can't rely on cookies in the same way and may connect to any instance.

## Decision

- **Sign-in methods:** passkeys (primary), email magic links, passwords, and Google/Apple SSO. See [0019](0019-sign-in-options-passkeys-sso.md). Every method ends in the session model below.
- **Passwords** (optional): argon2id hashing; minimum length 12; rejected if found in breached-password lists (k-anonymity range check or a bundled list); login rate limits and temporary lockouts.
- **Sessions:** opaque random 256-bit tokens, stored only as a SHA-256 hash in Postgres; sent as `HttpOnly; Secure; SameSite=Lax` cookies named `__Host-pc_session` (production); a new session is issued on every sign-in; all sessions revoked on password change. Web sessions last 30 days, mobile sessions 180 days. No JWTs for browser sessions.
- **CSRF:** SameSite cookies plus Go's `http.CrossOriginProtection` (standard library since Go 1.25), which rejects cross-origin state-changing requests using the browser's `Sec-Fetch-Site` and `Origin` headers. This replaces the CSRF tokens originally planned: it gives the same protection without tokens to manage. The native apps' origins are trusted, since they use bearer tokens.
- **Mobile apps:** sign-in returns an opaque bearer token (same session table, marked as a mobile session with a device name) stored in Keychain / Android Keystore. Tokens are long-lived but revocable, rotate on use of sensitive actions, and are listed in the user's "signed-in devices" page. Bearer-token requests don't need CSRF tokens. Biometric unlock can protect the app locally but doesn't replace MFA.
- **MFA:** passkeys/WebAuthn and TOTP, with one-time recovery codes.
  - **Required** for members of verified organizations, community moderators and owners, and instance admins, including people who sign in with Google or Apple.
  - Optional for everyone else.
- **Alerts:** email on new-device login, MFA changes, and email changes.
- **Email verification** before posting or applying.
- **Step-up:** sensitive actions (role grants, verification decisions, data exports) require re-entering MFA within the last few minutes.

## Consequences

- Sessions can be revoked instantly (moderation bans take effect immediately).
- Web and mobile share one session model, so "sign out everywhere" and bans cover both.
- One database lookup per request. Fine at this scale; cache in memory briefly if needed.
- MFA requirements add friction for shelter staff. Passkeys keep it low; the onboarding flow must explain why.
- Google and Apple sign-in are covered in [0019](0019-sign-in-options-passkeys-sso.md). They don't replace MFA requirements.

## Alternatives considered

- **JWT access tokens:** hard to revoke, easy to mishandle in a browser.
- **External identity provider (Keycloak, Authentik):** powerful, but another heavy service to run.
- **SMS codes:** vulnerable to SIM swapping, and they cost money.
