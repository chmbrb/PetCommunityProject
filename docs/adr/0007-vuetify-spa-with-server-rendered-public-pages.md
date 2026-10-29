# 0007. Vuetify SPA with server-rendered public pages

- **Status:** Accepted
- **Date:** 2026-10-29
- **Related:** PLAN.md (Stack), [0003](0003-go-backend.md), [0018](0018-mobile-apps-capacitor.md) (the same SPA packaged as mobile apps), THREAT_MODEL.md T3

## Context

Most of the app is interactive and behind login: dashboards, applications, moderation queues. But adoption listings must be found through search engines and shared on social media, and link previews require the page's HTML to contain the content and OpenGraph tags. A pure single-page app serves an empty shell to crawlers.

## Decision

- **Interactive app:** Vue 3 + Vuetify 3 single-page app, built with Vite into static files and served by Caddy.
- **Public pages:** Go renders a small set of public pages with `html/template`: listing detail, organization profile, community landing page, and lost/found posts. Each includes OpenGraph/Twitter card tags and JSON-LD structured data. These pages are lightweight and load the SPA for interactive actions (e.g. "Apply").
- **User content:** Markdown is converted to sanitized HTML on the server (allow-list). The SPA never renders unsanitized HTML (`v-html` only on server-sanitized fields).

## Consequences

- Listings are crawlable and share well, without a Node.js server in production.
- Two rendering paths for listing data. Keep the server-rendered pages minimal and share the data shape with the API.
- Server-rendered pages also give a fast, accessible view on weak mobile connections.

## Alternatives considered

- **Nuxt (Vue SSR):** one rendering path, but adds a Node runtime container and more resource use.
- **Prerendering service:** extra moving part and stale content.
- **SPA only:** poor search engine visibility and link previews for the core feature.
