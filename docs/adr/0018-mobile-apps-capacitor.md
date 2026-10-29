# 0018. Native mobile apps with Capacitor

- **Status:** Proposed
- **Date:** 2026-10-29
- **Related:** PLAN.md decision #8, Phases 1–3; [0007](0007-vuetify-spa-with-server-rendered-public-pages.md), [0012](0012-activitypub-shaped-model-federation-deferred.md), [0014](0014-authentication-sessions-mfa.md); THREAT_MODEL.md M1–M5

## Context

Native Android and iOS apps are an end goal: most adopters, volunteers, and shelter staff use phones, and the apps should increase participation and make communication faster. Native features that matter here:

- **Push notifications:** application status, new messages, saved-search matches, lost-pet alerts nearby
- **Camera and photo library:** shelter staff listing animals straight from the kennel
- **Location:** "near me" search, lost-pet alerts
- **Deep links:** shared listing URLs open in the app
- **App store presence:** familiar install path, and it signals legitimacy

The project is small and lean. Maintaining separate web, Android, and iOS UIs would triple the frontend work.

## Decision

Package the existing Vue 3 + Vuetify app as native Android and iOS apps with **Capacitor**:

- **One codebase:** the SPA from [0007](0007-vuetify-spa-with-server-rendered-public-pages.md) is built once and packaged into both apps. Platform differences sit behind a small `platform` module (web vs. native).
- **Native plugins:** push notifications (FCM on Android, APNs on iOS, UnifiedPush on Android devices without Google services), camera, geolocation, secure storage (Keychain / Android Keystore), app links / universal links, share sheet.
- **API-first backend:** everything goes through the versioned public API (`/api/v1`). The web app gets no private endpoints.
- **Multiple instances:** because the platform is federated, the apps start with an **instance picker** (with a default instance), like fediverse apps. The API exposes instance metadata (name, rules, enabled feature flags) so the app adapts per instance.
- **Push privacy:** push payloads contain no personal data, only a notification type and an ID (e.g. "new message"). The app fetches details over the API after opening. Users choose which notification types they get.
- **Releases:** CI builds both apps on every release. Betas go out through the Play Store testing track and TestFlight in Phase 2; public store release is in Phase 3.
- **One publisher for all instances:** the project publishes the apps under a single organization account on each store. Instances never need their own store accounts.
- **Apple fee:** once the nonprofit entity exists, apply for Apple's developer fee waiver for nonprofits. The waiver requires a free app with no in-app purchases, so donations are taken on the website only.
- **iOS stopgap:** until the App Store account exists, iOS users install the web app to their home screen, which supports web push on iOS 16.4+. The web app stays a good installable app after that, for people who don't want the native app.
- **F-Droid:** an Android build with no proprietary dependencies (UnifiedPush instead of FCM) is published on F-Droid, at no cost.
- **Store requirements are built in from the start:** in-app account deletion, report and block on all user-generated content, content filtering, privacy labels/data safety forms.

## Consequences

- One UI codebase serves web, Android, and iOS. Features ship everywhere at once.
- Vuetify inside a WebView feels less native than a fully native UI; scrolling and transitions need attention, and Vuetify's Material styling looks out of place on iOS. Acceptable for the MVP; revisit if user testing shows problems.
- Mobile clients need token-based auth instead of cookies (see [0014](0014-authentication-sessions-mfa.md)).
- Push delivery becomes a worker job queue (`push`), with device token management and cleanup of expired tokens.
- New costs: Apple Developer Program ($99/year, or $0 with the nonprofit fee waiver), Google Play ($25 one-time), and macOS hardware or a CI service for iOS builds. These go into the operating budget once, for the whole project.
- Two Android builds (Play Store with FCM, F-Droid without) add some CI and testing work.
- **Licensing risk:** Apple's App Store terms conflict with GPL-family licenses for code the publisher doesn't hold copyright to. Before accepting outside contributions to the client, adopt a contributor agreement or an app-store exception for the client code (PLAN.md open questions).
- Apps can't be updated instantly (store review). Keep the app tolerant of API changes and version the API.

## Alternatives considered

- **Fully native (Kotlin + Swift):** best user experience, but three UIs to maintain for a small volunteer project.
- **Flutter or React Native:** near-native feel, but a second UI codebase alongside the web app (or replacing it, losing the server-rendered public pages' shared code).
- **Progressive web app only:** free and needs no store accounts, but no store presence, and iOS web push only works after installing to the home screen. Used as the iOS stopgap and kept as an option, but not the end goal.
