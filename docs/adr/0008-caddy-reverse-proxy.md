# 0008. Caddy as the reverse proxy

- **Status:** Accepted
- **Date:** 2026-10-29
- **Related:** PLAN.md (Stack), [0009](0009-docker-compose-deployment.md), THREAT_MODEL.md D1

## Context

Every instance needs TLS, static file serving for the SPA, and routing to the API. Operators may not be experienced with TLS setup, and a misconfigured proxy is a common source of security problems (threat O4).

## Decision

Use Caddy v2 as the single entry point:

- Automatic HTTPS (ACME) with sensible TLS defaults
- Serves the built SPA and proxies `/api`, public pages, and (later) ActivityPub routes to the Go service
- Sets security headers (HSTS, CSP, `X-Content-Type-Options`, `Referrer-Policy`)
- Basic rate limiting and request size limits at the edge, with finer per-user limits in the API
- Media served from a separate hostname (e.g. `media.{instance}`) to isolate user content (see [0011](0011-media-processing-pipeline.md))

The project ships a ready-to-use `Caddyfile` where operators only set their domain.

## Consequences

- No manual certificate management; very small config.
- Rate limiting requires a Caddy plugin (custom build via `xcaddy`) or must be done in the API. Proposed: do rate limits in the API, keep the Caddy image stock.
- Operators who already run nginx or Traefik can put their own proxy in front instead; document the required headers and routes.

## Alternatives considered

- **nginx + certbot:** widely known, but more configuration and a separate certificate renewal process.
- **Traefik:** good with Docker labels, but more complex configuration for a single-app deployment.
