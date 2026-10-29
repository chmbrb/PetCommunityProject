# 0019. Sign-in options: passkeys, magic links, and Google/Apple SSO

- **Status:** Accepted
- **Date:** 2026-10-29
- **Related:** PLAN.md decision #9; [0014](0014-authentication-sessions-mfa.md), [0017](0017-instance-feature-flags.md), [0018](0018-mobile-apps-capacitor.md); THREAT_MODEL.md S3–S6

## Context

Many people, especially prospective adopters, give up when asked to create yet another account. Most of that reluctance is about creating and remembering another password. Single sign-on (SSO) with existing accounts (Google, Apple, Meta) reduces friction but brings privacy, security, and federation trade-offs:

- Apple App Store guideline 4.8: an iOS app offering third-party social login (Google, Meta) must also offer an equivalent option that collects only name and email, lets users keep their email private, and doesn't track app activity for advertising without consent. Sign in with Apple meets this, but isn't the only way to.
- Meta's login SDK tracks users and requires business verification and app review.
- Each instance is its own website, so each operator would need its own registrations with providers. Sign in with Apple on an instance's website requires that operator to have a paid Apple Developer account. Sign in with Apple inside the iOS app is covered by the project's single publishing account.
- An SSO login proves control of an external account, not identity.

## Decision

**Sign-in methods offered:**

| Method | Default | Notes |
|--------|---------|-------|
| Passkey | **Primary signup path** | No password; syncs through Google/Apple/other password managers |
| Email magic link | On | One-time sign-in link, short expiry, single use |
| Password | On | For people who prefer it; rules from [0014](0014-authentication-sessions-mfa.md) |
| Google | On (main instance), per-instance flag | Standard OpenID Connect |
| Apple | Main instance (web) and iOS app once the App Store account exists; per-instance flag | Optional for other instances: only operators who pay for their own Apple Developer account can offer it on the web |
| Meta | **Not offered** | Revisit only if pilot or user feedback shows real demand |

**Implementation:**

- **Generic OpenID Connect** (`coreos/go-oidc` + `golang.org/x/oauth2`) rather than provider-specific SDKs. Providers are configured per instance, which also allows a later option for shelters to sign their staff in with their own Google Workspace or Microsoft 365 accounts.
- **Strict token validation:** PKCE, `state` and `nonce` checks, issuer and audience validation, signature checks against the provider's published keys. Only email addresses the provider marks as verified are trusted.
- **Per-instance feature flag** `sso_providers` ([0017](0017-instance-feature-flags.md)): on for the main instance, off by default for other instances until the operator registers their own provider credentials. Setup docs walk operators through registration.
- **Multiple sign-in methods per account:** an account can have any mix of passkeys, password, magic link, and linked providers (`AuthIdentity` in DOMAIN_MODEL.md). Users are encouraged to add a second method so losing a Google or Apple account doesn't lock them out. The last remaining method can't be removed.
- **Account linking is always explicit:** a new SSO login whose email matches an existing account is **never** merged automatically. The user must sign in to the existing account by another method and confirm the link. Linking and unlinking send an email alert and are written to the audit log.
- **No trust from SSO:** SSO accounts start as `unverified`, like any other signup. Verification tiers are earned the same way for everyone.
- **MFA still required for privileged roles:** verified-organization members, moderators, community owners, and admins must register a passkey or TOTP on the platform itself, regardless of how they sign in. The platform can't reliably tell whether the user's Google or Apple account uses two-factor sign-in.
- **App Store guideline 4.8 compliance:** the iOS app starts **without Google sign-in**, so the guideline doesn't apply. Once the project's App Store account exists, add Sign in with Apple to the iOS app, then Google can be added there too. (A further option, to confirm with App Review, is presenting passkey signup with an optional, private email as the equivalent option.)
- **Apple specifics:** support "Hide My Email" relay addresses (register the instance's sending domain with Apple's relay; don't use relay addresses for duplicate-account checks). On account deletion, revoke the user's Apple tokens, as Apple requires.
- **Minimal scopes:** request only `openid email` (plus `name` on first Apple sign-in). No contacts, no profile photos, no access tokens kept beyond what deletion and revocation require.
- **Mobile apps:** use native sign-in sheets (Google Sign-In, Sign in with Apple) through Capacitor plugins, then exchange the provider's ID token with the selected instance's API. How an instance accepts tokens issued to the shared app (audience = the official app ID) is to be settled in a Phase 1 spike.

## Consequences

- Most people never need to create a password; signup takes seconds.
- Google and Apple learn when their users sign in to the platform. This is disclosed in the privacy policy, and passkeys remain the recommended, provider-independent option.
- Operators of smaller instances have extra setup if they want SSO, and Apple sign-in on their website costs them Apple's annual fee; the platform works fully without either. Instances never pay for the app itself.
- More sign-in paths means more code to secure and test. Every path ends in the same session model from [0014](0014-authentication-sessions-mfa.md), so bans, "sign out everywhere", and MFA step-up apply equally.

## Alternatives considered

- **No SSO:** simplest and most private, but loses users at signup and makes the iOS app less familiar.
- **Google, Apple, and Meta:** broader reach, but Meta's tracking and review process conflict with the platform's privacy goals, with little added reach.
- **Hosted identity service (Auth0, Clerk, etc.):** handles providers for us, but costs money per user, sends all sign-ins to a third party, and doesn't suit self-hosted instances.
- **Fediverse sign-in (use an existing Mastodon account):** interesting for Phase 5, but there's no widely adopted standard for it yet.
