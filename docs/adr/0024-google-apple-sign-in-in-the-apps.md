# 0024. Google and Apple sign-in in the shared apps

- **Status:** Proposed
- **Date:** 2026-11-16
- **Details:** the "Mobile apps" point of [0019](0019-sign-in-options-passkeys-sso.md), left to a Phase 1 spike. The rest of 0019 stands.
- **Related:** [0014](0014-authentication-sessions-mfa.md), [0017](0017-instance-feature-flags.md), [0018](0018-mobile-apps-capacitor.md), [0023](0023-mobile-instance-picker.md); THREAT_MODEL.md S3–S7, M4; PLAN.md Phase 1 (spike) and Phase 2 (Google and Apple sign-in)

## Context

On the web, each instance signs people in to Google or Apple with its own registration (its own OAuth client, or its own Apple Services ID), so the ID token's audience is that instance ([0019](0019-sign-in-options-passkeys-sso.md)). The apps are different: one app, published by the project, connects to any instance ([0018](0018-mobile-apps-capacitor.md), [0023](0023-mobile-instance-picker.md)). The native sign-in sheets issue ID tokens to **the app**, so every instance would receive tokens with the same audience. This spike settles how an instance can safely accept them.

What the providers' documentation establishes:

- **Google (Android, Credential Manager):** the app passes a *web* client ID from the project's Google Cloud project (`setServerClientId`), and the ID token's `aud` is that client ID. The Android OAuth client, registered with the app's package name and signing certificate, appears as `azp`, "the authorized presenter … only needed when the party requesting the ID Token is not the same as the audience". `setNonce()` puts a nonce in the token, and Google says to check it on the server to prevent replay. `sub` is "unique among all Google Accounts and never reused": the same for a person at every instance, and on the web too.
- **Apple (iOS):** the identity token's `aud` is the app's bundle ID. Apple's verification steps include checking the `nonce`. The user identifier (`sub`) and any private relay address are **specific to the developer team**: "they're the same for a user across all apps written by a single development team, and different … across apps written by different development teams." The app is under the project's team; an instance's own web sign-in is under its operator's team, unless the project runs that instance.
- **Apple private relay email:** to email relay addresses, the team must register its outbound domains or addresses, with SPF. Otherwise the mail doesn't arrive.
- **Apple token revocation:** apps that offer Sign in with Apple should revoke the user's tokens through Apple's REST API when an account is deleted (App Review guideline 5.1.1(v)). Revoking needs a refresh or access token, obtained by exchanging the sign-in's authorization code, and both calls need a client secret: a JWT signed with the **team's** Sign in with Apple private key.

The threats:

- **Replay:** an instance, or anyone who reads its logs, gets a token with the app's audience and presents it to another instance to sign in as that person. Google ID tokens last about an hour.
- **Relay by a malicious instance:** someone signs in to a malicious instance M through the app. M starts a sign-in at a real instance L, obtains L's nonce, and hands it to the app as its own. The app gets a token carrying L's nonce and sends it to M, which forwards it to L. A nonce issued by the instance alone doesn't stop this: M controls what the app is told.
- **Tokens from outside the app:** if a web page could obtain tokens with the app's audience, the app's protections wouldn't apply to them.

## Decision

**The app binds every token to the instance it's connected to.** Only the official app can obtain tokens with its audience, and the app knows which instance it's connected to, because it checked that in the picker ([0023](0023-mobile-instance-picker.md)). So:

1. The app asks the instance to start a sign-in: `POST /api/v1/auth/sso/{google|apple}/begin`. The instance returns a sign-in ID and a random 256-bit nonce, stored hashed, single use, valid for 5 minutes.
2. The app passes the provider `SHA-256(origin || "\n" || nonce)`, base64url-encoded, where `origin` is the connected instance's origin from the picker, never a value from the instance's response.
3. The app sends the ID token to `POST /api/v1/auth/sso/{provider}/finish` with the sign-in ID (and, from Apple, the name it shares only on the first sign-in).
4. The instance computes the same hash with **its own** origin (from `PC_PUBLIC_URL`) and the nonce it issued, and accepts the token only if the token's `nonce` matches. Then it checks the rest, below, and consumes the sign-in.

A token obtained through M carries a hash of M's origin, so L rejects it, even when M relayed L's nonce. Replay fails the same way, and the single-use nonce stops it at the same instance.

**What the instance checks:**

- The signature, against the provider's published keys (`coreos/go-oidc`, as on the web); `iss`; `exp`; the nonce, as above.
- **Google:** `aud` is the app's web client ID **and** `azp` is the app's Android client ID, so the token came from the Android app. The web client used as the app's server client ID has no JavaScript origins or redirect URIs registered, so no website can obtain tokens with that audience.
- **Apple:** `aud` is the app's bundle ID.
- The email is used only if the provider marks it verified. Accounts are never linked automatically ([0019](0019-sign-in-options-passkeys-sso.md)).

**Identities are keyed by who issued them.** A Google identity is `(issuer, sub)`, the same from the web and the app, so a person's Google sign-ins at one instance match. An Apple identity is `(issuer, team, sub)`: at an instance run by someone other than the project, Apple sign-in from the web and from the app are two different identities for the same person, and linking them is explicit, as for any other method.

**Instances opt in.** A feature flag, `app_sso` ([0017](0017-instance-feature-flags.md)), with the official app's identifiers as defaults: they're public values, and a modified app with its own registrations sets its own. It's on for the main instance.

**Apple sign-in in the app needs the instance's email to reach relay addresses.** Sign-in codes, notices, and account recovery all go by email ([0014](0014-authentication-sessions-mfa.md)). So an instance accepts Apple sign-in from the app only once its outbound email domain is registered in the project's Apple developer account; the project does this for the instances on the app's list. Elsewhere, the app doesn't offer Apple sign-in.

**Apple token revocation goes through a small project-run service.** Instances can't hold the project's Apple private key: with it, an operator could act as the official app toward Apple. When an account that signed in with Apple from the app is deleted, the app asks Apple for a fresh authorization code and sends it to a project-run endpoint, which exchanges it, revokes the tokens at once, and stores nothing. It holds the key and does nothing else. It's needed only when Sign in with Apple ships in the iOS app, which waits for the App Store account (Phase 2); its exact shape is decided then.

**Plugins.** Native sheets come through a Capacitor plugin that can pass a nonce: on Android for Google, and on iOS for Apple. `@capgo/capacitor-social-login` documents a nonce for both, and Capawesome's Google and Apple plugins document one for Google on Android and for Apple. The plugin is chosen and reviewed in Phase 2, like the secure storage plugin in [0023](0023-mobile-instance-picker.md). The iOS app still starts without Google sign-in ([0019](0019-sign-in-options-passkeys-sso.md)).

## Consequences

- Any instance can accept sign-ins from the official app without its own Google or Apple registration, and without trusting other instances.
- The protection depends on the app: a modified app that skips the binding would make its own users' tokens replayable. Instances only accept the official app's audience by default, and the official app is the only one Google and Apple will issue those tokens to.
- People see the project's name on Google's consent screen and in Apple's sign-in sheet, not the instance's. The app shows the connected domain beforehand, as for every sign-in ([0023](0023-mobile-instance-picker.md)). The project needs its own homepage and privacy policy for Google's brand verification.
- Google's `sub` is the same at every instance, so instances that compare notes could tell that two accounts belong to the same Google user. This is true of Google sign-in anywhere and goes in the privacy policy. Apple's identifiers are per team, which limits this for Apple.
- The project takes on two operational tasks: registering instances' email domains for Apple's relay, and running the revocation service. Both wait for Sign in with Apple in the app.
- Debug builds are signed with a different certificate, so Google sign-in in them needs that certificate registered as its own Android client.

## Alternatives considered

- **Instance-issued nonce without binding to the origin:** stops replay, but not a malicious instance relaying a sign-in in real time.
- **The instance's own Google or Apple registration in the app:** a single published app can't switch to a different client per instance, and an Apple bundle ID belongs to one team.
- **A project-run sign-in broker for every sign-in:** the broker would see every sign-in at every instance and become a central point of failure, against the federation goals. The revocation service is far narrower: it only acts on account deletion, and keeps nothing.
- **Sharing the project's Apple private key with instances:** lets any operator act as the official app toward Apple.
- **Skipping Apple token revocation:** Apple's guideline asks for it, and the forum workaround (asking people to revoke access in their Apple settings) risks App Review rejection.

## References

- Android: [Implement Sign in with Google](https://developer.android.com/identity/sign-in/credential-manager-siwg-implementation) (`setServerClientId`, `setNonce`)
- Google: [OpenID Connect reference](https://developers.google.com/identity/openid-connect/reference) (`sub`, `aud`, `azp`, `nonce`)
- Apple: [Verifying a user](https://developer.apple.com/documentation/signinwithapple/verifying-a-user), [Transferring your apps and users to another team](https://developer.apple.com/documentation/signinwithapple/transferring-your-apps-and-users-to-another-team) (team-scoped identifiers), [Communicating using the private email relay service](https://developer.apple.com/documentation/signinwithapple/communicating-using-the-private-email-relay-service), [Token revocation](https://developer.apple.com/documentation/signinwithapplerestapi/revoke-tokens), [5.1.1(v) Offering account deletion](https://developer.apple.com/support/offering-account-deletion-in-your-app/)
- Plugins: [`@capgo/capacitor-social-login`](https://github.com/Cap-go/capacitor-social-login), [`@capawesome/capacitor-google-sign-in`](https://cdn.jsdelivr.net/npm/@capawesome/capacitor-google-sign-in@0.1.2/README.md), [`@capawesome/capacitor-apple-sign-in`](https://capawesome.io/plugins/apple-sign-in/)
