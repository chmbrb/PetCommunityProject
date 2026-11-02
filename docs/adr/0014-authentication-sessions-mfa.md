# 0014. Authentication: server-side sessions and MFA

- **Status:** Accepted
- **Date:** 2026-10-29 (amended 2026-11-02: optional passkeys, TOTP details, email codes)
- **Related:** THREAT_MODEL.md S1, S2, T4, E1, M1; PERSONAS.md (Account taker); [0018](0018-mobile-apps-capacitor.md), [0019](0019-sign-in-options-passkeys-sso.md)

## Context

Takeover of a verified shelter account is one of the top MVP risks: it turns a trusted identity into a scam tool. The web SPA and the API are served from the same domain. The native mobile apps ([0018](0018-mobile-apps-capacitor.md)) can't rely on cookies in the same way and may connect to any instance.

## Decision

- **Sign-in methods:** passkeys (recommended, never required), email links or codes, passwords, and Google/Apple SSO. People can sign up with any of them. See [0019](0019-sign-in-options-passkeys-sso.md). Every method ends in the session model below.
- **Passwords** (optional), as built:
  - **Hashing:** argon2id at OWASP's recommended minimum (19 MiB, 2 passes, 1 lane), in the PHC string format. Hashes with older parameters are upgraded at the next sign-in. At most 4 hashes run at once, so a burst of attempts can't exhaust memory. Unknown accounts are checked against a dummy hash, so timing doesn't reveal which accounts exist.
  - **Rules** (NIST SP 800-63B): 12 to 256 characters, no composition rules; not the email address, its name part, or the handle; not found in known breaches. The breach check uses the Have I Been Pwned range API with k-anonymity and padding: only the first 5 hex characters of the password's SHA-1 hash leave the server. If the service is unreachable, the password is accepted (and a warning logged) rather than blocking people. `PC_BREACHED_PASSWORD_CHECK=false` turns it off, for instances without outbound internet access.
  - **Lockout:** 10 wrong passwords in a row lock password sign-in for 15 minutes. Email codes and passkeys keep working, so a lockout can't keep the owner out, and per-IP sign-in rate limits apply as well.
  - **New devices:** a password sign-in from a device the account hasn't signed in from needs an emailed code as well. The email warns that the password was used from a new device. Devices are recognized by a random token (an `HttpOnly` cookie in browsers, the `X-Device-Token` header from the apps), stored only as a hash and remembered for a year after the last sign-in by any method. A leaked password alone therefore can't take over an account. This check stands in until TOTP exists; then accounts with MFA get the MFA step instead.
  - **Changes:** setting, changing, or removing a password needs step-up and sends an email notice; setting or changing it signs out every other device. A password can be set when confirming an email sign-up, or later in account settings. It can be removed while the account can still sign in another way (a verified email address or a passkey).
- **Sessions:** opaque random 256-bit tokens, stored only as a SHA-256 hash in Postgres; sent as `HttpOnly; Secure; SameSite=Lax` cookies named `__Host-pc_session` (production); a new session is issued on every sign-in; all sessions revoked on password change. Web sessions last 30 days, mobile sessions 180 days. No JWTs for browser sessions.
- **CSRF:** SameSite cookies plus Go's `http.CrossOriginProtection` (standard library since Go 1.25), which rejects cross-origin state-changing requests using the browser's `Sec-Fetch-Site` and `Origin` headers. This replaces the CSRF tokens originally planned: it gives the same protection without tokens to manage. The native apps' origins are trusted, since they use bearer tokens.
- **Mobile apps:** sign-in returns an opaque bearer token (same session table, marked as a mobile session with a device name) stored in Keychain / Android Keystore. Tokens are long-lived but revocable, rotate on use of sensitive actions, and are listed in the user's "signed-in devices" page. Bearer-token requests don't need CSRF tokens. Biometric unlock can protect the app locally but doesn't replace MFA.
- **MFA:** passkeys/WebAuthn and TOTP, with one-time recovery codes. Either kind of factor satisfies an MFA requirement; neither is mandatory on its own.
  - **Required** for members of verified organizations, community moderators and owners, and instance admins, including people who sign in with Google or Apple.
  - Optional for everyone else, and offered to everyone from Phase 1.
  - **TOTP (RFC 6238)** with the settings every authenticator app supports: 6 digits, 30-second steps, SHA-1, accepting one step of clock drift either way. Setup shows a QR code (phone authenticator apps), the setup key as text with a copy button (password managers such as 1Password and Bitwarden, which can then fill the code in), and an `otpauth://` link (setting up on the same phone). It's enabled only after the person enters a valid code.
  - **Replay protection:** the last accepted time step is stored, so a code can't be reused, even within its 30 seconds.
  - **Attempt limits:** code entry has the same rate limits and temporary lockouts as passwords.
  - **TOTP secrets are encrypted at rest** (AES-256-GCM) with their own key, a new `mfa_key` secret file given only to the API, which checks codes. It can't be `PC_SECRET_KEY`, which only the worker holds ([0012](0012-activitypub-shaped-model-federation-deferred.md)). A database dump alone then doesn't expose the secrets.
  - **Recovery codes:** 10 random single-use codes, stored only as hashes and shown once. Using one sends an email alert. People can generate a new set, which invalidates the old one.
  - **Changes need step-up:** adding or removing TOTP, or generating new recovery codes, needs a recent sign-in, like passkey changes, and sends an email alert.
  - **No lock-out:** the last MFA factor can't be removed while the person's role requires MFA.
- **Email codes:** sign-in emails carry a one-time link and a 6-digit code for the same sign-in; the code is for when the link opens in the wrong browser or outside the app. The code is stored as a hash, expires with the link, and allows 5 wrong attempts. Password sign-ins on a new device, by accounts without MFA, also need an emailed code. Email codes never count as MFA: they prove only access to the inbox, which a magic link already proves, and inbox takeover is a common route to account takeover. NIST SP 800-63B excludes email for the same reason.
- **Alerts:** email on new-device login, MFA changes, recovery code use, and email changes. (Built so far: emails when a passkey is added or removed.)
- **Email verification** before posting or applying.
- **Step-up:** sensitive actions (role grants, verification decisions, data exports) require re-entering MFA within the last few minutes. Built so far: adding or removing a passkey requires a sign-in within the last 15 minutes (`auth.ReauthWindow`); otherwise the person confirms by signing in again with a passkey, which replaces their session rather than adding one.
- **Signed-in devices:** people can see their sessions (with a device label from the User-Agent, stored only as long as the session) and sign out any of them, or all but the current one.

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
- **Email codes as a second factor:** see "Email codes" above; they're offered as a sign-in method and a new-device check only.
- **Requiring passkeys:** the most phishing-resistant option, but some people's devices, browsers, or habits don't suit them yet, and a hard requirement would turn them away at sign-up. Passkeys stay the recommended option.
