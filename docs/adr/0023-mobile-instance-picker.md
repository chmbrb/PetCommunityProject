# 0023. Mobile instance picker and app sessions

- **Status:** Accepted
- **Date:** 2026-11-15 (amended 2026-11-15: secure storage plugin chosen; device token issued by the API)
- **Details:** the "instance picker" in [0018](0018-mobile-apps-capacitor.md) and the mobile tokens in [0014](0014-authentication-sessions-mfa.md). Both stand.
- **Related:** PLAN.md Phase 1 (mobile shell); [0017](0017-instance-feature-flags.md), [0019](0019-sign-in-options-passkeys-sso.md); THREAT_MODEL.md M4, S1

## Context

The apps are published once and can connect to any instance ([0018](0018-mobile-apps-capacitor.md)). Today a native build talks to one instance fixed at build time (`VITE_DEFAULT_INSTANCE_URL`), so the Phase 1 exit (sign up, turn on TOTP, and upload a photo from a phone camera in the app) can't be met for anyone but the default instance.

The app also doesn't finish signing in yet. The API issues a bearer token when a sign-in sends `"client": "mobile"` and accepts it in `Authorization` ([0014](0014-authentication-sessions-mfa.md)), but the app never sends either, and never stores a token or the `X-Device-Token`. Its requests are cross-origin without credentials, so the session cookie the API sets is dropped. Choosing an instance and keeping a session on it have to be designed together: the session belongs to the instance.

Threats:

- **Fake or phishing instances (M4).** Anyone can run a server that looks like an instance, with a convincing name, to collect passwords and email codes. The name an instance gives itself proves nothing; only its domain does.
- **Downgrade and redirects.** An address typed without a scheme, or a server that redirects, must not land the app on plain HTTP or on a different host than the one shown.
- **Token theft.** A bearer token lasts 180 days. It must not sit in storage that other apps, backups, or a WebView script can read.

## Decision

**One instance at a time.** The app is connected to exactly one instance, chosen on first launch and changeable from settings. Switching signs out of the current one. Several accounts or instances side by side can come later; nothing here prevents it.

**First launch.** The picker shows the default instance, preselected (from `VITE_DEFAULT_INSTANCE_URL`), and a field to enter another one. The default is the only entry on a curated list bundled with the app; the list grows by app release, not from a server the project runs.

**Normalizing what's typed.** The input is parsed as a URL, with `https://` added if there's no scheme:

- Only `https:` is accepted. Development builds also accept `http:` for `localhost`, `10.0.2.2` (the Android emulator's host), and private-network addresses.
- Userinfo (`user@host`), paths, queries, and fragments are rejected rather than silently dropped, so what's shown is what was typed.
- The host is converted to its ASCII (punycode) form, and that form is what's displayed, so lookalike Unicode domains are visible.
- The instance's identity is its origin (`https://host[:port]`).

**Checking the instance.** The app fetches `GET {origin}/api/v1/instance` with a 10-second timeout and **no redirects** (a redirect is an error, not followed). The instance is accepted only if:

- the response is JSON with the expected shape, and `api_version` is one the app supports (`v1`);
- a new `software` field reads `petcommunity` (see below), so an unrelated server that happens to answer the path is rejected;
- the `domain` it reports matches the host that was entered. An instance that claims another domain is rejected.

`GET /api/v1/instance` gains `"software": { "name": "petcommunity", "version": "…" }`, the same shape NodeInfo uses, so Phase 5 can publish NodeInfo from the same values. Modified versions keep `petcommunity` as long as they serve this API.

**Confirming before sign-in.** Before the sign-in screen, the app shows the instance's **domain** prominently and its name second, with a link to the instance's rules once they exist. An instance not on the curated list gets a plain notice: "This instance isn't on the app's list. Only continue if you trust *domain*." It's a notice, not a block: anyone may run an instance. The connected domain stays visible on the sign-in and sign-up screens and in settings.

**Sessions in the app.**

- Sign-ins and sign-ups from the app send `"client": "mobile"`. The API returns the bearer token, which is stored in the Keychain (iOS) or with an Android Keystore key, using the secure storage plugin below. It's never stored in `localStorage` or Capacitor Preferences.
- Every request to the connected instance sends `Authorization: Bearer …`. The token is sent only to the stored origin, never to URLs built from server responses (such as image URLs on another host).
- The API issues a device token with the app's first session (`session.device_token`). The app stores it the same way, keeps it when signing out, and sends it as `X-Device-Token`, so password sign-ins recognize the device as [0014](0014-authentication-sessions-mfa.md) describes.
- The second step of a sign-in (`POST /api/v1/auth/login/mfa`) sends no `client` field: the API rejects unknown fields, and the first step already recorded that it's the app. Only a `401` with code `unauthenticated` ends the stored session; other `401`s (`mfa_required`, `email_code_required`, `sign_in_failed`) are part of signing in.
- Signing out, or switching instance, calls `POST /api/v1/auth/logout` and then deletes the token. A `401` on an authenticated request deletes the token and returns to sign-in.
- The connected origin and the instance metadata are not secret and are kept in Capacitor Preferences.

**Secure storage plugin.** [`@aparajita/capacitor-secure-storage`](https://github.com/aparajita/capacitor-secure-storage) (MIT), pinned to an exact version like the app's other dependencies:

- **iOS:** each item is stored with the `afterFirstUnlockThisDeviceOnly` accessibility, so it's readable in the background after the phone's first unlock (for push, later), but never copied into backups or onto a new phone. iCloud Keychain sync stays off.
- **Android:** values are encrypted with AES-GCM under a key that's generated in, and never leaves, the Android Keystore. The plugin keeps the encrypted values in `WSSecureStorageSharedPreferences`, which the app excludes from Android backups: the key isn't backed up, so a restored copy couldn't be decrypted.
- **Only in the native apps.** The plugin falls back to `localStorage` on the web, so it's only loaded when `Capacitor.isNativePlatform()` is true. The web app keeps using its `HttpOnly` cookie.
- **A value that can't be read counts as signed out.** If a read fails (a reset Keystore, or data restored from elsewhere), the app deletes the item and returns to sign-in. A missing device token is issued again at the next sign-in. If secure storage can't be opened at all, the app keeps the session in memory only, so signing in still works until the app closes.
- **A reinstall starts clean.** iOS keeps Keychain items after an app is deleted. The app records in Preferences, which are deleted with the app, that it has run before; if that's missing, it clears secure storage before reading anything.

**Sign-in methods in the app.** Email codes and passwords work as on the web: email codes exist for exactly this case, where a link would open in the browser instead of the app. Two-step sign-in works the same. Passkeys in the app are **not** part of this decision (see Consequences).

## Consequences

- Phase 1's mobile exit becomes testable against any instance, including a local one from the emulator.
- `X-Device-Token` and mobile sessions, already in the API, get their first client.
- The secure storage plugin is a community plugin with one maintainer, and its iOS side depends on `keychain-swift`, which hasn't had a release since 2024. Both are small and MIT-licensed. Updates get the same review as the first choice: the Keychain accessibility used, and the Android cipher and key setup. If either stops being maintained, it's small enough to fork. The plugin also lists `@capacitor/app` and `@capacitor/keyboard` as dependencies that its code doesn't use; check after `cap sync` that they aren't added to the native projects.
- **Passkeys don't work in the app yet.** The WebView's origin (`https://localhost` or `capacitor://localhost`) isn't an origin any instance accepts, and native passkeys need each instance's domain to be associated with the app. Android checks this at run time through a `/.well-known/assetlinks.json` file on the instance, which any instance can serve. iOS only trusts domains listed in the app's entitlements when the app is built, so an iOS app can't use passkeys on instances added later. A follow-up ADR decides how passkeys work in the app. Until then, the app hides passkey sign-in and offers email codes and passwords.
- Email links keep opening in the browser. App links per instance have the same build-time limit on iOS as passkeys.
- The curated list is only as current as the installed app. Updating it needs a release; a fetched list would need a server the project runs, which it avoids.
- Switching instances signs you out. People with accounts on several instances will want more, later.

## Alternatives considered

- **Several instances signed in at once:** what some fediverse apps do, but it multiplies the session handling, push registration, and what the UI must show about which instance is active. Not needed for the pilot.
- **A curated list fetched from the project's server:** easier to update, but a central server every app depends on, against the project's federation goals, and a single target for tampering with the list.
- **Blocking instances not on the list:** stops phishing instances, but also every legitimate new instance until an app release. A clear notice and the visible domain are the balance chosen.
- **Following redirects during the check:** convenient when an instance moves, but it lets the app end up somewhere other than the host the person checked.
- **Keeping the token in `localStorage` or Preferences:** readable by any script in the WebView and included in some device backups.
- **`capacitor-secure-storage-plugin`:** well maintained, but its iOS items always use `afterFirstUnlock`, which lets an encrypted backup restore the token onto a new phone, and on Android it encrypts values with RSA and PKCS#1 v1.5 padding, an older scheme than AES-GCM.
- **`@capgo/capacitor-native-biometric`:** stores secrets behind a biometric prompt, which suits unlocking a saved password but not a token the app needs for every request. It also brings several Android UI libraries.
- **Capawesome Secure Preferences and Ionic Identity Vault:** available only to sponsors or as a paid product.
- **Cookies in the app:** the API never allows credentials cross-origin ([0014](0014-authentication-sessions-mfa.md)), and third-party cookies in WebViews are unreliable.
